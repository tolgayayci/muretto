package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
)

func loadEmbedded(t *testing.T) *Registry {
	t.Helper()
	r, err := Load(adapterfiles.Files)
	if err != nil {
		t.Fatalf("load adapters: %v", err)
	}
	return r
}

func TestLoadEmbedded(t *testing.T) {
	r := loadEmbedded(t)
	names := map[string]bool{}
	for _, p := range r.Providers() {
		names[p.Name] = true
		if p.TokenHelp == "" || len(p.Coverage) == 0 {
			t.Errorf("%s: token_help and coverage are required for shipped adapters", p.Name)
		}
	}
	for _, want := range []string{"supabase", "vercel"} {
		if !names[want] {
			t.Errorf("provider %s not loaded", want)
		}
	}
	for _, rule := range r.Allowlist() {
		if rule.Method != "GET" && rule.BodySHA256 == "" {
			t.Errorf("non-GET rule without pinned body: %+v", rule)
		}
		for _, q := range rule.Query {
			if strings.Contains(strings.ToLower(q), "decrypt") {
				t.Errorf("decrypt in allowlist: %+v", rule)
			}
		}
	}
}

// The template must stay a valid adapter, so contributors can copy it.
func TestTemplateIsValid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "adapters", "template.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter(data, "template.yaml"); err != nil {
		t.Fatalf("template.yaml: %v", err)
	}
}

func TestRejectsBadAdapters(t *testing.T) {
	base := `format: 1
provider: x-test
display_name: X
api: { base_url: https://api.example.com, auth: { header: Authorization, scheme: Bearer } }
token_help: t
accounts: { endpoint: orgs, id: "{id}" }
`
	cases := map[string]string{
		"POST without pinned body": base + `endpoints:
  orgs: { path: /orgs }
  q: { method: POST, path: /q }
kinds: []`,
		"DELETE": base + `endpoints:
  orgs: { path: /orgs, method: DELETE }
kinds: []`,
		"decrypt query": base + `endpoints:
  orgs: { path: /orgs, query: { decrypt: "true" } }
kinds: []`,
		"unknown key": base + `endpoints:
  orgs: { path: /orgs, bogus: 1 }
kinds: []`,
		"http base": strings.Replace(base, "https://", "http://", 1) + `endpoints:
  orgs: { path: /orgs }
kinds: []`,
		"stripped attribute": base + `endpoints:
  orgs: { path: /orgs }
kinds:
  - kind: thing
    sources: [{ endpoint: orgs, id: "{id}", attributes: [value] }]`,
		"bad predicate": base + `endpoints:
  orgs: { path: /orgs }
kinds:
  - kind: thing
    sources: [{ endpoint: orgs, id: "{id}", where: { field: a, equals: b, in: [c] } }]`,
		"unknown filter": base + `endpoints:
  orgs: { path: /orgs }
kinds:
  - kind: thing
    sources: [{ endpoint: orgs, id: "{id|shout}" }]`,
		"unknown preprocess step": base + `endpoints:
  orgs: { path: /orgs }
kinds:
  - kind: thing
    sources: [{ endpoint: orgs, id: "{id}" }]
signatures:
  - id: x.sql
    kind: thing
    role: created
    when: { tool: ^Bash$ }
    extract: [{ from: command, preprocess: [shell_args, mysql], pattern: 'create (?P<n>\w+)', value: "{n}" }]`,
		"referenced_if names no group": base + `endpoints:
  orgs: { path: /orgs }
kinds:
  - kind: thing
    sources: [{ endpoint: orgs, id: "{id}" }]
signatures:
  - id: x.sql
    kind: thing
    role: created
    when: { tool: ^Bash$ }
    extract: [{ from: command, preprocess: sql, pattern: 'create (?P<n>\w+)', referenced_if: ine, value: "{n}" }]`,
	}
	for name, src := range cases {
		if _, err := parseAdapter([]byte(src), name); err == nil {
			t.Errorf("%s: want a load error", name)
		}
	}
}

var _ = broker.ErrRefused
