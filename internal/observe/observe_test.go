package observe

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/hook"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
)

const (
	dev        = "dev-fake-0001"
	salt       = "path-salt-fake-0001"
	researchFx = "../../docs/research/fixtures/claude-code/"
	sessA      = "aaaaaaaa-1111-4111-8111-000000000001"
	sessB      = "bbbbbbbb-2222-4222-8222-000000000002"
	sessDoc    = "0b6e3c1a-7f2d-4c55-9a10-3e8d2f4b6a01"
	bash1      = "toolu_01FAKEparentBash000000001"
)

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// claudeDir builds a fake ~/.claude with the research fixture and the
// sanitized parent / subagent / fork transcripts.
func claudeDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	proj := filepath.Join(d, "projects", "-home-dev-acme-app")
	copyFile(t, researchFx+"transcript-sample.jsonl", filepath.Join(proj, sessDoc+".jsonl"))
	copyFile(t, "testdata/transcripts/parent.jsonl", filepath.Join(proj, sessA+".jsonl"))
	copyFile(t, "testdata/transcripts/subagent.jsonl", filepath.Join(proj, sessA, "subagents", "agent-afake00000000001.jsonl"))
	copyFile(t, "testdata/transcripts/fork.jsonl", filepath.Join(proj, sessB+".jsonl"))
	// Set-aside copies are ignored.
	copyFile(t, "testdata/transcripts/fork.jsonl", filepath.Join(proj, sessB+".orphaned-1-x.jsonl"))
	return d
}

func byID(b Batch) (map[string]contract.Session, map[string]RawAction) {
	s := map[string]contract.Session{}
	for _, x := range b.Sessions {
		s[x.ID] = x
	}
	a := map[string]RawAction{}
	for _, x := range b.Actions {
		a[x.Action.ID] = x
	}
	return s, a
}

