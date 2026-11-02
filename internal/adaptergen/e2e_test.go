package adaptergen

import (
	"context"
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
	"github.com/tolgayayci/muretto/internal/openapi"
)

var update = flag.Bool("update", false, "rewrite the golden adapter files in testdata")

const fakeSecret = "MURETTO_FAKE_SECRET"

func loadSpec(t *testing.T, name string) *openapi.Doc {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := openapi.Parse(data, openapi.Options{FS: os.DirFS(filepath.Dir(path)), Path: filepath.Base(path)})
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func generate(t *testing.T, name string, opt Options) *Result {
	t.Helper()
	opt.SpecName = filepath.Base(name)
	res, err := Generate(loadSpec(t, name), opt)
	if err != nil {
		if res != nil {
			t.Logf("%s", res.YAML)
		}
		t.Fatalf("generate %s: %v", name, err)
	}
	return res
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/adaptergen -update)", err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the generated adapter (run with -update after checking the diff):\n%s", path, got)
	}
}

// ---- scanning a generated adapter against a fake API ----

type scanOut struct {
	res   adapters.ScanResult
	calls []string
}

// scan loads a generated adapter into the real engine and scans one account
// through an offline broker answering from the spec (FakeAPI).
func scan(t *testing.T, doc *openapi.Doc, res *Result, account string) scanOut {
	t.Helper()
	reg, err := adapters.LoadFile(res.Provider+".yaml", res.YAML)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	u, _ := url.Parse(res.Report.BaseURL)
	f := NewFakeAPI(doc, u.Path)
	b := broker.NewOffline(reg.Allowlist(), f.Respond)
	ctx := context.Background()
	accts, err := reg.Accounts(ctx, b, res.Provider, "tok")
	switch {
	case err == adapters.ErrAccountInput:
		accts = []adapters.Account{{ID: "typed-org", Name: "typed-org"}}
	case err != nil:
		t.Fatalf("accounts: %v", err)
	case len(accts) == 0:
		t.Fatal("no accounts")
	}
	acct := accts[0]
	if account != "" {
		acct.ID = account
	}
	sr, err := reg.Scan(ctx, b, res.Provider, adapters.Credentials{Token: "tok", AccountID: acct.ID, AccountName: acct.Name, AccountSlug: acct.Slug})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, c := range f.Unmatched {
		t.Errorf("request for an endpoint the spec does not have: %s", c)
	}
	raw, _ := json.Marshal(sr)
	if strings.Contains(string(raw), fakeSecret) {
		t.Errorf("a secret field reached the scan result: %s", raw)
	}
	for _, c := range sr.Checks {
		if c.Status != contract.ProviderCheckStatusOk {
			t.Errorf("check %s scope=%v rule=%v failed: %s", c.Kind, deref(c.Scope), deref(c.RuleID), deref(c.Error))
		}
	}
	return scanOut{res: sr, calls: f.Calls}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func kindsOf(sr adapters.ScanResult) map[string]int {
	out := map[string]int{}
	for _, r := range sr.Resources {
		out[strings.SplitN(r.Kind, ".", 2)[1]]++
	}
	return out
}

func selectedKinds(r *Report) []string {
	var out []string
	for _, k := range r.Kinds {
		if k.Selected {
			out = append(out, k.Kind)
		}
	}
	sort.Strings(out)
	return out
}

// ---- tests ----

// The same made-up API ("Acme Cloud") described in every spec version must
// give the same kinds, and each adapter must scan cleanly end to end.
func TestAcmeEveryVersion(t *testing.T) {
	want := []string{"database", "project", "webhook"}
	for _, spec := range []string{"acme-2.0.yaml", "acme-3.0.json", "acme-3.1.yaml", "acme-3.2.yaml"} {
		t.Run(spec, func(t *testing.T) {
			res := generate(t, spec, Options{})
			golden(t, strings.TrimSuffix(spec, filepath.Ext(spec))+".adapter.yaml", res.YAML)
			if got := selectedKinds(res.Report); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("kinds = %v, want %v\n%s", got, want, res.YAML)
			}
			out := scan(t, loadSpec(t, spec), res, "")
			k := kindsOf(out.res)
			// Two orgs are listed, but projects are scoped by the account,
			// databases fan out per project (2 x 2), webhooks are top-level.
			if k["project"] != 2 || k["database"] != 4 || k["webhook"] != 2 {
				t.Errorf("resources per kind = %v\ncalls: %v", k, out.calls)
			}
			// The public flag alternates, so one database is exposed and one
			// is not, per project.
			states := map[contract.ExposureFindingState]int{}
			for _, f := range out.res.Findings {
				states[f.State]++
			}
			if states[contract.ExposureFindingStateExposed] == 0 || states[contract.ExposureFindingStateNotExposed] == 0 {
				t.Errorf("findings = %v, want some exposed and some not exposed", states)
			}
			for _, c := range out.calls {
				if strings.Contains(c, "/projects?") && !strings.Contains(c, "org_id=") {
					t.Errorf("project list without the account: %s", c)
				}
			}
		})
	}
}

