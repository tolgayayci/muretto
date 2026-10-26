// Package hook implements `muretto hook <event>`: it writes one redacted,
// size-capped spool file and returns. It never blocks or alters the agent.
//
// What it does, in order: read stdin (first 1 MiB kept, the rest drained),
// parse the top-level keys tolerantly, cap every text field at 64 KiB (head +
// tail), redact, and write one SpoolRecord as a 0600 file via temp + rename.
// No network, no locks, no stdout/stderr. Errors go to hook-errors.log.
package hook

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
)

const (
	// MaxStdin is how much of stdin is parsed. The rest is drained and only
	// its last TailKeep bytes are kept, to recover scalars such as tool_use_id
	// that Claude Code sends after a huge tool_response.
	MaxStdin = 1 << 20
	// MaxField caps each text field (head + tail are kept).
	MaxField = 64 << 10
	// TitleMax is the length of title_candidate, in characters.
	TitleMax = 80

	tailKeep     = 64 << 10
	maxDrain     = 64 << 20 // stop reading after this much; never block on an endless pipe
	maxLogSize   = 1 << 20
	titleMaxRead = 4096 // only the start of the prompt is ever looked at
)

// knownKeys are the top-level stdin keys this version understands (stored or
// knowingly dropped). Anything else is listed by name in extra_keys.
var knownKeys = map[string]bool{
	"session_id": true, "transcript_path": true, "cwd": true, "hook_event_name": true,
	"prompt_id": true, "permission_mode": true, "effort": true, "scratchpad_dir": true,
	"agent_id": true, "agent_type": true, "source": true, "model": true, "session_title": true,
	"prompt": true, "tool_name": true, "tool_input": true, "tool_use_id": true,
	"tool_response": true, "duration_ms": true, "mcp_server": true, "error": true,
	"is_interrupt": true, "reason": true, "seconds_since_last_response": true,
	"context_tokens": true, "prompt_cache_likely_expired": true, "estimated_cache_write_usd": true,
}

// Run never returns an error, never writes to stdout or stderr and never
// touches the network. The caller always exits 0.
func Run(event string, stdin io.Reader, p paths.Paths, now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			logError(p, now, event, fmt.Sprintf("panic: %v", r))
		}
	}()
	if err := run(event, stdin, p, now); err != nil {
		logError(p, now, event, err.Error())
	}
}