func hasWarning(b Batch, code string) bool {
	for _, w := range b.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestScanResearchFixture(t *testing.T) {
	d := t.TempDir()
	copyFile(t, researchFx+"transcript-sample.jsonl", filepath.Join(d, "projects", "-home-dev-acme-app", sessDoc+".jsonl"))
	b, err := ScanTranscripts(d, dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sessions) != 1 || len(b.Actions) != 2 {
		t.Fatalf("sessions=%d actions=%d", len(b.Sessions), len(b.Actions))
	}
	s := b.Sessions[0]
	if s.ID != sessDoc || *s.TaskTitle != "waitlist-demo" || s.TitleSource != contract.SessionTitleSourceCustomTitle ||
		*s.AgentVersion != "2.1.310" || *s.GitBranch != "main" || *s.Model != "claude-haiku-5-5" ||
		!slices.Equal(s.Sources, []contract.SessionSourcesElem{"transcript"}) || s.DeveloperID != dev ||
		s.Agent != contract.SessionAgentClaudeCode {
		t.Errorf("session: %+v", s)
	}
	if !s.StartedAt.Equal(time.Date(2026, 10, 26, 12, 0, 0, 10e6, time.UTC)) || s.LastActivityAt == nil {
		t.Errorf("times: %v %v", s.StartedAt, s.LastActivityAt)
	}
	if s.Repo == nil || *s.Repo.DisplayName != "acme-app" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(s.Repo.PathHash) {
		t.Errorf("repo: %+v", s.Repo)
	}
	_, acts := byID(b)
	bash := acts["toolu_01FAKEbashAAAAAAAAAAAAAAA"]
	if bash.Output != "muretto-test-ref abcdefghijklmnopqrst" || bash.Action.Status != "ok" || bash.Action.Source != "transcript" ||
		*bash.Action.InputExcerpt != "echo muretto-test-ref abcdefghijklmnopqrst" || bash.Action.SessionID != sessDoc {
		t.Errorf("bash: %+v / %q", bash.Action, bash.Output)
	}
	mcp := acts["toolu_01FAKEmcpBBBBBBBBBBBBBBBB"]
	if !strings.Contains(mcp.Output, "zzfakeref000000000000") || *mcp.Action.McpServer != "fakeinfra" {
		t.Errorf("mcp: %+v / %q", mcp.Action, mcp.Output)
	}
	if mcp.Input.(map[string]any)["name"] != "demo-waitlist" {
		t.Errorf("mcp input: %v", mcp.Input)
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings: %v", b.Warnings)
	}
}

func TestScanParentSubagentFork(t *testing.T) {
	b, err := ScanTranscripts(claudeDir(t), dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sess, acts := byID(b)
	if len(sess) != 3 {
		t.Fatalf("sessions: %d", len(sess))
	}
	a := sess[sessA]
	// Slash command and meta caveat skipped; first real prompt's first line.
	if a.TaskTitle == nil || *a.TaskTitle != "Prototype a customer import" || a.TitleSource != contract.SessionTitleSourceFirstPrompt {
		t.Errorf("A title: %v %v", a.TaskTitle, a.TitleSource)
	}
	if *a.GitBranch != "feat/import" {
		t.Errorf("branch %v", a.GitBranch)
	}
	if fb := sess[sessB]; *fb.TaskTitle != "fork: bucket work" || fb.TitleSource != contract.SessionTitleSourceCustomTitle {
		t.Errorf("B title: %v", *fb.TaskTitle)
	}
	want := map[string]struct {
		session, agent, outSub string
		status                 contract.AgentActionStatus
	}{
		bash1:                             {sessA, "", "Warning: fake warning on stderr", "ok"},
		"toolu_01FAKEparentMcp0000000002": {sessA, "", `{"success":true}`, "ok"},
		"toolu_01FAKEparentAgent00000003": {sessA, "", "Seeded 3 rows.", "ok"},
		"toolu_01FAKEparentFail00000004":  {sessA, "", "bucket customer-files already exists", "error"},
		"toolu_01FAKEsubagentBash0000005": {sessA, "afake00000000001", "INSERT 0 3", "ok"},
		"toolu_01FAKEforkBash00000000006": {sessB, "", "bucket", "ok"},
	}
	if len(acts) != len(want)+2 { // + the research fixture's two
		t.Errorf("got %d actions, want %d", len(acts), len(want)+2)
	}
	for id, w := range want {
		got, ok := acts[id]
		if !ok {
			t.Errorf("missing action %s", id)
			continue
		}
		if got.Action.SessionID != w.session || got.Action.Status != w.status || !strings.Contains(got.Output, w.outSub) ||
			(w.agent != "" && (got.Action.AgentID == nil || *got.Action.AgentID != w.agent)) || (w.agent == "" && got.Action.AgentID != nil) {
			t.Errorf("%s: %+v output=%q", id, got.Action, got.Output)
		}
	}
	// Bash: stdout then stderr.
	if acts[bash1].Output != "Created a new project at https://supabase.com/dashboard/project/abcdefghijklmnopqrst\nWarning: fake warning on stderr" {
		t.Errorf("bash output %q", acts[bash1].Output)
	}
	if *acts["toolu_01FAKEparentMcp0000000002"].Action.McpServer != "supabase" {
		t.Error("mcp server not derived from tool name")
	}
	// The fork copied bash1 with identical timing; the parent started earlier,
	// so it keeps the action and nothing is ambiguous.
	if hasWarning(b, "action_owner_ambiguous") || !hasWarning(b, "transcript_lines_skipped") {
		t.Errorf("warnings: %v", b.Warnings)
	}
}

func TestForkFullyIdenticalIsFlagged(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	var b Batch
	for _, id := range []string{sessB, sessA} {
		b.Sessions = append(b.Sessions, contract.Session{ID: id, StartedAt: at, Sources: []contract.SessionSourcesElem{"transcript"}})
		b.Actions = append(b.Actions, newRawAction(bash1, id, "", at, "Bash", "", "ok", "transcript", nil, "x"))
	}
	m := Merge(b)
	if len(m.Actions) != 1 || m.Actions[0].Action.SessionID != sessA || !hasWarning(m, "action_owner_ambiguous") {
		t.Errorf("%+v %v", m.Actions, m.Warnings)
	}
	if len(m.Ambiguous) != 1 || m.Ambiguous[0] != bash1 {
		t.Errorf("Ambiguous = %v, want [%s]", m.Ambiguous, bash1)
	}
	if again := Merge(m, m); len(again.Ambiguous) != 1 {
		t.Errorf("re-merge Ambiguous = %v, want one id", again.Ambiguous)
	}
}

func TestHookDecidesForkOwner(t *testing.T) {
	tr, err := ScanTranscripts(claudeDir(t), dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// Hooks fire only in the session that ran the tool: the spool says B ran it.
	p := spoolHome(t)
	in, _ := json.Marshal(map[string]any{"session_id": sessB, "tool_name": "Bash", "tool_use_id": bash1,
		"tool_input": map[string]any{"command": "supabase projects create acme-import"}, "tool_response": map[string]any{"stdout": "ok", "stderr": ""}})
	hook.Run("PostToolUse", bytes.NewReader(in), p, time.Date(2026, 10, 20, 9, 0, 9, 0, time.UTC))
	sp, err := IngestSpool(p, dev, salt)
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(sp, tr)
	_, acts := byID(m)
	if acts[bash1].Action.SessionID != sessB || acts[bash1].Action.Source != "hook" {
		t.Errorf("owner %s source %s", acts[bash1].Action.SessionID, acts[bash1].Action.Source)
	}
	if hasWarning(m, "action_owner_ambiguous") || len(m.Ambiguous) != 0 {
		t.Error("hook evidence should remove the ambiguity")
	}
}

func TestForkWithLaterCopyTimestamps(t *testing.T) {
	// If the fork's copy carries a later timestamp, the earliest session wins.
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	mk := func(sid string, d time.Duration) RawAction {
		return newRawAction(bash1, sid, "", at.Add(d), "Bash", "", "ok", "transcript", nil, "x")
	}
	sess := func(id string, d time.Duration) contract.Session {
		return contract.Session{ID: id, StartedAt: at.Add(d), Sources: []contract.SessionSourcesElem{"transcript"}}
	}
	m := Merge(Batch{Sessions: []contract.Session{sess(sessB, time.Hour), sess(sessA, 0)},
		Actions: []RawAction{mk(sessB, time.Hour), mk(sessA, 0)}})
	if len(m.Actions) != 1 || m.Actions[0].Action.SessionID != sessA || len(m.Warnings) != 0 {
		t.Errorf("%+v %v", m.Actions, m.Warnings)
	}
}

func spoolHome(t *testing.T) paths.Paths {
	t.Helper()
	p := paths.At(filepath.Join(t.TempDir(), "home"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFx(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(researchFx + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestIngestSpool(t *testing.T) {
	p := spoolHome(t)
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ev := func(event, js string, d time.Duration) {
		hook.Run(event, strings.NewReader(js), p, t0.Add(d))
	}
	ev("SessionStart", `{"session_id":"`+sessDoc+`","source":"startup","cwd":"/home/dev/acme-app"}`, 0)
	ev("UserPromptSubmit", `{"session_id":"`+sessDoc+`","prompt":"Add a waitlist signup table\nwith details"}`, time.Second)
	hook.Run("PostToolUse", bytes.NewReader(readFx(t, "hook-posttooluse-bash.json")), p, t0.Add(2*time.Second))
	hook.Run("PostToolUse", bytes.NewReader(readFx(t, "hook-posttooluse-mcp.json")), p, t0.Add(3*time.Second))
	ev("UserPromptSubmit", `{"session_id":"`+sessDoc+`","prompt":"second prompt"}`, 4*time.Second)
	ev("PostToolUseFailure", `{"session_id":"`+sessDoc+`","tool_name":"Bash","tool_use_id":"toolu_fail","tool_input":{"command":"false"},"error":"Exit code 1\nboom"}`, 5*time.Second)
	ev("SessionEnd", `{"session_id":"`+sessDoc+`","reason":"other"}`, 6*time.Second)
	// A second session whose first prompt was a task notification.
	ev("UserPromptSubmit", `{"session_id":"s2","prompt":"<task-notification>x</task-notification>"}`, 7*time.Second)
	ev("PostToolUse", `{"session_id":"s2","tool_name":"Write","tool_input":{"file_path":"/x/migration.sql","content":"create table t();"},"tool_response":{"type":"create","filePath":"/x/migration.sql"}}`, 8*time.Second)
	// Junk in the spool: a corrupt file, an unrelated file, a fresh and a stale temp file.
	must(t, os.WriteFile(filepath.Join(p.Spool, "1-1-bad.json"), []byte("{oops"), 0o600))
	must(t, os.WriteFile(filepath.Join(p.Spool, "README.txt"), []byte("x"), 0o600))
	must(t, os.WriteFile(filepath.Join(p.Spool, ".tmp-fresh.json"), []byte("{"), 0o600))
	stale := filepath.Join(p.Spool, ".tmp-stale.json")
	must(t, os.WriteFile(stale, []byte("{"), 0o600))
	old := time.Now().Add(-2 * time.Hour)
	must(t, os.Chtimes(stale, old, old))

	b, err := IngestSpool(p, dev, salt)
	if err != nil {
		t.Fatal(err)
	}
	sess, acts := byID(b)
	s := sess[sessDoc]
	if s.TaskTitle == nil || *s.TaskTitle != "Add a waitlist signup table" || s.TitleSource != contract.SessionTitleSourceFirstPrompt {
		t.Errorf("title: %v %v", s.TaskTitle, s.TitleSource)
	}
	if !s.StartedAt.Equal(t0) || s.EndedAt == nil || !s.EndedAt.Equal(t0.Add(6*time.Second)) || *s.EndReason != "other" ||
		!slices.Equal(s.Sources, []contract.SessionSourcesElem{"hook"}) || s.Repo == nil || *s.Repo.DisplayName != "acme-app" {
		t.Errorf("session: %+v", s)
	}
	if s2 := sess["s2"]; s2.TaskTitle != nil || s2.TitleSource != contract.SessionTitleSourceNone {
		t.Errorf("s2 title: %+v", s2)
	}
	if got := acts["toolu_01FAKEbashAAAAAAAAAAAAAAA"]; got.Output != "muretto-test-ref abcdefghijklmnopqrst" || got.Action.Source != "hook" ||
		!got.Action.OccurredAt.Equal(t0.Add(2*time.Second)) || len(got.Action.Identifiers) != 0 || got.Action.Identifiers == nil {
		t.Errorf("bash: %+v %q", got.Action, got.Output)
	}
	if got := acts["toolu_01FAKEmcpBBBBBBBBBBBBBBBB"]; !strings.Contains(got.Output, "zzfakeref000000000000") || *got.Action.McpServer != "fakeinfra" {
		t.Errorf("mcp: %+v %q", got.Action, got.Output)
	}
	if got := acts["toolu_fail"]; got.Action.Status != "error" || got.Output != "Exit code 1\nboom" {
		t.Errorf("fail: %+v %q", got.Action, got.Output)
	}
	// No tool_use_id: a stable synthetic id from the spool file name.
	var write RawAction
	for _, a := range b.Actions {
		if a.Action.ToolName == "Write" {
			write = a
		}
	}
	if !strings.HasPrefix(write.Action.ID, "spool:") || !strings.Contains(write.Output, "migration.sql") {
		t.Errorf("write: %+v", write.Action)
	}
	// 9 records + corrupt + stale temp are consumed; README and the fresh temp are not.
	if len(b.Files) != 11 || !hasWarning(b, "spool_invalid") {
		t.Errorf("files=%d warnings=%v", len(b.Files), b.Warnings)
	}
	for _, f := range b.Files {
		if strings.Contains(f, "README") || strings.Contains(f, "fresh") {
			t.Errorf("consumed %s", f)
		}
	}
	if err := Ack(b.Files); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(p.Spool)
	if len(left) != 2 {
		t.Errorf("left after Ack: %d", len(left))
	}
	if err := Ack(b.Files); err != nil {
		t.Errorf("second Ack: %v", err)
	}
	if b2, err := IngestSpool(p, dev, salt); err != nil || len(b2.Sessions) != 0 {
		t.Errorf("re-ingest: %v %v", b2.Sessions, err)
	}
}

func TestIngestSpoolMissingDir(t *testing.T) {
	b, err := IngestSpool(paths.At(filepath.Join(t.TempDir(), "nope")), dev, salt)
	if err != nil || len(b.Sessions) != 0 {
		t.Errorf("%v %v", b, err)
	}
	if _, err := IngestSpool(paths.At(t.TempDir()), "", salt); err == nil {
		t.Error("empty developer id accepted")
	}
	if _, err := IngestSpool(paths.At(t.TempDir()), dev, ""); err == nil {
		t.Error("empty path salt accepted")
	}
	if _, err := ScanTranscripts(t.TempDir(), dev, "", time.Time{}); err == nil {
		t.Error("empty path salt accepted by ScanTranscripts")
	}
}

func TestMergeSpoolAndTranscript(t *testing.T) {
	p := spoolHome(t)
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 5e6, time.UTC)
	hook.Run("UserPromptSubmit", strings.NewReader(`{"session_id":"`+sessDoc+`","prompt":"Add a waitlist signup table"}`), p, t0)
	hook.Run("PostToolUse", bytes.NewReader(readFx(t, "hook-posttooluse-bash.json")), p, t0.Add(2*time.Second))
	sp, err := IngestSpool(p, dev, salt)
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	copyFile(t, researchFx+"transcript-sample.jsonl", filepath.Join(d, "projects", "-home-dev-acme-app", sessDoc+".jsonl"))
	tr, err := ScanTranscripts(d, dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(sp, tr)
	if len(m.Sessions) != 1 || len(m.Actions) != 2 {
		t.Fatalf("sessions=%d actions=%d", len(m.Sessions), len(m.Actions))
	}
	s := m.Sessions[0]
	if !slices.Equal(s.Sources, []contract.SessionSourcesElem{"hook", "transcript"}) || *s.TaskTitle != "waitlist-demo" ||
		s.TitleSource != "custom_title" || *s.AgentVersion != "2.1.310" || *s.Model != "claude-haiku-5-5" || !s.StartedAt.Equal(t0) {
		t.Errorf("merged session: %+v", s)
	}
	_, acts := byID(m)
	if acts["toolu_01FAKEbashAAAAAAAAAAAAAAA"].Action.Source != "hook" || acts["toolu_01FAKEmcpBBBBBBBBBBBBBBBB"].Action.Source != "transcript" {
		t.Error("wrong action sources after merge")
	}
	if len(m.Files) != 2 {
		t.Errorf("files: %v", m.Files)
	}
}

func TestTitleOrder(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		acc  sessionAcc
		want string
		src  contract.SessionTitleSource
	}{
		{sessionAcc{customTitle: "c", firstPrompt: "p", aiTitle: "a"}, "c", "custom_title"},
		{sessionAcc{firstPrompt: "p", aiTitle: "a"}, "p", "first_prompt"},
		{sessionAcc{aiTitle: "a"}, "a", "ai_title"},
		{sessionAcc{}, "", "none"},
		{sessionAcc{customTitle: strings.Repeat("x", 300)}, strings.Repeat("x", 120), "custom_title"},
	} {
		tc.acc.started = at
		s := tc.acc.session(dev, newRepoCache(salt))
		if s.TitleSource != tc.src || deref(s.TaskTitle) != tc.want {
			t.Errorf("%+v -> %q %s", tc.acc, deref(s.TaskTitle), s.TitleSource)
		}
	}
}

func TestTranscriptRedaction(t *testing.T) {
	d := t.TempDir()
	pw := "Q" + strings.Repeat("z8", 9)
	tok := "sbp_" + strings.Repeat("4e", 20)
	lines := []map[string]any{
		{"type": "user", "sessionId": "s1", "timestamp": "2026-10-26T12:00:00Z", "cwd": "/w",
			"message": map[string]any{"role": "user", "content": "deploy using " + tok + "\nand more"}},
		{"type": "assistant", "sessionId": "s1", "timestamp": "2026-10-26T12:00:01Z", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_r", "name": "Bash", "input": map[string]any{"command": "psql postgres://postgres:" + pw + "@db.abcdefghijklmnopqrst.supabase.co/postgres", "password": pw}}}}},
		{"type": "user", "sessionId": "s1", "timestamp": "2026-10-26T12:00:02Z", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_r", "content": "SUPABASE_ACCESS_TOKEN=" + tok}}},
			"toolUseResult": map[string]any{"stdout": "token: " + tok + "\nref abcdefghijklmnopqrst", "stderr": ""}},
	}
	var buf bytes.Buffer
	for _, l := range lines {
		b, _ := json.Marshal(l)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	f := filepath.Join(d, "projects", "-w", "s1.jsonl")
	must(t, os.MkdirAll(filepath.Dir(f), 0o700))
	must(t, os.WriteFile(f, buf.Bytes(), 0o600))
	b, err := ScanTranscripts(d, dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := json.Marshal(b)
	inputs, _ := json.Marshal(b.Actions[0].Input)
	blob := string(all) + string(inputs) + b.Actions[0].Output
	for _, s := range []string{pw, tok} {
		if strings.Contains(blob, s) {
			t.Errorf("secret %q survived: %s", s, blob)
		}
	}
	if !strings.Contains(b.Actions[0].Output, "abcdefghijklmnopqrst") || *b.Sessions[0].TaskTitle != "deploy using "+redact.Placeholder {
		t.Errorf("lost content: %q / %q", b.Actions[0].Output, *b.Sessions[0].TaskTitle)
	}
}

func TestTranscriptTolerance(t *testing.T) {
	d := t.TempDir()
	proj := filepath.Join(d, "projects", "-w")
	must(t, os.MkdirAll(proj, 0o700))
	defer func(v int) { maxLine = v }(maxLine)
	maxLine = 1 << 10
	junk := strings.Join([]string{
		"\x00\xff\xfe",
		`{"type":"user"`,
		`[]`, `null`, `42`, `"str"`,
		`{"type":"user","sessionId":"s1","timestamp":"not a time","message":"not an object"}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-10-26T12:00:00Z","message":{"content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"echo hi"}},{"type":7}]}}`,
		`{"type":"user","sessionId":"s1","timestamp":"2026-10-26T12:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_x","content":[{"type":"image","source":{}},{"type":"text","text":"hi"}]}]}}`,
		`{"type":"user","sessionId":"s1","timestamp":"2026-10-26T12:00:02Z","message":{"content":"` + strings.Repeat("y", 4<<10) + `"}}`,
		`{"type":"brand-new","sessionId":"s1","x":{"y":[{}]}}`,
		``,
	}, "\n")
	must(t, os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(junk), 0o600))
	must(t, os.WriteFile(filepath.Join(proj, "empty.jsonl"), nil, 0o600))
	b, err := ScanTranscripts(d, dev, salt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Actions) != 1 || b.Actions[0].Output != "hi" {
		t.Errorf("actions: %+v", b.Actions)
	}
	if !hasWarning(b, "transcript_lines_skipped") {
		t.Error("no warning for bad lines")
	}
}

func TestSinceAndMissingDir(t *testing.T) {
	d := claudeDir(t)
	b, err := ScanTranscripts(d, dev, salt, time.Now().Add(time.Hour))
	if err != nil || len(b.Sessions) != 0 {
		t.Errorf("since filter: %d %v", len(b.Sessions), err)
	}
	b, err = ScanTranscripts(filepath.Join(d, "nope"), dev, salt, time.Time{})
	if err != nil || len(b.Sessions) != 0 {
		t.Errorf("missing dir: %v", err)
	}
}

func TestUntestedVersionWarns(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "projects", "-w", "s1.jsonl")
	must(t, os.MkdirAll(filepath.Dir(f), 0o700))
	must(t, os.WriteFile(f, []byte(`{"type":"user","sessionId":"s1","version":"9.0.0","timestamp":"2026-10-26T12:00:00Z","message":{"content":"hi there"}}`+"\n"), 0o600))
	b, _ := ScanTranscripts(d, dev, salt, time.Time{})
	if !hasWarning(b, "transcript_version_untested") {
		t.Error("no version warning")
	}
}

func TestExcerptCap(t *testing.T) {
	long := strings.Repeat("é", 10000) + "TAIL"
	e := Excerpt(long)
	if n := len([]rune(e)); n > ExcerptMax {
		t.Errorf("excerpt %d runes", n)
	}
	if !strings.HasSuffix(e, "TAIL") {
		t.Error("tail lost")
	}
	a := newRawAction("id", "s", "", time.Now(), "Bash", "", "ok", "hook", map[string]any{"command": long}, long)
	if len([]rune(*a.Action.InputExcerpt)) > ExcerptMax || len([]rune(*a.Action.ResultExcerpt)) > ExcerptMax {
		t.Error("action excerpts not capped")
	}
}

func TestOutputText(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{map[string]any{"stdout": "a", "stderr": "b"}, "a\nb"},
		{map[string]any{"stdout": "", "stderr": "b"}, "b"},
		{[]any{map[string]any{"type": "text", "text": "x"}, map[string]any{"type": "image"}, map[string]any{"type": "text", "text": "y"}}, "x\ny"},
		{map[string]any{"content": []any{map[string]any{"type": "text", "text": "z"}}, "isError": false}, "z"},
		{"plain", "plain"},
		{nil, ""},
		{map[string]any{"type": "create", "filePath": "/a"}, `{"filePath":"/a","type":"create"}`},
	} {
		if got := OutputText("", tc.in); got != tc.want {
			t.Errorf("OutputText(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
