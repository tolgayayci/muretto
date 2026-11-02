package adapters

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

type extractCase struct {
	Name   string            `yaml:"name"`
	Tool   string            `yaml:"tool"`
	Status string            `yaml:"status"`
	Input  any               `yaml:"input"`
	Output string            `yaml:"output"`
	MCP    map[string]string `yaml:"mcp"` // MCP server URL params recorded by the hook
	Guess  string            `yaml:"guess"`
	// Matches is checked by internal/adaptercheck (the resources the
	// created identifiers must match); this runner ignores it.
	Matches []string `yaml:"matches"`
	Expect  []struct {
		Kind      string `yaml:"kind"`
		Value     string `yaml:"value"`
		Container string `yaml:"container"`
		Role      string `yaml:"role"`
	} `yaml:"expect"`
}

func rawAction(tool, status string, input any, output string) observe.RawAction {
	st := contract.AgentActionStatusOk
	if status == "error" {
		st = contract.AgentActionStatusError
	}
	return observe.RawAction{
		Action: contract.AgentAction{ID: "toolu_FAKE", SessionID: "00000000-0000-4000-8000-00000000000a", ToolName: tool, Status: st, Source: contract.AgentActionSourceHook},
		Input:  input,
		Output: output,
	}
}

func idKey(kind, value, container, role string) string {
	return kind + " " + value + " @" + container + " " + role
}