func run(event string, stdin io.Reader, p paths.Paths, now time.Time) error {
	if p.Spool == "" {
		return errors.New("spool directory not set")
	}
	if stdin == nil {
		return errors.New("no stdin")
	}
	head, tail, overflow, err := readCapped(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	rec, err := build(event, head, tail, overflow, now)
	if err != nil {
		return err
	}
	return writeSpool(p.Spool, rec, now)
}

// readCapped returns the first MaxStdin bytes, and the last tailKeep bytes of
// whatever followed (overflow reports whether anything did).
func readCapped(r io.Reader) (head, tail []byte, overflow bool, err error) {
	head, err = io.ReadAll(io.LimitReader(r, MaxStdin))
	if err != nil || len(head) < MaxStdin {
		return head, nil, false, err
	}
	buf := make([]byte, 32<<10)
	ring := make([]byte, 0, 2*tailKeep)
	var total int64
	for total < maxDrain {
		n, rerr := r.Read(buf)
		if n > 0 {
			overflow = true
			total += int64(n)
			ring = append(ring, buf[:n]...)
			if len(ring) > tailKeep {
				ring = append(ring[:0], ring[len(ring)-tailKeep:]...)
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return head, ring, overflow, rerr
		}
	}
	return head, ring, overflow, nil
}

// build turns raw hook stdin into a redacted, capped SpoolRecord.
func build(event string, head, tail []byte, overflow bool, now time.Time) (*contract.SpoolRecord, error) {
	fields, order, cut, err := parseTopLevel(head)
	if err != nil {
		return nil, err
	}
	truncated := overflow || cut != ""
	if overflow {
		recoverScalars(fields, tail)
	}

	str := func(k string) *string {
		raw, ok := fields[k]
		if !ok {
			return nil
		}
		var s string
		if json.Unmarshal(raw, &s) != nil || s == "" {
			return nil
		}
		return &s
	}

	if event == "" {
		if e := str("hook_event_name"); e != nil {
			event = *e
		}
	}
	if event == "" || len(event) > 64 {
		return nil, errors.New("missing or invalid event name")
	}
	sid := str("session_id")
	if sid == nil || len(*sid) > 128 {
		return nil, errors.New("stdin has no usable session_id")
	}

	rec := &contract.SpoolRecord{
		SpoolVersion:   1,
		ReceivedAt:     now.UTC(),
		Event:          event,
		SessionID:      *sid,
		TranscriptPath: str("transcript_path"),
		Cwd:            str("cwd"),
		PromptID:       str("prompt_id"),
		AgentID:        str("agent_id"),
		AgentType:      str("agent_type"),
		Source:         str("source"),
		Reason:         str("reason"),
		ToolName:       str("tool_name"),
		ToolUseID:      str("tool_use_id"),
	}
	if t := str("session_title"); t != nil {
		s := truncateRunes(redact.String(*t), 200)
		rec.SessionTitle = &s
	}
	if m := str("model"); m != nil {
		s := truncateRunes(*m, 100)
		rec.Model = &s
	}
	if pr := str("prompt"); pr != nil {
		if title, ok := TitleCandidate(*pr); ok {
			rec.TitleCandidate = &title
		}
	}
	if raw, ok := fields["mcp_server"]; ok {
		rec.McpServer = mcpServerName(raw)
	}
	if raw, ok := fields["duration_ms"]; ok {
		var f float64
		if json.Unmarshal(raw, &f) == nil && f >= 0 && f < 1e12 {
			d := int(f)
			rec.DurationMs = &d
		}
	}
	if e := str("error"); e != nil {
		s, c := capString(*e)
		truncated = truncated || c
		s = redact.String(s)
		rec.Error = &s
	}
	if raw, ok := fields["tool_input"]; ok {
		v, c := decodeCapped(raw)
		truncated = truncated || c
		rec.ToolInput = redact.Value(v)
	}
	if raw, ok := fields["tool_response"]; ok {
		v, c := decodeCapped(raw)
		truncated = truncated || c
		rec.ToolResponse = redact.Value(v)
	} else if cut == "tool_response" || cut == "tool_input" {
		// The value was cut by the stdin cap: keep its start as text so the
		// ids printed early in a huge output are not lost.
		partial, _ := capString(strings.ToValidUTF8(string(fields["\x00partial"]), ""))
		if cut == "tool_response" {
			rec.ToolResponse = redact.String(partial)
		} else {
			rec.ToolInput = redact.String(partial)
		}
	}
	if os.Getenv("CLAUDE_CODE_CHILD_SESSION") != "" {
		t := true
		rec.ChildSession = &t
	}
	if truncated {
		t := true
		rec.Truncated = &t
	}
	for _, k := range order {
		if !knownKeys[k] && len(rec.ExtraKeys) < 50 && len(k) <= 100 {
			rec.ExtraKeys = append(rec.ExtraKeys, k)
		}
	}
	slices.Sort(rec.ExtraKeys)
	return rec, nil
}

// parseTopLevel decodes the top-level object key by key. When the input ends
// early (stdin cap), the keys decoded so far are returned, the name of the key
// being decoded is returned as cut, and its partial raw text is stored under
// the "\x00partial" key.
func parseTopLevel(data []byte) (fields map[string]json.RawMessage, order []string, cut string, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, "", fmt.Errorf("stdin is not JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, "", errors.New("stdin is not a JSON object")
	}
	fields = make(map[string]json.RawMessage, 24)
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return partialOrErr(fields, order, "", nil, len(data) >= MaxStdin, err)
		}
		key, ok := kt.(string)
		if !ok {
			return nil, nil, "", errors.New("stdin has a non-string key")
		}
		start := dec.InputOffset()
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			rest := bytes.TrimLeft(data[start:], " \t\r\n:")
			return partialOrErr(fields, order, key, rest, len(data) >= MaxStdin, err)
		}
		if _, dup := fields[key]; !dup {
			order = append(order, key)
		}
		fields[key] = raw
	}
	return fields, order, "", nil
}

func partialOrErr(fields map[string]json.RawMessage, order []string, key string, rest []byte, capped bool, err error) (map[string]json.RawMessage, []string, string, error) {
	if !capped || len(fields) == 0 {
		return nil, nil, "", fmt.Errorf("stdin is not valid JSON: %w", err)
	}
	if key != "" {
		fields["\x00partial"] = rest
		return fields, order, key, nil
	}
	return fields, order, "stdin", nil
}

var tailScalar = regexp.MustCompile(`"(tool_use_id|session_id|tool_name|duration_ms|agent_id|agent_type|prompt_id|cwd|transcript_path|hook_event_name)"\s*:\s*("(?:[^"\\]|\\.){0,1000}"|-?[0-9]{1,15})`)

// recoverScalars fills scalar fields that were beyond the stdin cap from the
// drained tail of the input.
func recoverScalars(fields map[string]json.RawMessage, tail []byte) {
	for _, m := range tailScalar.FindAllSubmatch(tail, -1) {
		k := string(m[1])
		if _, ok := fields[k]; !ok {
			fields[k] = json.RawMessage(slices.Clone(m[2]))
		}
	}
}

// mcpServerName accepts {"name": "..."} (observed) or a plain string.
func mcpServerName(raw json.RawMessage) *string {
	var obj struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Name != "" {
		s := truncateRunes(obj.Name, 200)
		return &s
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		s = truncateRunes(s, 200)
		return &s
	}
	return nil
}

