package main

// Project-scoped Supabase MCP server (docs/decisions/0905-project-scoped-mcp.md):
// the server is pinned to one project in its URL (?project_ref=...), so its
// tools take no project_id. The hook reads the server's identifier params
// from Claude Code's MCP config, and the supabase.mcp.* signatures use them
// as the container.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

const (
	sessPinned   = "9e000000-0000-4000-8000-000000000009"
	otherRef     = "zyxwvutsrqponmlkjihg"
	idPinTable   = "supabase.table:" + e2eRef + "/public.customers"
	idPinBucket  = "supabase.bucket:" + e2eRef + "/demo-uploads"
	idPinFunc    = "supabase.function:" + e2eRef + "/demo-hello"
	pinCustomers = "create table public.customers (id bigint generated always as identity primary key, name text not null); insert into public.customers (name) values ('Ada');"
	pinBucket    = "insert into storage.buckets (id, name, public) values ('demo-uploads','demo-uploads',true);"
)

// pinnedWorld serves a project holding the table, bucket and function the
// pinned session creates, and writes a .mcp.json pinning ref (if not empty)
// into the session's working directory.
func pinnedWorld(t *testing.T, ref string) *e2eWorld {
	t.Helper()
	w := newE2EWorld(t, e2eOpts{catalog: true})
	sb := "api.supabase.com"
	p := "/v1/projects/" + e2eRef
	w.api.set(sb, "GET", p+"/functions", e2eRoute{body: `[{"id":"00000000-0000-4000-8000-000000000031","slug":"demo-hello","name":"demo-hello",
		"status":"ACTIVE","version":1,"created_at":{{ms:-9m}},"updated_at":{{ms:-9m}},"verify_jwt":true,"import_map":false}]`})
	w.api.set(sb, "GET", p+"/storage/buckets", e2eRoute{body: `[{"id":"demo-uploads","name":"demo-uploads","owner":"",
		"created_at":"{{ts:-11m}}","updated_at":"{{ts:-11m}}","public":true}]`})
	w.api.set(sb, "GET", p+"/advisors/security", e2eRoute{body: `{"lints":[]}`})
	w.api.set(sb, "POST", p+"/database/query/read-only", e2eRoute{status: http.StatusCreated,
		body: `[{"schema":"public","name":"customers","rls_enabled":true,"rls_forced":false,"anon_schema_usage":true,"anon_select":true,"authenticated_select":true}]`})
	if ref != "" {
		cfg := map[string]any{"mcpServers": map[string]any{"supabase": map[string]any{
			"type":    "http",
			"url":     "https://mcp.supabase.com/mcp?project_ref=" + ref + "&features=database,functions,development",
			"headers": map[string]any{"Authorization": "Bearer ${SUPABASE_ACCESS_TOKEN}"},
		}}}
		b, _ := json.Marshal(cfg)
		if err := os.WriteFile(filepath.Join(w.repo, ".mcp.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// pinnedSession runs the three live-shaped calls of a session using a
// project-pinned server: no project_id in any input.
func (w *e2eWorld) pinnedSession(source string) {
	w.start(sessPinned, -15*time.Minute, "add customers, an upload bucket and a hello function")
	call := func(id string, at time.Duration, tool string, input map[string]any, result string) {
		w.hook(sessPinned, at, map[string]any{
			"hook_event_name": "PostToolUse", "prompt_id": "prompt-" + sessPinned[:8], "permission_mode": "default",
			"tool_name": "mcp__supabase__" + tool, "tool_input": input,
			"tool_response": []any{map[string]any{"type": "text", "text": result}},
			"tool_use_id":   id, "duration_ms": 300,
			"mcp_server": map[string]any{"name": "supabase", "source": source},
		})
	}
	call("toolu_01PINNED000000000000001", -12*time.Minute, "apply_migration",
		map[string]any{"name": "create_customers_table", "query": pinCustomers}, `{"success":true}`)
	call("toolu_01PINNED000000000000002", -11*time.Minute, "execute_sql",
		map[string]any{"query": pinBucket}, sqlResult("[]"))
	call("toolu_01PINNED000000000000003", -10*time.Minute, "deploy_edge_function",
		map[string]any{"name": "demo-hello", "entrypoint_path": "index.ts", "verify_jwt": true,
			"files": []any{map[string]any{"name": "index.ts", "content": "Deno.serve(() => new Response('hello'))"}}},
		`{"id":"00000000-0000-4000-8000-000000000031","slug":"demo-hello","name":"demo-hello","status":"ACTIVE","version":1,`+
			`"created_at":1793099195402,"updated_at":1793099195402,"verify_jwt":true,"import_map":false,`+
			`"entrypoint_path":"file:///tmp/user_fn/source/index.ts","ezbr_sha256":"00"}`)
	w.end(sessPinned, -9*time.Minute)
}

func evidenceOnly(a contract.Answer) []string {
	var out []string
	for _, r := range a.Resources {
		if r.SeenVia == contract.ResourceSeenViaEvidence {
			out = append(out, r.ID)
		}
	}
	return out
}

func TestE2EPinnedMCPServer(t *testing.T) {
	w := pinnedWorld(t, e2eRef)
	w.pinnedSession("project")
	x := e2eAnswer{t, w.answer()}
	w.api.check(t)

	x.exact(idPinTable, sessPinned, "name_in_container", "create table public.customers")
	x.exact(idPinBucket, sessPinned, "name_in_container", "insert into storage.buckets")
	x.exact(idPinFunc, sessPinned, "name_in_container", "demo-hello")
	if ev := evidenceOnly(x.a); len(ev) != 0 {
		t.Errorf("evidence-only duplicates beside the listed resources: %v", ev)
	}

	// A run with another extract version (new binary or adapters) re-runs
	// the signatures on the stored actions: the persisted params keep the
	// attribution.
	var out, errb bytes.Buffer
	a := w.app(&out, &errb)
	a.extractVersion = "bumped-for-test"
	if code := a.run(context.Background(), []string{"--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var got contract.Answer
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	x = e2eAnswer{t, got}
	x.exact(idPinTable, sessPinned, "name_in_container", "create table public.customers")
	x.exact(idPinBucket, sessPinned, "name_in_container", "insert into storage.buckets")
	x.exact(idPinFunc, sessPinned, "name_in_container", "demo-hello")
}

// A server passed with --mcp-config (source "dynamic") can't be resolved:
// no project is guessed, everything stays unknown.
func TestE2EPinnedMCPServerDynamic(t *testing.T) {
	w := pinnedWorld(t, e2eRef)
	w.pinnedSession("dynamic")
	x := e2eAnswer{t, w.answer()}
	w.api.check(t)

	for _, id := range []string{idPinTable, idPinBucket, idPinFunc} {
		x.unknown(id, "creator unknown")
	}
	// Seen in the session only, for every kind alike.
	want := map[string]bool{"supabase.table:public.customers": true, "supabase.bucket:demo-uploads": true, "supabase.function:demo-hello": true}
	for _, id := range evidenceOnly(x.a) {
		if !want[id] {
			t.Errorf("unexpected evidence-only resource %s", id)
		}
		delete(want, id)
		x.unknown(id, "not confirmed")
	}
	if len(want) != 0 {
		t.Errorf("missing evidence-only resources %v", want)
	}
}

// .mcp.json pins another project than the one holding the resources: they
// are never attributed across projects.
func TestE2EPinnedMCPServerOtherProject(t *testing.T) {
	w := pinnedWorld(t, otherRef)
	w.pinnedSession("project")
	x := e2eAnswer{t, w.answer()}
	w.api.check(t)

	for _, id := range []string{idPinTable, idPinBucket, idPinFunc} {
		x.unknown(id, "creator unknown")
	}
	for _, id := range evidenceOnly(x.a) {
		x.unknown(id, "not confirmed")
	}
}
