// Package observe turns Claude Code hook spool files and transcripts into
// sessions and raw actions: the story side of the join.
//
// Hooks (the spool) are the primary source; transcripts are secondary and are
// parsed tolerantly because their format is internal to Claude Code. Merge
// combines both: actions are deduplicated on tool_use_id (the earliest session
// wins, because --fork-session copies history), and the task title is chosen
// as custom title, then the first real prompt's first line, then the AI title.
// Everything that leaves this package is redacted; excerpts are capped at
// 4000 characters.
package observe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/hook"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
)

// RawAction is an agent action plus the redacted material adapter signatures
// match against. Action.Identifiers is empty until adapters.Registry.Extract fills it.
type RawAction struct {
	Action contract.AgentAction
	Input  any    // redacted tool_input as decoded JSON
	Output string // redacted result text (Bash stdout+stderr, MCP text content, or error text)
	// MCP holds the identifier params (e.g. project_ref) of the MCP server
	// the call went to, from its URL in Claude Code's config. Hook records
	// only; nil otherwise.
	MCP map[string]string
}

// Batch is the result of one ingest pass.
type Batch struct {
	Sessions []contract.Session
	Actions  []RawAction
	Files    []string // spool files consumed; delete with Ack only after the store commits
	Warnings []contract.Warning
	// Ambiguous lists tool_use_ids that Merge found in two or more sessions with
	// identical timing. The kept owner is a tie-break, so attribution must treat
	// resources created by these actions as unknown.
	Ambiguous []string
}

const (
	// ExcerptMax is the maximum length, in characters, of input/result excerpts.
	ExcerptMax = 4000
	// OutputMax caps RawAction.Output, in bytes.
	OutputMax = hook.MaxField
	// TaskTitleMax is the schema limit for Session.task_title.
	TaskTitleMax = 120

	staleTemp = time.Hour
)

// sessionAcc accumulates what one source knows about a session.
type sessionAcc struct {
	id                       string
	started, last            time.Time
	ended                    *time.Time
	endReason                string
	cwd                      string
	branch, version, model   string
	customTitle, firstPrompt string
	firstPromptAt            time.Time
	aiTitle                  string
	source                   contract.SessionSourcesElem
}

func (a *sessionAcc) seen(t time.Time) {
	if t.IsZero() {
		return
	}
	if a.started.IsZero() || t.Before(a.started) {
		a.started = t
	}
	if t.After(a.last) {
		a.last = t
	}
}

func (a *sessionAcc) session(developerID string, repos *repoCache) contract.Session {
	s := contract.Session{
		ID:          a.id,
		Agent:       contract.SessionAgentClaudeCode,
		DeveloperID: developerID,
		StartedAt:   a.started.UTC(),
		Sources:     []contract.SessionSourcesElem{a.source},
		TitleSource: contract.SessionTitleSourceNone,
	}
	if !a.last.IsZero() {
		l := a.last.UTC()
		s.LastActivityAt = &l
	}
	if a.ended != nil {
		e := a.ended.UTC()
		s.EndedAt = &e
	}
	s.EndReason = optional(truncateRunes(a.endReason, 50))
	s.GitBranch = optional(truncateRunes(redact.String(a.branch), 250))
	s.AgentVersion = optional(truncateRunes(a.version, 50))
	s.Model = optional(truncateRunes(a.model, 100))
	switch {
	case strings.TrimSpace(a.customTitle) != "":
		s.TaskTitle = optional(cleanTitle(a.customTitle))
		s.TitleSource = contract.SessionTitleSourceCustomTitle
	case a.firstPrompt != "":
		s.TaskTitle = optional(cleanTitle(a.firstPrompt))
		s.TitleSource = contract.SessionTitleSourceFirstPrompt
	case strings.TrimSpace(a.aiTitle) != "":
		s.TaskTitle = optional(cleanTitle(a.aiTitle))
		s.TitleSource = contract.SessionTitleSourceAiTitle
	}
	if a.cwd != "" {
		s.Repo = repos.get(a.cwd)
	}
	return s
}

func cleanTitle(t string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(t), "\n")
	return strings.TrimSpace(truncateRunes(redact.String(strings.TrimSpace(line)), TaskTitleMax))
}

