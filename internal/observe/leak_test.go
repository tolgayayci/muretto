package observe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/hook"
)

// TestHookToIngestNoSecrets feeds the inputs an independent review found
// leaking through the real hook (hook.Run writes the spool) and ingest
// (IngestSpool), and checks that no secret reaches the spool file bytes or the
// RawActions, whether it sits in the tool input, the tool output or an error.
func TestHookToIngestNoSecrets(t *testing.T) {
	// Every secret is distinct and built at runtime, so the test can tell
	// exactly which input leaked and no secret-shaped literal is committed.
	n := 0
	sec := func() string {
		n++
		return fmt.Sprintf("Zq%02dFk%sX", n, strings.Repeat("e9", 5))
	}
	type leak struct{ text, secret string }
	var cases []leak
	add := func(format string) {
		s := sec()
		cases = append(cases, leak{strings.ReplaceAll(format, "SECRET", s), s})
	}
	esc2 := `\\\"`
	add("DB_PASS=SECRET psql")
	add("export SMTP_PASS='SECRET'")
	add("REDIS_PASS=SECRET redis-cli ping")
	add("psql postgresql://postgres:abc,SECRET@db.abcdefghijklmnopqrst.supabase.co:5432/postgres")
	add("psql 'postgresql://postgres:ab(SECRET)ef@db.abcdefghijklmnopqrst.supabase.co/postgres'")
	add("psql postgresql://postgres:ab{SECRET}ef@db.x.supabase.co/postgres")
	add("psql postgresql://postgres:ab[SECRET]ef@db.x.supabase.co/postgres")
	add("curl -u admin:SECRET https://api.example.com")
	add("mysql -uroot -pSECRET app")
	add("vercel deploy -t SECRET --yes")
	add(`echo "{` + esc2 + `password` + esc2 + `:` + esc2 + `SECRET` + esc2 + `}"`)
	add("python -c \"print({'Authorization': ['Bearer SECRET']})\"")
	add("printf 'password: correct horse SECRET\\n'")

	p := spoolHome(t)
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	run := func(event string, v map[string]any, i int) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		hook.Run(event, strings.NewReader(string(raw)), p, t0.Add(time.Duration(i)*time.Second))
	}
	run("SessionStart", map[string]any{"session_id": "s-leak", "source": "startup", "cwd": "/home/dev/app"}, 0)
	for i, c := range cases {
		// In the tool input ...
		run("PostToolUse", map[string]any{
			"session_id": "s-leak", "tool_name": "Bash", "tool_use_id": fmt.Sprintf("toolu_in%02d", i),
			"tool_input":    map[string]any{"command": c.text, "description": "run it"},
			"tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false},
		}, i+1)
		// ... in the tool output ...
		run("PostToolUse", map[string]any{
			"session_id": "s-leak", "tool_name": "Bash", "tool_use_id": fmt.Sprintf("toolu_out%02d", i),
			"tool_input":    map[string]any{"command": "cat .env"},
			"tool_response": map[string]any{"stdout": c.text, "stderr": "", "interrupted": false},
		}, i+1)
		// ... and in a failure's error text.
		run("PostToolUseFailure", map[string]any{
			"session_id": "s-leak", "tool_name": "Bash", "tool_use_id": fmt.Sprintf("toolu_err%02d", i),
			"tool_input": map[string]any{"command": "deploy"}, "error": "Exit code 1\n" + c.text,
		}, i+1)
	}
	// MCP text content holding double-escaped JSON.
	mcpSecret := sec()
	mcpText := `"Below is the result <untrusted-data>[{` + esc2 + `db_pass` + esc2 + `:` + esc2 + mcpSecret + esc2 + `}]</untrusted-data>"`
	run("PostToolUse", map[string]any{
		"session_id": "s-leak", "tool_name": "mcp__supabase__execute_sql", "tool_use_id": "toolu_mcp",
		"tool_input":    map[string]any{"project_id": "abcdefghijklmnopqrst", "query": "select * from cfg"},
		"tool_response": []any{map[string]any{"type": "text", "text": mcpText}},
	}, 99)
	cases = append(cases, leak{mcpText, mcpSecret})

	files, err := filepath.Glob(filepath.Join(p.Spool, "*.json"))
	if err != nil || len(files) < 3*(len(cases)-1) {
		t.Fatalf("spool has %d files (%v); hook did not record", len(files), err)
	}
	var spool strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		spool.Write(b)
	}

	b, err := IngestSpool(p, dev, salt)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Actions) != 3*(len(cases)-1)+1 {
		t.Fatalf("ingested %d actions, want %d", len(b.Actions), 3*(len(cases)-1)+1)
	}
	ingested, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if strings.Contains(spool.String(), c.secret) {
			t.Errorf("secret reached the spool: %s", c.text)
		}
		if strings.Contains(string(ingested), c.secret) {
			t.Errorf("secret reached a RawAction: %s", c.text)
		}
	}
	// The redaction did happen (the inputs are not just dropped).
	if !strings.Contains(spool.String(), "[REDACTED]") || !strings.Contains(string(ingested), "curl -u admin:[REDACTED]") {
		t.Error("expected redacted forms in the spool and RawActions")
	}
}
