package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
	"github.com/tolgayayci/muretto/schema"
)

const fixtures = "../../docs/research/fixtures/claude-code/"

var now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func newPaths(t testing.TB) paths.Paths {
	t.Helper()
	p := paths.At(filepath.Join(t.TempDir(), "home"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// spoolFiles returns the decoded records in the spool, failing on temp files.
func spoolFiles(t *testing.T, p paths.Paths) []contract.SpoolRecord {
	t.Helper()
	entries, err := os.ReadDir(p.Spool)
	if err != nil {
		t.Fatal(err)
	}
	nameRE := regexp.MustCompile(`^\d+-\d+-[0-9a-f]{16}\.json$`)
	var recs []contract.SpoolRecord
	for _, e := range entries {
		if !nameRE.MatchString(e.Name()) {
			t.Errorf("unexpected spool entry %q", e.Name())
			continue
		}
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v, want 0600", e.Name(), fi.Mode().Perm())
		}
		raw, err := os.ReadFile(filepath.Join(p.Spool, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		checkSchemaKeys(t, raw)
		var r contract.SpoolRecord
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		recs = append(recs, r)
	}
	return recs
}

// checkSchemaKeys verifies the record only uses properties the schema allows
// (additionalProperties is false) and has the required ones.
func checkSchemaKeys(t *testing.T, raw []byte) {
	t.Helper()
	sb, err := schema.Files.ReadFile("spool-record.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var sch struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(sb, &sch); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for k := range got {
		if _, ok := sch.Properties[k]; !ok {
			t.Errorf("spool record has key %q not in schema", k)
		}
	}
	for _, k := range sch.Required {
		if _, ok := got[k]; !ok {
			t.Errorf("spool record lacks required key %q", k)
		}
	}
}

func runHook(t *testing.T, p paths.Paths, event string, stdin []byte) []contract.SpoolRecord {
	t.Helper()
	Run(event, bytes.NewReader(stdin), p, now)
	return spoolFiles(t, p)
}

func TestBashFixture(t *testing.T) {
	p := newPaths(t)
	recs := runHook(t, p, "PostToolUse", readFixture(t, "hook-posttooluse-bash.json"))
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.Event != "PostToolUse" || r.SessionID != "0b6e3c1a-7f2d-4c55-9a10-3e8d2f4b6a01" || *r.ToolName != "Bash" ||
		*r.ToolUseID != "toolu_01FAKEbashAAAAAAAAAAAAAAA" || *r.DurationMs != 700 || *r.Cwd != "/home/dev/acme-app" ||
		*r.PromptID != "5d1f7e2a-0c3b-4e8f-b1a2-9c7d6e5f4a30" || r.SpoolVersion != 1 || !r.ReceivedAt.Equal(now) {
		t.Errorf("unexpected record: %+v", r)
	}
	resp := r.ToolResponse.(map[string]any)
	if resp["stdout"] != "muretto-test-ref abcdefghijklmnopqrst" {
		t.Errorf("stdout = %v", resp["stdout"])
	}
	if r.Truncated != nil || len(r.ExtraKeys) != 0 {
		t.Errorf("truncated=%v extra=%v", r.Truncated, r.ExtraKeys)
	}
	if _, err := os.Stat(p.HookLog); err == nil {
		t.Error("hook-errors.log written on success")
	}
}

func TestMCPFixture(t *testing.T) {
	p := newPaths(t)
	recs := runHook(t, p, "PostToolUse", readFixture(t, "hook-posttooluse-mcp.json"))
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.McpServer == nil || *r.McpServer != "fakeinfra" || *r.ToolName != "mcp__fakeinfra__create_project" {
		t.Errorf("mcp: %+v", r)
	}
	arr, ok := r.ToolResponse.([]any)
	if !ok || len(arr) != 1 || !strings.Contains(arr[0].(map[string]any)["text"].(string), "zzfakeref000000000000") {
		t.Errorf("tool_response = %#v", r.ToolResponse)
	}
}

func TestEventFallsBackToStdin(t *testing.T) {
	p := newPaths(t)
	recs := runHook(t, p, "", []byte(`{"session_id":"s1","hook_event_name":"SessionEnd","reason":"other"}`))
	if len(recs) != 1 || recs[0].Event != "SessionEnd" || *recs[0].Reason != "other" {
		t.Fatalf("%+v", recs)
	}
}

func TestSessionStartAndSubagent(t *testing.T) {
	p := newPaths(t)
	Run("SessionStart", strings.NewReader(`{"session_id":"s1","source":"startup","session_title":"waitlist-demo","model":"claude-x","transcript_path":"/home/dev/.claude/projects/p/s1.jsonl"}`), p, now)
	Run("SubagentStart", strings.NewReader(`{"session_id":"s1","agent_id":"a5d7fake","agent_type":"general-purpose","brand_new_key":1}`), p, now.Add(time.Second))
	recs := spoolFiles(t, p)
	if len(recs) != 2 {
		t.Fatalf("got %d", len(recs))
	}
	for _, r := range recs {
		switch r.Event {
		case "SessionStart":
			if *r.Source != "startup" || *r.SessionTitle != "waitlist-demo" || *r.TranscriptPath == "" {
				t.Errorf("%+v", r)
			}
		case "SubagentStart":
			if *r.AgentID != "a5d7fake" || *r.AgentType != "general-purpose" || len(r.ExtraKeys) != 1 || r.ExtraKeys[0] != "brand_new_key" {
				t.Errorf("%+v", r)
			}
		default:
			t.Errorf("event %q", r.Event)
		}
	}
}

func TestTitleCandidate(t *testing.T) {
	long := strings.Repeat("word ", 40)
	sbp := "sbp_" + strings.Repeat("0a", 20)
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Add a waitlist signup table", "Add a waitlist signup table", true},
		{"\n\n  First line\nsecond line with secrets", "First line", true},
		{"<task-notification>\n<task-id>x</task-id>", "", false},
		{"   ", "", false},
		{"deploy with token " + sbp + " please", "deploy with token " + redact.Placeholder + " please", true},
		{long, strings.TrimSpace(long[:80]), true},
		{"héllo wörld ✓ " + strings.Repeat("é", 100), "héllo wörld ✓ " + strings.Repeat("é", 66), true},
	}
	for _, tc := range tests {
		got, ok := TitleCandidate(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("TitleCandidate(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
		if len([]rune(got)) > TitleMax {
			t.Errorf("title too long: %d", len([]rune(got)))
		}
	}
}

func TestUserPromptStoresOnlyTitle(t *testing.T) {
	p := newPaths(t)
	prompt := "Create the customers table\nHere is the full secret context that must never be stored: password=hunter2xyz"
	in, _ := json.Marshal(map[string]any{"session_id": "s1", "prompt": prompt, "prompt_id": "p1"})
	recs := runHook(t, p, "UserPromptSubmit", in)
	if len(recs) != 1 || recs[0].TitleCandidate == nil || *recs[0].TitleCandidate != "Create the customers table" {
		t.Fatalf("%+v", recs)
	}
	raw, _ := os.ReadDir(p.Spool)
	b, _ := os.ReadFile(filepath.Join(p.Spool, raw[0].Name()))
	if strings.Contains(string(b), "full secret context") || strings.Contains(string(b), "hunter2") {
		t.Error("full prompt leaked into spool")
	}
}

func TestTaskNotificationPromptHasNoTitle(t *testing.T) {
	p := newPaths(t)
	recs := runHook(t, p, "UserPromptSubmit", []byte(`{"session_id":"s1","prompt":"<task-notification>\nsubagent done</task-notification>"}`))
	if len(recs) != 1 || recs[0].TitleCandidate != nil {
		t.Fatalf("%+v", recs)
	}
}

func TestRedactsBeforeWriting(t *testing.T) {
	p := newPaths(t)
	pw := "Zx" + strings.Repeat("9q", 8)
	sbp := "sbp_" + strings.Repeat("1b", 20)
	in, _ := json.Marshal(map[string]any{
		"session_id": "s1", "tool_name": "Bash", "tool_use_id": "toolu_1",
		"tool_input":    map[string]any{"command": "supabase link --project-ref abcdefghijklmnopqrst --db-password " + pw, "password": pw},
		"tool_response": map[string]any{"stdout": "token " + sbp + "\npostgres://postgres:" + pw + "@db.abcdefghijklmnopqrst.supabase.co/postgres", "stderr": ""},
	})
	runHook(t, p, "PostToolUse", in)
	entries, _ := os.ReadDir(p.Spool)
	b, _ := os.ReadFile(filepath.Join(p.Spool, entries[0].Name()))
	for _, s := range []string{pw, sbp} {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("secret %q in spool file: %s", s, b)
		}
	}
	if !bytes.Contains(b, []byte("abcdefghijklmnopqrst")) {
		t.Error("project ref lost")
	}
}

func TestFailureEvent(t *testing.T) {
	p := newPaths(t)
	recs := runHook(t, p, "PostToolUseFailure", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"toolu_2","tool_input":{"command":"supabase projects create x"},"error":"Exit code 1\nproject ref abcdefghijklmnopqrst created, then failed","is_interrupt":false,"duration_ms":12}`))
	if len(recs) != 1 || recs[0].Error == nil || !strings.Contains(*recs[0].Error, "abcdefghijklmnopqrst") || len(recs[0].ExtraKeys) != 0 {
		t.Fatalf("%+v", recs)
	}
}

func TestFieldCap(t *testing.T) {
	p := newPaths(t)
	big := "HEAD-ref-abcdefghijklmnopqrst " + strings.Repeat("x ", 100<<10) + " TAIL-dpl_FakeDeploy123"
	in, _ := json.Marshal(map[string]any{"session_id": "s1", "tool_use_id": "toolu_3", "tool_response": map[string]any{"stdout": big}, "error": big})
	recs := runHook(t, p, "PostToolUse", in)
	r := recs[0]
	out := r.ToolResponse.(map[string]any)["stdout"].(string)
	if len(out) > MaxField || len(*r.Error) > MaxField {
		t.Errorf("field not capped: %d / %d", len(out), len(*r.Error))
	}
	if !strings.HasPrefix(out, "HEAD-ref-abcdefghijklmnopqrst") || !strings.HasSuffix(out, "TAIL-dpl_FakeDeploy123") {
		t.Error("head or tail lost")
	}
	if r.Truncated == nil || !*r.Truncated {
		t.Error("truncated flag not set")
	}
}

func TestCapNeverSplitsSecrets(t *testing.T) {
	// A token straddling the head cut must not leave a recognisable-less piece.
	tok := "sbp_" + strings.Repeat("7c", 20)
	for off := -60; off <= 60; off += 3 {
		pad := (MaxField-len("\n…[truncated by muretto]…\n"))/2 + off
		s := strings.Repeat("a", pad) + " " + tok + " " + strings.Repeat("b", 200<<10)
		capped, _ := capString(s)
		red := redact.String(capped)
		if strings.Contains(red, tok[:12]) {
			t.Fatalf("offset %d: token prefix survived capping+redaction", off)
		}
	}
	// A private key whose BEGIN line falls in the dropped middle.
	key := "-----BEGIN PRIVATE " + "KEY-----\n" + strings.Repeat("QUJD\n", 30<<10) + "-----END PRIVATE " + "KEY-----\nafter"
	capped, _ := capString(key)
	red := redact.String(capped)
	if strings.Contains(red, "QUJD") {
		t.Error("PEM body survived")
	}
	if !strings.HasSuffix(red, "after") {
		t.Error("text after PEM lost")
	}
}

func TestOversizedStdin(t *testing.T) {
	p := newPaths(t)
	big := "ref abcdefghijklmnopqrst\n" + strings.Repeat("line of output\n", (2<<20)/15)
	in, _ := json.Marshal(map[string]any{"session_id": "s1", "tool_name": "Bash", "tool_input": map[string]any{"command": "cat big"}})
	// tool_response then tool_use_id, the order Claude Code uses.
	in = append(in[:len(in)-1], []byte(`,"tool_response":{"stdout":`+mustJSON(big)+`},"tool_use_id":"toolu_after_cut","duration_ms":9}`)...)
	if len(in) <= MaxStdin {
		t.Fatal("test input too small")
	}
	recs := runHook(t, p, "PostToolUse", in)
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.ToolUseID == nil || *r.ToolUseID != "toolu_after_cut" || r.DurationMs == nil || *r.DurationMs != 9 {
		t.Errorf("scalars after the cut not recovered: %+v", r)
	}
	s, ok := r.ToolResponse.(string)
	if !ok || !strings.Contains(s, "abcdefghijklmnopqrst") || len(s) > MaxField {
		t.Errorf("partial tool_response: %T len %d", r.ToolResponse, len(s))
	}
	if r.Truncated == nil || !*r.Truncated {
		t.Error("truncated not set")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// garbage covers inputs a broken or future Claude Code could send.
var garbage = [][]byte{
	nil,
	[]byte(""),
	[]byte("null"),
	[]byte("[]"),
	[]byte("{"),
	[]byte(`{"session_id":`),
	[]byte(`{"session_id":5}`),
	[]byte(`{"session_id":""}`),
	[]byte(`{"session_id":"` + strings.Repeat("x", 200) + `"}`),
	[]byte("\x00\xff\xfe garbage"),
	[]byte(`{"session_id":"s","tool_input":` + strings.Repeat("[", 10000) + strings.Repeat("]", 10000) + `}`),
	[]byte(strings.Repeat("{", 100000)),
	[]byte(`{"session_id":"s","tool_response":"\ud800 bad surrogate","duration_ms":"NaN","mcp_server":[1,2]}`),
	[]byte(`{"session_id":"s","duration_ms":1e300,"tool_input":{"a":` + strings.Repeat(`{"a":`, 200) + `1` + strings.Repeat(`}`, 200) + `}}`),
	bytes.Repeat([]byte{0xff}, MaxStdin+10),
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("boom") }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestGarbageNeverPanicsOrPrints(t *testing.T) {
	p := newPaths(t)
	stdout, stderr := captureStdio(t, func() {
		for _, g := range garbage {
			Run("PostToolUse", bytes.NewReader(g), p, now)
		}
		Run("PostToolUse", panicReader{}, p, now)
		Run("PostToolUse", errReader{}, p, now)
		Run("PostToolUse", nil, p, now)
		Run("PostToolUse", strings.NewReader(`{"session_id":"s"}`), paths.Paths{}, now)
		// Valid input too: success must be silent as well.
		Run("PostToolUse", bytes.NewReader(readFixture(t, "hook-posttooluse-bash.json")), p, now)
	})
	if stdout != "" || stderr != "" {
		t.Errorf("hook printed: stdout=%q stderr=%q", stdout, stderr)
	}
	logb, err := os.ReadFile(p.HookLog)
	if err != nil {
		t.Fatal("no hook-errors.log written for garbage input")
	}
	if !strings.Contains(string(logb), "panic: boom") {
		t.Error("panic not logged")
	}
	fi, _ := os.Stat(p.HookLog)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", fi.Mode().Perm())
	}
	// Every spool file that was written must be valid JSON with a session id.
	for _, r := range spoolFiles(t, p) {
		if r.SessionID == "" {
			t.Error("record without session id")
		}
	}
}

func TestUnwritableSpool(t *testing.T) {
	p := newPaths(t)
	if err := os.Chmod(p.Spool, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p.Spool, 0o700)
	stdout, stderr := captureStdio(t, func() {
		Run("PostToolUse", bytes.NewReader(readFixture(t, "hook-posttooluse-bash.json")), p, now)
	})
	if stdout != "" || stderr != "" {
		t.Error("printed output")
	}
	b, _ := os.ReadFile(p.HookLog)
	if !strings.Contains(string(b), "spool") {
		t.Errorf("error not logged: %q", b)
	}
}

func TestConcurrentWritesAreUnique(t *testing.T) {
	p := newPaths(t)
	in := readFixture(t, "hook-posttooluse-bash.json")
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Run("PostToolUse", bytes.NewReader(in), p, now) // same timestamp on purpose
		}()
	}
	wg.Wait()
	if n := len(spoolFiles(t, p)); n != 64 {
		t.Errorf("got %d files, want 64", n)
	}
}

// captureStdio swaps os.Stdout and os.Stderr for pipes while f runs.
func captureStdio(t *testing.T, f func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	var bo, be bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&bo, ro); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&be, re); done <- struct{}{} }()
	func() {
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		f()
	}()
	_ = wo.Close()
	_ = we.Close()
	<-done
	<-done
	return bo.String(), be.String()
}

func BenchmarkRunBashFixture(b *testing.B) {
	p := newPaths(b)
	in := readFixture(b, "hook-posttooluse-bash.json")
	for b.Loop() {
		Run("PostToolUse", bytes.NewReader(in), p, time.Now())
	}
}

func BenchmarkRun64KiBOutput(b *testing.B) {
	p := newPaths(b)
	var sb strings.Builder
	for sb.Len() < 64<<10 {
		sb.WriteString(`{"id":"prj_FakeProject1234567890abcd","ref":"abcdefghijklmnopqrst","url":"https://web-abc.vercel.app"}` + "\n")
	}
	in, _ := json.Marshal(map[string]any{"session_id": "s1", "tool_name": "Bash", "tool_use_id": "toolu_1",
		"tool_input": map[string]any{"command": "vercel ls --json"}, "tool_response": map[string]any{"stdout": sb.String(), "stderr": ""}})
	b.SetBytes(int64(len(in)))
	for b.Loop() {
		Run("PostToolUse", bytes.NewReader(in), p, time.Now())
	}
}