// IngestSpool reads all spool files. pathSalt keys the repo path hash (see
// PathHash) and must not be empty.
func IngestSpool(p paths.Paths, developerID, pathSalt string) (Batch, error) {
	var b Batch
	if developerID == "" {
		return b, errors.New("developer id is empty")
	}
	if pathSalt == "" {
		return b, errors.New("path hash salt is empty")
	}
	entries, err := os.ReadDir(p.Spool)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, fmt.Errorf("read spool: %w", err)
	}
	type item struct {
		file string
		rec  contract.SpoolRecord
	}
	var items []item
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(p.Spool, name)
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, ".tmp-") {
			// A hook that died between create and rename leaves these behind.
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > staleTemp {
				b.Files = append(b.Files, full)
			}
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			b.Warnings = append(b.Warnings, warn("spool_unreadable", fmt.Sprintf("cannot read spool file %s: %v", name, err)))
			continue
		}
		var rec contract.SpoolRecord
		if err := json.Unmarshal(raw, &rec); err != nil || rec.SessionID == "" || rec.Event == "" {
			// Corrupt or foreign: consume it so it doesn't block the spool forever.
			b.Warnings = append(b.Warnings, warn("spool_invalid", fmt.Sprintf("skipped invalid spool file %s", name)))
			b.Files = append(b.Files, full)
			continue
		}
		items = append(items, item{full, rec})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].rec.ReceivedAt.Equal(items[j].rec.ReceivedAt) {
			return items[i].rec.ReceivedAt.Before(items[j].rec.ReceivedAt)
		}
		return items[i].file < items[j].file
	})

	sessions := map[string]*sessionAcc{}
	var order []string
	seenAction := map[string]bool{}
	for _, it := range items {
		r := it.rec
		b.Files = append(b.Files, it.file)
		acc := sessions[r.SessionID]
		if acc == nil {
			acc = &sessionAcc{id: r.SessionID, source: contract.SessionSourcesElemHook}
			sessions[r.SessionID] = acc
			order = append(order, r.SessionID)
		}
		acc.seen(r.ReceivedAt)
		if r.Cwd != nil && acc.cwd == "" {
			acc.cwd = *r.Cwd
		}
		if r.SessionTitle != nil && *r.SessionTitle != "" {
			acc.customTitle = *r.SessionTitle // the latest rename wins
		}
		if r.Model != nil && *r.Model != "" && acc.model == "" {
			acc.model = *r.Model
		}
		switch r.Event {
		case "UserPromptSubmit":
			if r.TitleCandidate != nil && acc.firstPrompt == "" && !strings.HasPrefix(*r.TitleCandidate, "<task-notification>") {
				acc.firstPrompt = *r.TitleCandidate
				acc.firstPromptAt = r.ReceivedAt
			}
		case "SessionEnd":
			t := r.ReceivedAt
			acc.ended = &t
			if r.Reason != nil {
				acc.endReason = *r.Reason
			}
		case "PostToolUse", "PostToolUseFailure":
			ra := spoolAction(r, it.file)
			if seenAction[ra.Action.ID] {
				continue
			}
			seenAction[ra.Action.ID] = true
			b.Actions = append(b.Actions, ra)
		}
	}
	repos := newRepoCache(pathSalt)
	for _, id := range order {
		b.Sessions = append(b.Sessions, sessions[id].session(developerID, repos))
	}
	return b, nil
}

// spoolAction turns one PostToolUse(Failure) record into a RawAction.
func spoolAction(r contract.SpoolRecord, file string) RawAction {
	id := deref(r.ToolUseID)
	if id == "" {
		// No tool_use_id (e.g. cut by the stdin cap and not recoverable): use the
		// unique spool file name so the action is still recorded once.
		id = "spool:" + strings.TrimSuffix(filepath.Base(file), ".json")
	}
	tool := deref(r.ToolName)
	status := contract.AgentActionStatusOk
	var out string
	if r.Event == "PostToolUseFailure" {
		status = contract.AgentActionStatusError
		out = deref(r.Error)
		if out == "" {
			out = OutputText(tool, r.ToolResponse)
		}
	} else {
		out = OutputText(tool, r.ToolResponse)
	}
	mcp := deref(r.McpServer)
	if mcp == "" {
		mcp = mcpServerFromTool(tool)
	}
	ra := newRawAction(id, r.SessionID, deref(r.AgentID), r.ReceivedAt, tool, mcp, status,
		contract.AgentActionSourceHook, r.ToolInput, out)
	ra.MCP = MCPParams(r.McpParams)
	return ra
}