func TestExtractFixtures(t *testing.T) {
	r := loadEmbedded(t)
	files, _ := filepath.Glob(filepath.Join(adaptersDir, "*", "testdata", "extract", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no extract cases")
	}
	total := 0
	for _, f := range files {
		var cases []extractCase
		readYAML(t, f, &cases)
		for _, c := range cases {
			total++
			t.Run(c.Name, func(t *testing.T) {
				a := rawAction(c.Tool, c.Status, c.Input, c.Output)
				a.MCP = c.MCP
				var got, want []string
				for _, id := range r.Extract(a) {
					cont := ""
					if id.Container != nil {
						cont = *id.Container
					}
					got = append(got, idKey(id.Kind, id.Value, cont, string(id.Role)))
					if id.Excerpt == nil || *id.Excerpt == "" || len(*id.Excerpt) > 200 {
						t.Errorf("identifier %s needs a short excerpt", id.Value)
					}
					if id.SignatureID == "" {
						t.Errorf("identifier %s has no signature id", id.Value)
					}
				}
				for _, e := range c.Expect {
					want = append(want, idKey(e.Kind, e.Value, e.Container, e.Role))
				}
				sort.Strings(got)
				sort.Strings(want)
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Errorf("\n got:\n  %s\n want:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
				}
				if c.Guess != "" {
					if g := r.ProviderGuess(a); g != c.Guess {
						t.Errorf("ProviderGuess = %q, want %q", g, c.Guess)
					}
				}
			})
		}
	}
	if total < 30 {
		t.Errorf("only %d extract cases; keep the suite broad", total)
	}
}

func TestProviderGuess(t *testing.T) {
	r := loadEmbedded(t)
	cases := map[string]string{
		"mcp__supabase__list_tables":          "supabase",
		"mcp__claude_ai_Supabase__get_logs":   "supabase",
		"mcp__claude_ai_Vercel__list_teams":   "vercel",
		"mcp__plugin_vercel_vercel__get_user": "vercel",
		"mcp__github__create_issue":           "github",
		"mcp__linear__create_issue":           "",
		"Read":                                "",
	}
	for tool, want := range cases {
		if got := r.ProviderGuess(rawAction(tool, "ok", map[string]any{}, "")); got != want {
			t.Errorf("%s: guess %q, want %q", tool, got, want)
		}
	}
	// A Bash call is only guessed from a signature with a command pattern:
	// tool-only signatures (any output with an id) don't name a provider.
	for cmd, want := range map[string]string{
		"ls -la":                           "",
		"cat .vercel/project.json":         "",
		"stripe projects add supabase/db":  "supabase",
		"gh repo create acme/app --public": "github",
		"wrangler d1 create example-db":    "cloudflare",
	} {
		if got := r.ProviderGuess(rawAction("Bash", "ok", map[string]any{"command": cmd}, "")); got != want {
			t.Errorf("Bash %q: guess %q, want %q", cmd, got, want)
		}
	}
}

func strp(s string) *string { return &s }

func TestMatches(t *testing.T) {
	r := loadEmbedded(t)
	table := contract.Resource{
		ID: "supabase.table:abcdefghijklmnopqrst/public.customers", Provider: "supabase", Kind: "supabase.table",
		ProviderID: "abcdefghijklmnopqrst/public.customers", Name: "public.customers",
		Attributes: contract.ResourceAttributes{"schema": "public", "table": "customers", "project_ref": "abcdefghijklmnopqrst"},
	}
	env := contract.Resource{
		ID: "vercel.env_var:prj_EXAMPLE000000000000000001/NEXT_PUBLIC_SUPABASE_URL/preview", Provider: "vercel", Kind: "vercel.env_var",
		ProviderID: "prj_EXAMPLE000000000000000001/NEXT_PUBLIC_SUPABASE_URL/preview", AccountName: strp("example-team"),
		Attributes: contract.ResourceAttributes{"key": "NEXT_PUBLIC_SUPABASE_URL", "env_id": "EXAMPLEenvid0000", "project_name": "example-waitlist", "project_id": "prj_EXAMPLE000000000000000001"},
	}
	dep := contract.Resource{
		ID: "vercel.deployment:dpl_EXAMPLE1", Provider: "vercel", Kind: "vercel.deployment", ProviderID: "dpl_EXAMPLE1",
		Attributes: contract.ResourceAttributes{"host": "example-waitlist-abc-example-team.vercel.app"},
	}
	cases := []struct {
		name string
		id   contract.Identifier
		res  contract.Resource
		want bool
	}{
		{"table with project", contract.Identifier{Provider: "supabase", Kind: "supabase.table", Value: "public.customers", Container: strp("abcdefghijklmnopqrst"), SignatureID: "supabase.mcp.sql"}, table, true},
		{"table other project", contract.Identifier{Provider: "supabase", Kind: "supabase.table", Value: "public.customers", Container: strp("bcdefghijklmnopqrstu"), SignatureID: "supabase.mcp.sql"}, table, false},
		{"table without project never matches", contract.Identifier{Provider: "supabase", Kind: "supabase.table", Value: "public.customers", SignatureID: "supabase.cli.db_query"}, table, false},
		{"env by MCP key in project", contract.Identifier{Provider: "vercel", Kind: "vercel.env_var", Value: "NEXT_PUBLIC_SUPABASE_URL", Container: strp("prj_EXAMPLE000000000000000001"), SignatureID: "vercel.mcp.create_project_env"}, env, true},
		{"env by CLI name", contract.Identifier{Provider: "vercel", Kind: "vercel.env_var", Value: "NEXT_PUBLIC_SUPABASE_URL", Container: strp("example-waitlist"), SignatureID: "vercel.cli.env_add"}, env, true},
		{"env by CLI name, other project", contract.Identifier{Provider: "vercel", Kind: "vercel.env_var", Value: "NEXT_PUBLIC_SUPABASE_URL", Container: strp("other"), SignatureID: "vercel.cli.env_add"}, env, false},
		{"deployment by id", contract.Identifier{Provider: "vercel", Kind: "vercel.deployment", Value: "dpl_EXAMPLE1", SignatureID: "vercel.cli.deploy"}, dep, true},
		{"deployment by host", contract.Identifier{Provider: "vercel", Kind: "vercel.deployment", Value: "example-waitlist-abc-example-team.vercel.app", SignatureID: "vercel.cli.deploy"}, dep, true},
		{"kind mismatch", contract.Identifier{Provider: "vercel", Kind: "vercel.project", Value: "dpl_EXAMPLE1", SignatureID: "vercel.cli.deploy"}, dep, false},
	}
	for _, c := range cases {
		if got := r.Matches(c.id, c.res); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseJSONResults(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"no json here", 0},
		{`  {"a":1}`, 1},
		{`[1,2]`, 1},
		{`"just a string"`, 0},
		{"noise {\"not\":\"at line start\"}\n", 0},
		// A pretty-printed value's own lines are not parsed again.
		{"{\n  \"a\": {\n\"b\": 1\n  },\n  \"c\": [\n{\"d\": 2}\n]\n}\ntrailing", 1},
		{"{\"a\":1}\nnoise\n{\"b\":2}\n[3]\n{broken\n", 3},
		{strings.Repeat("{}\n", maxResults+5), maxResults},
	}
	for _, c := range cases {
		if got := parseJSONResults(c.in); len(got) != c.want {
			t.Errorf("%q: got %d values %v, want %d", c.in, len(got), got, c.want)
		}
	}
}

// input_matches applies to MCP tools too (Stripe's generic stripe_api_write).
func TestMCPInputMatches(t *testing.T) {
	y := `format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
mcp_servers: [acme]
endpoints:
  me: { path: /v1/me }
  things: { path: /v1/things }
accounts: { endpoint: me, items: $, id: "{id}" }
kinds:
  - kind: thing
    sources: [{ endpoint: things, items: $, id: "{id}" }]
signatures:
  - id: acme.mcp.write
    kind: thing
    role: created
    when: { mcp_tools: [api_write], input_matches: { method: '^POST$', path: '^/v1/things$' } }
    extract: [{ from: result, value: "{result.id}" }]
`
	r, err := LoadFile("acme.yaml", []byte(y))
	if err != nil {
		t.Fatal(err)
	}
	post := rawAction("mcp__acme__api_write", "ok", map[string]any{"method": "POST", "path": "/v1/things"}, `{"id":"t1"}`)
	if ids := r.Extract(post); len(ids) != 1 {
		t.Errorf("POST: %+v", ids)
	}
	del := rawAction("mcp__acme__api_write", "ok", map[string]any{"method": "DELETE", "path": "/v1/things"}, `{"id":"t1"}`)
	if ids := r.Extract(del); len(ids) != 0 {
		t.Errorf("DELETE matched: %+v", ids)
	}
}
