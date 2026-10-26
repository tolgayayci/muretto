package observe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/hook"
)

// maxLine is the longest transcript line parsed. Longer lines (huge pasted
// prompts, api-request-blob) are skipped, never fatal. A var for tests.
var maxLine = 16 << 20

const (
	// TestedVersionPrefix is the Claude Code version line the parser was
	// verified against; other versions parse but produce a warning.
	TestedVersionPrefix = "2.1."
)

// tLine holds the transcript keys Muretto reads. Everything else is ignored.
type tLine struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	SessionID        string          `json:"sessionId"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	Version          string          `json:"version"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	AgentID          string          `json:"agentId"`
	CustomTitle      string          `json:"customTitle"`
	AITitle          string          `json:"aiTitle"`
	TurnOrigin       string          `json:"turnOrigin"`
	Origin           json.RawMessage `json:"origin"`
	Message          json.RawMessage `json:"message"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
}

type tMessage struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type tBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

type pendingUse struct {
	sessionID, agentID, name string
	input                    any
	at                       time.Time
	order                    int
}

// ScanTranscripts reads Claude Code transcripts modified since the given time:
// <claudeDir>/projects/*/*.jsonl and <claudeDir>/projects/*/<session>/subagents/*.jsonl.
// A zero since reads everything. Unreadable files and lines become warnings.
func ScanTranscripts(claudeDir, developerID string, since time.Time) (Batch, error) {
	var b Batch
	if developerID == "" {
		return b, errors.New("developer id is empty")
	}
	root := filepath.Join(claudeDir, "projects")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return b, nil
	} else if err != nil {
		return b, fmt.Errorf("read transcripts: %w", err)
	}
	main, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	subs, _ := filepath.Glob(filepath.Join(root, "*", "*", "subagents", "*.jsonl"))
	files := append(main, subs...)
	sort.Strings(files)

	sc := newScanner(developerID)
	for _, f := range files {
		if strings.Contains(filepath.Base(f), ".orphaned-") {
			continue // set-aside copies; the live file has the same lines
		}
		fi, err := os.Stat(f)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if !since.IsZero() && fi.ModTime().Before(since) {
			continue
		}
		if err := sc.file(f); err != nil {
			sc.warnings = append(sc.warnings, warn("transcript_unreadable", fmt.Sprintf("cannot read transcript %s: %v", filepath.Base(f), err)))
		}
	}
	return sc.batch(), nil
}

// scanner accumulates sessions and actions across transcript files.
type scanner struct {
	developerID string
	sessions    map[string]*sessionAcc
	order       []string
	pending     map[string]*pendingUse // tool_use id -> call, until its result arrives
	actions     map[string]RawAction   // key: session + tool_use id
	actionKeys  []string
	versions    map[string]bool
	warnings    []contract.Warning
	seq         int
}

func newScanner(developerID string) *scanner {
	return &scanner{
		developerID: developerID,
		sessions:    map[string]*sessionAcc{},
		pending:     map[string]*pendingUse{},
		actions:     map[string]RawAction{},
		versions:    map[string]bool{},
	}
}

// file parses one JSONL transcript. Bad lines are counted, never fatal.
func (s *scanner) file(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// The main file's name is the session id; used when lines lack sessionId.
	fileSession := ""
	if filepath.Base(filepath.Dir(path)) != "subagents" {
		fileSession = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	// Pending uses are per file: a result always follows its call in the same file.
	s.pending = map[string]*pendingUse{}
	r := bufio.NewReaderSize(f, 256<<10)
	bad, long := 0, 0
	for {
		line, tooLong, err := readLine(r)
		if tooLong {
			long++
		} else if len(bytes.TrimSpace(line)) > 0 {
			var l tLine
			if jerr := json.Unmarshal(line, &l); jerr != nil {
				bad++
			} else {
				s.line(&l, fileSession)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	if bad > 0 || long > 0 {
		s.warnings = append(s.warnings, warn("transcript_lines_skipped",
			fmt.Sprintf("%s: skipped %d unparsable and %d oversized lines", filepath.Base(path), bad, long)))
	}
	return nil
}

// readLine reads one line, discarding (and flagging) lines over maxLine.
func readLine(r *bufio.Reader) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > maxLine {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return buf, tooLong, err
	}
}

func (s *scanner) acc(id string) *sessionAcc {
	a := s.sessions[id]
	if a == nil {
		a = &sessionAcc{id: id, source: contract.SessionSourcesElemTranscript}
		s.sessions[id] = a
		s.order = append(s.order, id)
	}
	return a
}

func (s *scanner) line(l *tLine, fileSession string) {
	sid := l.SessionID
	if sid == "" {
		sid = fileSession
	}
	if sid == "" || len(sid) > 128 {
		return
	}
	at := parseTime(l.Timestamp)
	switch l.Type {
	case "custom-title":
		if l.CustomTitle != "" {
			s.acc(sid).customTitle = l.CustomTitle // the latest rename wins
		}
		return
	case "ai-title":
		if l.AITitle != "" {
			s.acc(sid).aiTitle = l.AITitle
		}
		return
	case "user", "assistant":
	default:
		return // attachment, system, queue-operation, cost-state, unknown future types...
	}
	a := s.acc(sid)
	a.seen(at)
	if a.cwd == "" && l.Cwd != "" && !l.IsSidechain {
		a.cwd = l.Cwd
	}
	if l.GitBranch != "" && !l.IsSidechain {
		a.branch = l.GitBranch // latest wins: the branch can change mid-session
	}
	if l.Version != "" {
		a.version = l.Version
		s.versions[l.Version] = true
	}
	var msg tMessage
	if len(l.Message) == 0 || json.Unmarshal(l.Message, &msg) != nil {
		return
	}
	if l.Type == "assistant" {
		if msg.Model != "" && !strings.HasPrefix(msg.Model, "<") && !l.IsSidechain {
			a.model = msg.Model
		}
		for _, blk := range blocks(msg.Content) {
			if blk.Type != "tool_use" || blk.ID == "" {
				continue
			}
			s.seq++
			s.pending[blk.ID] = &pendingUse{sessionID: sid, agentID: agentID(l), name: blk.Name,
				input: decodeAny(blk.Input), at: at, order: s.seq}
		}
		return
	}
	// user line: a human prompt or tool results.
	bl := blocks(msg.Content)
	results := 0
	for _, blk := range bl {
		if blk.Type == "tool_result" {
			results++
		}
	}
	if results == 0 {
		s.prompt(a, l, msg.Content, bl, at)
		return
	}
	for _, blk := range bl {
		if blk.Type != "tool_result" || blk.ToolUseID == "" {
			continue
		}
		use := s.pending[blk.ToolUseID]
		if use == nil {
			continue // result without its call in this file (e.g. compacted away)
		}
		delete(s.pending, blk.ToolUseID)
		var out string
		if results == 1 && len(l.ToolUseResult) > 0 && !blk.IsError {
			out = OutputText(use.name, decodeAny(l.ToolUseResult))
		}
		if out == "" {
			out = contentText(blk.Content)
		}
		status := contract.AgentActionStatusOk
		if blk.IsError {
			status = contract.AgentActionStatusError
		}
		when := at
		if when.IsZero() {
			when = use.at
		}
		key := use.sessionID + "\x00" + blk.ToolUseID
		if _, dup := s.actions[key]; dup {
			continue
		}
		s.actions[key] = newRawAction(blk.ToolUseID, use.sessionID, use.agentID, when, use.name,
			mcpServerFromTool(use.name), status, contract.AgentActionSourceTranscript, use.input, out)
		s.actionKeys = append(s.actionKeys, key)
	}
}

// prompt records the first real human prompt of a session.
func (s *scanner) prompt(a *sessionAcc, l *tLine, raw json.RawMessage, bl []tBlock, at time.Time) {
	if a.firstPrompt != "" || l.IsSidechain || l.IsMeta || l.IsCompactSummary {
		return
	}
	if l.TurnOrigin == "task_notification" || l.TurnOrigin == "peer" {
		return
	}
	if len(l.Origin) > 0 {
		var o struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(l.Origin, &o) == nil && o.Kind != "" && o.Kind != "human" {
			return
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		for _, blk := range bl {
			if blk.Type == "text" && !isSyntheticPrompt(blk.Text) {
				text = blk.Text
				break
			}
		}
	}
	if isSyntheticPrompt(text) {
		return
	}
	if t, ok := hook.TitleCandidate(text); ok {
		a.firstPrompt = t
		a.firstPromptAt = at
	}
}

// syntheticTags start prompts that Claude Code injects, not the human.
var syntheticTags = []string{
	"<task-notification>", "<command-name>", "<command-message>", "<command-args>",
	"<local-command-stdout>", "<local-command-stderr>", "<local-command-caveat>",
	"<system-reminder>", "<user-prompt-submit-hook>", "<bash-input>", "<bash-stdout>", "<bash-stderr>",
}

func isSyntheticPrompt(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return true
	}
	for _, tag := range syntheticTags {
		if strings.HasPrefix(t, tag) {
			return true
		}
	}
	return false
}

func agentID(l *tLine) string {
	if l.IsSidechain {
		return l.AgentID
	}
	return ""
}

func blocks(content json.RawMessage) []tBlock {
	if len(content) == 0 || content[0] != '[' {
		return nil
	}
	var bl []tBlock
	if json.Unmarshal(content, &bl) != nil {
		// One odd block shouldn't hide the others: decode them one by one.
		var raws []json.RawMessage
		if json.Unmarshal(content, &raws) != nil {
			return nil
		}
		for _, r := range raws {
			var b tBlock
			if json.Unmarshal(r, &b) == nil {
				bl = append(bl, b)
			}
		}
	}
	return bl
}

// contentText renders tool_result content (a string or a content array).
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return OutputText("", decodeAny(raw))
}

func decodeAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	return v
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func (s *scanner) batch() Batch {
	var b Batch
	repos := newRepoCache()
	for _, id := range s.order {
		a := s.sessions[id]
		if a.started.IsZero() {
			continue // only title lines, no messages: nothing to anchor the session
		}
		b.Sessions = append(b.Sessions, a.session(s.developerID, repos))
	}
	for _, k := range s.actionKeys {
		b.Actions = append(b.Actions, s.actions[k])
	}
	for v := range s.versions {
		if !strings.HasPrefix(v, TestedVersionPrefix) {
			b.Warnings = append(b.Warnings, warn("transcript_version_untested",
				fmt.Sprintf("transcripts from Claude Code %s were parsed, but only %sx is tested", v, TestedVersionPrefix)))
		}
	}
	sort.Slice(b.Warnings, func(i, j int) bool { return b.Warnings[i].Message < b.Warnings[j].Message })
	b.Warnings = append(s.warnings, b.Warnings...)
	// Within transcripts, a fork copies its parent's tool calls: dedupe here too.
	return Merge(b)
}