// MCPParams returns the params the hook may record (hook.MCPParamAllowed),
// or nil if none: a spool file or store row is never trusted blindly.
func MCPParams(m map[string]string) map[string]string {
	var out map[string]string
	for k, v := range m {
		if hook.MCPParamAllowed(k, v) {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}
	return out
}

func newRawAction(id, sessionID, agentID string, at time.Time, tool, mcp string, status contract.AgentActionStatus,
	source contract.AgentActionSource, input any, output string) RawAction {
	in := redact.Value(input)
	out := capText(redact.String(output))
	a := contract.AgentAction{
		ID:          truncateRunes(id, 128),
		SessionID:   sessionID,
		OccurredAt:  at.UTC(),
		ToolName:    truncateRunes(tool, 200),
		Status:      status,
		Source:      source,
		Identifiers: []contract.Identifier{},
	}
	a.AgentID = optional(agentID)
	a.McpServer = optional(truncateRunes(mcp, 200))
	a.InputExcerpt = optional(Excerpt(InputText(tool, in)))
	a.ResultExcerpt = optional(Excerpt(out))
	return RawAction{Action: a, Input: in, Output: out}
}

// Ack deletes consumed spool files. Files already gone are not an error.
func Ack(files []string) error {
	var errs []error
	for _, f := range files {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// OutputText renders a tool response as text: Bash stdout + stderr, MCP text
// content joined, a plain string as is, anything else as compact JSON.
func OutputText(tool string, resp any) string {
	switch t := resp.(type) {
	case nil:
		return ""
	case string:
		return t
	case map[string]any:
		if so, ok := t["stdout"].(string); ok {
			se, _ := t["stderr"].(string)
			switch {
			case se == "":
				return so
			case so == "":
				return se
			default:
				return so + "\n" + se
			}
		}
		if c, ok := t["content"].([]any); ok { // {content:[...], isError} shape
			if s, ok := joinContent(c); ok {
				return s
			}
		}
	case []any:
		if s, ok := joinContent(t); ok {
			return s
		}
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return ""
	}
	return string(b)
}

// joinContent joins the text of an MCP/Anthropic content array.
func joinContent(blocks []any) (string, bool) {
	var parts []string
	found := false
	for _, e := range blocks {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if txt, ok := m["text"].(string); ok && (m["type"] == "text" || m["type"] == nil) {
			parts = append(parts, txt)
			found = true
		}
	}
	return strings.Join(parts, "\n"), found
}

// InputText renders a tool input for the excerpt: the Bash command, or the
// input as compact JSON.
func InputText(tool string, in any) string {
	if m, ok := in.(map[string]any); ok {
		if c, ok := m["command"].(string); ok && (tool == "Bash" || tool == "PowerShell") {
			return c
		}
	}
	if in == nil {
		return ""
	}
	if s, ok := in.(string); ok {
		return s
	}
	b, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return string(b)
}

// Excerpt caps text at ExcerptMax characters, keeping the head and the tail.
func Excerpt(s string) string {
	r := []rune(s)
	if len(r) <= ExcerptMax {
		return s
	}
	const mid = " … "
	keep := ExcerptMax - len([]rune(mid))
	head := keep * 3 / 4
	return string(r[:head]) + mid + string(r[len(r)-(keep-head):])
}

func capText(s string) string {
	if len(s) <= OutputMax {
		return s
	}
	h := OutputMax / 2
	for h > 0 && !isRuneStart(s[h]) {
		h--
	}
	t := len(s) - OutputMax/2
	for t < len(s) && !isRuneStart(s[t]) {
		t++
	}
	return s[:h] + "\n…\n" + s[t:]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// mcpServerFromTool extracts <server> from mcp__<server>__<tool>.
func mcpServerFromTool(tool string) string {
	rest, ok := strings.CutPrefix(tool, "mcp__")
	if !ok {
		return ""
	}
	server, _, ok := strings.Cut(rest, "__")
	if !ok {
		return ""
	}
	return server
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func warn(code, msg string) contract.Warning {
	return contract.Warning{Code: code, Message: redact.String(msg)}
}

func addSource(list []contract.SessionSourcesElem, s contract.SessionSourcesElem) []contract.SessionSourcesElem {
	if slices.Contains(list, s) {
		return list
	}
	list = append(list, s)
	slices.Sort(list)
	return list
}