// TitleCandidate returns the redacted first line of a prompt, at most TitleMax
// characters. It returns false for synthetic <task-notification> turns and
// prompts with no text.
func TitleCandidate(prompt string) (string, bool) {
	if len(prompt) > titleMaxRead {
		prompt = prompt[:titleMaxRead]
	}
	prompt = strings.TrimLeft(prompt, " \t\r\n")
	if prompt == "" || strings.HasPrefix(prompt, "<task-notification>") {
		return "", false
	}
	line, _, _ := strings.Cut(prompt, "\n")
	line = strings.TrimSpace(strings.ToValidUTF8(line, ""))
	if line == "" {
		return "", false
	}
	// Redact the whole first line before cutting, so a secret is never split
	// into a piece the redactor no longer recognises.
	return strings.TrimSpace(truncateRunes(redact.String(line), TitleMax)), true
}

// decodeCapped decodes a JSON value and caps every string in it.
func decodeCapped(raw json.RawMessage) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		s, c := capString(string(raw))
		return s, c
	}
	cut := false
	v = capStrings(v, &cut, 0)
	return v, cut
}

func capStrings(v any, cut *bool, depth int) any {
	switch t := v.(type) {
	case string:
		s, c := capString(t)
		*cut = *cut || c
		return s
	case map[string]any:
		if depth > 64 {
			*cut = true
			return nil
		}
		for k, e := range t {
			t[k] = capStrings(e, cut, depth+1)
		}
		return t
	case []any:
		if depth > 64 {
			*cut = true
			return nil
		}
		for i, e := range t {
			t[i] = capStrings(e, cut, depth+1)
		}
		return t
	default:
		return v
	}
}

// capString keeps the head and tail of s within MaxField bytes. Cuts are moved
// to a separator so no token is split (a split secret could escape the
// redactor), and a private key block whose BEGIN line was dropped is removed.
func capString(s string) (string, bool) {
	if len(s) <= MaxField {
		return s, false
	}
	const marker = "\n…[truncated by muretto]…\n"
	half := (MaxField - len(marker)) / 2
	h := snapBack(s, half)
	t := snapForward(s, len(s)-half)
	tail := s[t:]
	if end := strings.Index(tail, "-----END "); end >= 0 && !strings.Contains(tail[:end], "-----BEGIN ") {
		tail = redact.Placeholder + "\n" + tail[end:] // drop the key body, keep the END line
	}
	return s[:h] + marker + tail, true
}

func isSep(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', ',', ';', '&', '|', '(', ')', '[', ']', '{', '}', '<', '>':
		return true
	}
	return false
}

// snapBack returns a cut position <= i at a separator (or a rune boundary if
// none is near).
func snapBack(s string, i int) int {
	for j := i; j > 0 && i-j < 512; j-- {
		if isSep(s[j-1]) {
			return j
		}
	}
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func snapForward(s string, i int) int {
	for j := i; j < len(s) && j-i < 512; j++ {
		if isSep(s[j]) {
			return j
		}
	}
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := 0; k < n; k++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// writeSpool writes rec as <unix_nanos>-<pid>-<rand>.json, atomically, 0600.
func writeSpool(dir string, rec *contract.SpoolRecord, now time.Time) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return fmt.Errorf("encode spool record: %w", err)
	}
	if err := os.MkdirAll(dir, paths.DirPerm); err != nil {
		return fmt.Errorf("create spool dir: %w", err)
	}
	var rb [8]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return fmt.Errorf("random name: %w", err)
	}
	name := fmt.Sprintf("%d-%d-%s.json", now.UnixNano(), os.Getpid(), hex.EncodeToString(rb[:]))
	tmp := filepath.Join(dir, ".tmp-"+name)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, paths.FilePerm)
	if err != nil {
		return fmt.Errorf("create spool file: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write spool file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close spool file: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename spool file: %w", err)
	}
	return nil
}

// logError appends one line to hook-errors.log. It never fails loudly: if
// even the log can't be written, the error is dropped.
func logError(p paths.Paths, now time.Time, event, msg string) {
	defer func() { _ = recover() }()
	if p.HookLog == "" {
		return
	}
	if fi, err := os.Stat(p.HookLog); err == nil && fi.Size() > maxLogSize {
		_ = os.Rename(p.HookLog, p.HookLog+".1")
	}
	_ = os.MkdirAll(filepath.Dir(p.HookLog), paths.DirPerm)
	f, err := os.OpenFile(p.HookLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, paths.FilePerm)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf("%s hook %s: %s\n", now.UTC().Format(time.RFC3339), truncateRunes(event, 64),
		truncateRunes(redact.String(strings.ReplaceAll(msg, "\n", " ")), 500))
	_, _ = f.WriteString(line)
}
