package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/contract"
)

const (
	pinRef   = "abcdefghijklmnopqrst"
	otherPin = "zyxwvutsrqponmlkjihg"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func server(url string) map[string]any {
	return map[string]any{"type": "http", "url": url}
}

func servers(m map[string]any) map[string]any { return map[string]any{"mcpServers": m} }

// mcpCall runs the hook on a PostToolUse for mcp__<name>__execute_sql from
// cwd and returns the record.
func mcpCall(t *testing.T, cwd, name, source string) contract.SpoolRecord {
	t.Helper()
	p := newPaths(t)
	in := mustJSON(map[string]any{
		"session_id": "s1", "hook_event_name": "PostToolUse", "cwd": cwd,
		"tool_name": "mcp__" + name + "__execute_sql", "tool_input": map[string]any{"query": "select 1"},
		"tool_response": []any{map[string]any{"type": "text", "text": "{}"}}, "tool_use_id": "toolu_1",
		"mcp_server": map[string]any{"name": name, "source": source},
	})
	recs := runHook(t, p, "PostToolUse", []byte(in))
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	return recs[0]
}

// isolate points the global Claude Code config at a temp dir.
func isolate(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return dir
}

func TestMCPParamsProjectScope(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	cwd := filepath.Join(root, "repo", "sub", "dir")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	// The nearest .mcp.json that defines the server wins; one that doesn't
	// define it is skipped.
	writeJSON(t, filepath.Join(root, ".mcp.json"), servers(map[string]any{
		"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + otherPin)}))
	writeJSON(t, filepath.Join(root, "repo", ".mcp.json"), servers(map[string]any{
		"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + pinRef + "&features=database,functions&read_only=true")}))
	writeJSON(t, filepath.Join(root, "repo", "sub", ".mcp.json"), servers(map[string]any{
		"other": server("https://example.com/mcp?project_ref=" + otherPin)}))

	r := mcpCall(t, cwd, "supabase", "project")
	if r.McpServerScope == nil || *r.McpServerScope != "project" {
		t.Errorf("scope = %v", r.McpServerScope)
	}
	if want := (contract.SpoolRecordMcpParams{"project_ref": pinRef}); !reflect.DeepEqual(r.McpParams, want) {
		t.Errorf("params = %v, want %v", r.McpParams, want)
	}
	if r := mcpCall(t, cwd, "other", "project"); r.McpParams["project_ref"] != otherPin {
		t.Errorf("other server: %v", r.McpParams)
	}
	if r := mcpCall(t, cwd, "missing", "project"); r.McpParams != nil {
		t.Errorf("undefined server: %v", r.McpParams)
	}
}

func TestMCPParamsGlobalScopes(t *testing.T) {
	dir := isolate(t)
	cwd := filepath.Join(t.TempDir(), "repo", "pkg")
	repo := filepath.Dir(cwd)
	writeJSON(t, filepath.Join(dir, ".claude.json"), map[string]any{
		"numStartups": 3,
		"mcpServers":  map[string]any{"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + otherPin)},
		"projects": map[string]any{
			repo: servers(map[string]any{"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + pinRef)}),
		},
	})
	if r := mcpCall(t, cwd, "supabase", "local"); r.McpParams["project_ref"] != pinRef {
		t.Errorf("local: %v", r.McpParams)
	}
	if r := mcpCall(t, cwd, "supabase", "user"); r.McpParams["project_ref"] != otherPin {
		t.Errorf("user: %v", r.McpParams)
	}
	if r := mcpCall(t, t.TempDir(), "supabase", "local"); r.McpParams != nil {
		t.Errorf("local outside the project: %v", r.McpParams)
	}
}

// Scopes the hook can't resolve record the scope and nothing else: no guess.
func TestMCPParamsUnresolvableScopes(t *testing.T) {
	isolate(t)
	cwd := t.TempDir()
	writeJSON(t, filepath.Join(cwd, ".mcp.json"), servers(map[string]any{
		"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + pinRef)}))
	for _, src := range []string{"dynamic", "plugin", "claudeai", "", "bad scope!"} {
		r := mcpCall(t, cwd, "supabase", src)
		if r.McpParams != nil {
			t.Errorf("%q: params %v", src, r.McpParams)
		}
		if src == "dynamic" && (r.McpServerScope == nil || *r.McpServerScope != "dynamic") {
			t.Errorf("dynamic scope not kept: %v", r.McpServerScope)
		}
		if (src == "" || src == "bad scope!") && r.McpServerScope != nil {
			t.Errorf("%q: scope %v", src, *r.McpServerScope)
		}
	}
}

func TestMCPParamsEnvExpansion(t *testing.T) {
	isolate(t)
	t.Setenv("MURETTO_TEST_REF", pinRef)
	t.Setenv("MURETTO_TEST_EMPTY", "")
	cases := []struct {
		query string
		want  map[string]string
	}{
		{"project_ref=${MURETTO_TEST_REF}", map[string]string{"project_ref": pinRef}},
		{"project_ref=${MURETTO_TEST_UNSET:-" + otherPin + "}", map[string]string{"project_ref": otherPin}},
		{"project_ref=${MURETTO_TEST_EMPTY:-" + otherPin + "}", map[string]string{"project_ref": otherPin}},
		{"project_ref=${MURETTO_TEST_UNSET}", nil},
		{"project_ref=${MURETTO_TEST_REF", nil},
		{"project_ref=" + pinRef + "&project_ref=" + otherPin, nil},
		{"project_ref=" + pinRef + "&project_ref=" + pinRef, map[string]string{"project_ref": pinRef}},
		{"project_ref=" + pinRef + "&team_id=team_FAKE01#frag", map[string]string{"project_ref": pinRef, "team_id": "team_FAKE01"}},
		{"Project_Ref=" + pinRef, nil},
		{"project_ref=a%2Fb", nil},
		{"project_ref=" + strings.Repeat("a", 65), nil},
	}
	for _, c := range cases {
		cwd := t.TempDir()
		writeJSON(t, filepath.Join(cwd, ".mcp.json"), servers(map[string]any{
			"supabase": server("https://mcp.supabase.com/mcp?" + c.query)}))
		got := map[string]string(mcpCall(t, cwd, "supabase", "project").McpParams)
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.query, got, c.want)
		}
	}
}

// A server whose URL, path, headers, env and other params carry secrets
// records only the allowlisted id, and no secret reaches the spool.
func TestMCPParamsRecordNoSecrets(t *testing.T) {
	isolate(t)
	token := "sbp_" + strings.Repeat("0123456789abcdef", 3)[:40]
	t.Setenv("MURETTO_TEST_TOKEN", token)
	cwd := t.TempDir()
	writeJSON(t, filepath.Join(cwd, ".mcp.json"), servers(map[string]any{"supabase": map[string]any{
		"type": "http",
		"url": "https://user:" + token + "@mcp.supabase.com/mcp/" + token + "?access_token=" + token +
			"&project_ref=" + pinRef + "&project_id=" + token + "&org_id=${MURETTO_TEST_TOKEN}&api_key=${MURETTO_TEST_TOKEN}",
		"headers": map[string]any{"Authorization": "Bearer " + token, "X-Project-Ref": "secret-header"},
		"env":     map[string]any{"SUPABASE_ACCESS_TOKEN": token},
	}}))
	p := newPaths(t)
	in := mustJSON(map[string]any{
		"session_id": "s1", "hook_event_name": "PostToolUse", "cwd": cwd,
		"tool_name": "mcp__supabase__list_tables", "tool_input": map[string]any{}, "tool_use_id": "toolu_1",
		"tool_response": []any{map[string]any{"type": "text", "text": "[]"}},
		"mcp_server":    map[string]any{"name": "supabase", "source": "project"},
	})
	recs := runHook(t, p, "PostToolUse", []byte(in))
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if want := (contract.SpoolRecordMcpParams{"project_ref": pinRef}); !reflect.DeepEqual(recs[0].McpParams, want) {
		t.Errorf("params = %v, want %v", recs[0].McpParams, want)
	}
	entries, _ := os.ReadDir(p.Spool)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(p.Spool, e.Name()))
		for _, s := range []string{token, "secret-header", "mcp.supabase.com", "access_token", "api_key"} {
			if strings.Contains(string(b), s) {
				t.Errorf("spool record contains %q", s)
			}
		}
	}
}

// Broken, huge or odd config files record nothing and never fail the hook.
func TestMCPParamsBadConfigs(t *testing.T) {
	dir := isolate(t)
	writeJSON(t, filepath.Join(dir, ".claude.json"), map[string]any{"mcpServers": "not an object", "projects": []any{1}})
	cwd := t.TempDir()
	for name, body := range map[string]string{
		"garbage":  "{not json",
		"no url":   `{"mcpServers":{"supabase":{"type":"stdio","command":"npx","args":["--project-ref=` + pinRef + `"]}}}`,
		"url type": `{"mcpServers":{"supabase":{"url":42}}}`,
		"no query": `{"mcpServers":{"supabase":{"url":"https://mcp.supabase.com/mcp"}}}`,
	} {
		if err := os.WriteFile(filepath.Join(cwd, ".mcp.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if r := mcpCall(t, cwd, "supabase", "project"); r.McpParams != nil {
			t.Errorf("%s: %v", name, r.McpParams)
		}
	}
	for _, src := range []string{"local", "user"} {
		if r := mcpCall(t, cwd, "supabase", src); r.McpParams != nil {
			t.Errorf("%s: %v", src, r.McpParams)
		}
	}
	// A directory named .mcp.json, and a file over the cap.
	big := t.TempDir()
	if err := os.Mkdir(filepath.Join(big, ".mcp.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r := mcpCall(t, big, "supabase", "project"); r.McpParams != nil {
		t.Errorf("directory: %v", r.McpParams)
	}
	huge := t.TempDir()
	f, err := os.Create(filepath.Join(huge, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxMCPConfig + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if r := mcpCall(t, huge, "supabase", "project"); r.McpParams != nil {
		t.Errorf("huge: %v", r.McpParams)
	}
}

// Only tool results look the config up.
func TestMCPParamsOnlyOnToolResults(t *testing.T) {
	isolate(t)
	cwd := t.TempDir()
	writeJSON(t, filepath.Join(cwd, ".mcp.json"), servers(map[string]any{
		"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + pinRef)}))
	p := newPaths(t)
	in := mustJSON(map[string]any{"session_id": "s1", "hook_event_name": "PreToolUse", "cwd": cwd,
		"tool_name": "mcp__supabase__list_tables", "mcp_server": map[string]any{"name": "supabase", "source": "project"}})
	recs := runHook(t, p, "PreToolUse", []byte(in))
	if len(recs) != 1 || recs[0].McpParams != nil {
		t.Errorf("%+v", recs)
	}
}

func BenchmarkRunMCPProjectScope(b *testing.B) {
	b.Setenv("CLAUDE_CONFIG_DIR", b.TempDir())
	p := newPaths(b)
	cwd := filepath.Join(b.TempDir(), "a", "b", "c")
	_ = os.MkdirAll(cwd, 0o700)
	cfg, _ := json.Marshal(servers(map[string]any{"supabase": server("https://mcp.supabase.com/mcp?project_ref=" + pinRef)}))
	_ = os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(cwd)), ".mcp.json"), cfg, 0o600)
	in := []byte(mustJSON(map[string]any{"session_id": "s1", "hook_event_name": "PostToolUse", "cwd": cwd,
		"tool_name": "mcp__supabase__execute_sql", "tool_input": map[string]any{"query": "select 1"},
		"tool_response": []any{map[string]any{"type": "text", "text": "{}"}}, "tool_use_id": "toolu_1",
		"mcp_server": map[string]any{"name": "supabase", "source": "project"}}))
	for b.Loop() {
		Run("PostToolUse", strings.NewReader(string(in)), p, now)
	}
}