// A curl call that creates a database must be recognised and attributed to
// the resource the scan finds.
func TestAcmeCurlSignature(t *testing.T) {
	res := generate(t, "acme-3.1.yaml", Options{})
	if res.Provider != "acme-cloud" {
		t.Fatalf("provider = %q, want acme-cloud (from the title)", res.Provider)
	}
	reg, err := adapters.LoadFile("acme.yaml", res.YAML)
	if err != nil {
		t.Fatal(err)
	}
	act := observe.RawAction{
		Action: contract.AgentAction{ID: "toolu_FAKE", SessionID: "00000000-0000-4000-8000-00000000000a", ToolName: "Bash", Status: contract.AgentActionStatusOk, Source: contract.AgentActionSourceHook},
		Input:  map[string]any{"command": `curl -sS -X POST https://api.acme.dev/v1/projects/prj_1/databases -H "Authorization: Bearer $ACME_CLOUD_TOKEN" -d '{"name":"main"}'`},
		Output: `{"id":"db_9","name":"main","public":false}`,
	}
	ids := reg.Extract(act)
	if len(ids) != 1 || ids[0].Kind != "acme-cloud.database" || ids[0].Value != "db_9" || ids[0].Container == nil || *ids[0].Container != "prj_1" {
		t.Fatalf("identifiers = %+v", ids)
	}
	db := contract.Resource{Provider: "acme-cloud", Kind: "acme-cloud.database", ProviderID: "prj_1/db_9",
		Attributes: contract.ResourceAttributes{"database_id": "db_9", "project_id": "prj_1"}}
	if !reg.Matches(ids[0], db) {
		t.Errorf("identifier does not match the scanned database")
	}
	other := db
	other.Attributes = contract.ResourceAttributes{"database_id": "db_9", "project_id": "prj_2"}
	if reg.Matches(ids[0], other) {
		t.Errorf("identifier matched a database in another project")
	}
	// A failed create (error body) and a list call prove nothing.
	act.Output = `{"error":{"message":"quota"}}`
	if ids := reg.Extract(act); len(ids) != 0 {
		t.Errorf("error response yielded %+v", ids)
	}
	act.Input = map[string]any{"command": `curl -sS https://api.acme.dev/v1/projects/prj_1/databases`}
	act.Output = `{"data":[{"id":"db_9"}]}`
	if ids := reg.Extract(act); len(ids) != 0 {
		t.Errorf("GET list yielded %+v", ids)
	}
}

func TestOptions(t *testing.T) {
	t.Run("kinds pulls in parents", func(t *testing.T) {
		res := generate(t, "acme-3.1.yaml", Options{Kinds: []string{"database"}})
		if got := selectedKinds(res.Report); strings.Join(got, ",") != "database,project" {
			t.Errorf("kinds = %v", got)
		}
	})
	t.Run("unknown kind", func(t *testing.T) {
		_, err := Generate(loadSpec(t, "acme-3.1.yaml"), Options{Kinds: []string{"nope"}})
		if err == nil || !strings.Contains(err.Error(), "candidates: ") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("no rules", func(t *testing.T) {
		res := generate(t, "acme-3.1.yaml", Options{NoRules: true})
		if strings.Contains(string(res.YAML), "rules:") {
			t.Error("rules emitted with NoRules")
		}
	})
	t.Run("mcp server", func(t *testing.T) {
		res := generate(t, "acme-3.1.yaml", Options{MCPServer: "acme"})
		if !strings.Contains(string(res.YAML), "mcp_tools: [createDatabase, create_database]") {
			t.Errorf("no MCP signature:\n%s", res.YAML)
		}
	})
	t.Run("provider and base url", func(t *testing.T) {
		res := generate(t, "acme-3.1.yaml", Options{Provider: "acme-staging", BaseURL: "https://staging.acme.dev"})
		if res.Provider != "acme-staging" || !strings.Contains(string(res.YAML), "base_url: https://staging.acme.dev") {
			t.Errorf("provider %s:\n%s", res.Provider, res.YAML)
		}
	})
	t.Run("implicit account", func(t *testing.T) {
		res := generate(t, "acme-3.1.yaml", Options{Accounts: "none"})
		if !strings.Contains(string(res.YAML), "id: default") {
			t.Errorf("no implicit account:\n%s", res.YAML)
		}
	})
}
