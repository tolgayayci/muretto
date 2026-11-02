package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
)

const testdataDir = "../../adapters/testdata"

type route struct {
	Method string            `yaml:"method"`
	Path   string            `yaml:"path"`
	Query  map[string]string `yaml:"query"`
	Status int               `yaml:"status"`
	File   string            `yaml:"file"`
	Body   string            `yaml:"body"`
}

type expectation struct {
	Account   struct{ ID, Name, Slug string } `yaml:"account"`
	Options   []string                        `yaml:"options"`
	Resources []string                        `yaml:"resources"`
	Fields    map[string]map[string]any       `yaml:"fields"`
	Findings  map[string]string               `yaml:"findings"`
	Fixes     map[string]string               `yaml:"fixes"`
	Checks    []string                        `yaml:"checks"`
	Forbidden []string                        `yaml:"forbidden"`
}

// fixtureServer serves a scenario's routes. Every request it sees has
// already passed the broker; it checks each one again against the allowlist
// and records anything unexpected.
type fixtureServer struct {
	t      *testing.T
	dir    string
	routes []route
	allow  *broker.Broker

	mu        sync.Mutex
	requests  []string
	problems  []string
	unmatched []string
}

func (f *fixtureServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	var best *route
	bestScore := -1
	for i := range f.routes {
		rt := &f.routes[i]
		m := rt.Method
		if m == "" {
			m = "GET"
		}
		if m != r.Method || rt.Path != r.URL.Path {
			continue
		}
		q := r.URL.Query()
		ok := true
		for k, v := range rt.Query {
			ok = ok && q.Get(k) == v
		}
		if ok && len(rt.Query) > bestScore {
			best, bestScore = rt, len(rt.Query)
		}
	}
	if best == nil {
		f.unmatched = append(f.unmatched, r.Method+" "+r.URL.RequestURI())
		http.Error(w, "no fixture", http.StatusNotImplemented)
		return
	}
	body := []byte(best.Body)
	if best.File != "" {
		var err error
		body, err = os.ReadFile(filepath.Join(f.dir, best.File))
		if err != nil {
			f.problems = append(f.problems, err.Error())
		}
	}
	status := best.Status
	if status == 0 {
		status = 200
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// redirectTransport sends the broker's (real-host, https) requests to the
// fixture server, after re-checking them against an independent broker built
// from the same allowlist.
type redirectTransport struct {
	f      *fixtureServer
	target *url.URL
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := rt.f.allow.Check(req); err != nil {
		rt.f.mu.Lock()
		rt.f.problems = append(rt.f.problems, "request outside allowlist reached the network: "+err.Error())
		rt.f.mu.Unlock()
		return nil, err
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		rt.f.mu.Lock()
		rt.f.problems = append(rt.f.problems, "non-read method "+req.Method)
		rt.f.mu.Unlock()
	}
	out := req.Clone(req.Context())
	out.URL.Scheme = rt.target.Scheme
	out.URL.Host = rt.target.Host
	out.Host = ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		out.Body = io.NopCloser(strings.NewReader(string(data)))
	}
	return http.DefaultTransport.RoundTrip(out)
}

type scenario struct {
	provider, name, dir string
}

func scenarios(t *testing.T) []scenario {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(testdataDir, "*", "scan", "*"))
	if err != nil {
		t.Fatal(err)
	}
	var out []scenario
	for _, d := range dirs {
		parts := strings.Split(filepath.ToSlash(d), "/")
		out = append(out, scenario{provider: parts[len(parts)-3], name: parts[len(parts)-1], dir: d})
	}
	if len(out) == 0 {
		t.Fatal("no scan scenarios found")
	}
	return out
}

func runScenario(t *testing.T, r *Registry, sc scenario) (ScanResult, expectation, *fixtureServer) {
	t.Helper()
	var routes []route
	readYAML(t, filepath.Join(sc.dir, "routes.yaml"), &routes)
	var exp expectation
	readYAML(t, filepath.Join(sc.dir, "expect.yaml"), &exp)

	f := &fixtureServer{t: t, dir: sc.dir, routes: routes, allow: broker.New(nil, r.Allowlist())}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	b := broker.New(&http.Client{Transport: &redirectTransport{f: f, target: target}}, r.Allowlist())

	opts := map[string]bool{}
	for _, o := range exp.Options {
		opts[o] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := r.Scan(ctx, b, sc.provider, Credentials{
		Token:       "fake-test-token",
		AccountID:   exp.Account.ID,
		AccountName: exp.Account.Name,
		AccountSlug: exp.Account.Slug,
		Options:     opts,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return res, exp, f
}

func TestScanFixtures(t *testing.T) {
	r := loadEmbedded(t)
	for _, sc := range scenarios(t) {
		t.Run(sc.provider+"/"+sc.name, func(t *testing.T) {
			res, exp, f := runScenario(t, r, sc)
			for _, p := range f.problems {
				t.Error(p)
			}
			for _, u := range f.unmatched {
				t.Errorf("request with no fixture: %s", u)
			}
			if len(f.requests) == 0 {
				t.Error("scan made no requests")
			}

			var gotIDs []string
			byID := map[string]contract.Resource{}
			for _, res := range res.Resources {
				gotIDs = append(gotIDs, res.ID)
				byID[res.ID] = res
			}
			assertSameSet(t, "resources", gotIDs, exp.Resources)

			for id, fields := range exp.Fields {
				res, ok := byID[id]
				if !ok {
					t.Errorf("fields: resource %s missing", id)
					continue
				}
				for k, want := range fields {
					got := fieldOf(res, k)
					if fmt.Sprint(got) != fmt.Sprint(want) {
						t.Errorf("%s %s = %v, want %v", id, k, got, want)
					}
				}
			}

			gotF := map[string]string{}
			fixes := map[string]string{}
			for _, fi := range res.Findings {
				gotF[fi.ID] = string(fi.State)
				fixes[fi.ID] = fi.Fix.Text
				if fi.State != contract.ExposureFindingStateUnknown && fi.CheckedAt == nil {
					t.Errorf("%s: decided finding without checked_at", fi.ID)
				}
				if _, ok := byID[fi.ResourceID]; !ok {
					t.Errorf("%s: finding for unknown resource", fi.ID)
				}
			}
			if exp.Findings == nil {
				exp.Findings = map[string]string{}
			}
			assertSameMap(t, "findings", gotF, exp.Findings)
			for id, want := range exp.Fixes {
				if strings.TrimSpace(fixes[id]) != want {
					t.Errorf("fix %s = %q, want %q", id, fixes[id], want)
				}
			}

			var gotC []string
			for _, c := range res.Checks {
				gotC = append(gotC, checkKey(c))
			}
			assertSameMultiset(t, "checks", gotC, exp.Checks)

			raw, _ := json.Marshal(res)
			for _, bad := range exp.Forbidden {
				if strings.Contains(string(raw), bad) {
					t.Errorf("scan result contains forbidden %q", bad)
				}
			}
		})
	}
}

// TestScanRequestsMatchAllowlist runs every scenario and asserts that every
// request that reached the (fixture) network matched the allowlist, and that
// the set of allowlisted endpoints is exactly what the adapters declare.
func TestScanRequestsMatchAllowlist(t *testing.T) {
	r := loadEmbedded(t)
	check := broker.New(nil, r.Allowlist())
	// Each provider's host comes from its own adapter file (api.acme.dev,
	// console.neon.tech, ...), never from its name.
	hosts := map[string]string{}
	for _, rule := range r.Allowlist() {
		hosts[rule.Provider] = rule.Host
	}
	for _, sc := range scenarios(t) {
		_, _, f := runScenario(t, r, sc)
		for _, p := range f.problems {
			t.Errorf("%s/%s: %s", sc.provider, sc.name, p)
		}
		for _, line := range f.requests {
			method, uri, _ := strings.Cut(line, " ")
			host := hosts[sc.provider]
			req, _ := broker.NewRequest(context.Background(), method, "https://"+host+uri, nil)
			if method == http.MethodPost {
				// The fixture server saw the pinned body; Check needs it again.
				for _, rule := range check.Rules() {
					if rule.Method == http.MethodPost && rule.Host == host {
						for _, name := range []string{"supabase_rls_catalog_v1"} {
							if body, sha, ok := broker.PinnedBody(name); ok && sha == rule.BodySHA256 {
								req, _ = broker.NewRequest(context.Background(), method, "https://"+host+uri, body)
							}
						}
					}
				}
			}
			if _, err := check.Check(req); err != nil {
				t.Errorf("%s/%s: %s does not match the allowlist: %v", sc.provider, sc.name, line, err)
			}
		}
	}
}

func checkKey(c contract.ProviderCheck) string {
	scope, rule := "", ""
	if c.Scope != nil {
		scope = *c.Scope
	}
	if c.RuleID != nil {
		rule = *c.RuleID
	}
	status := string(c.Status)
	if c.ErrorCode != nil {
		status += ":" + string(*c.ErrorCode)
	}
	return c.Kind + "|" + scope + "|" + rule + "|" + status
}

func fieldOf(r contract.Resource, k string) any {
	str := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	switch k {
	case "_name":
		return r.Name
	case "_status":
		return str(r.Status)
	case "_url":
		return str(r.Url)
	case "_parent_id":
		return str(r.ParentID)
	case "_seen_via":
		return string(r.SeenVia)
	case "_created_at":
		if r.CreatedAt == nil {
			return nil
		}
		return r.CreatedAt.Format(time.RFC3339)
	}
	v, ok := r.Attributes[k]
	if !ok {
		return "<absent>"
	}
	return v
}

func readYAML(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && err != io.EOF {
		t.Fatalf("%s: %v", path, err)
	}
}

func assertSameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := map[string]bool{}, map[string]bool{}
	for _, s := range got {
		if g[s] {
			t.Errorf("%s: duplicate %s", what, s)
		}
		g[s] = true
	}
	for _, s := range want {
		w[s] = true
	}
	for s := range w {
		if !g[s] {
			t.Errorf("%s: missing %s", what, s)
		}
	}
	for s := range g {
		if !w[s] {
			t.Errorf("%s: unexpected %s", what, s)
		}
	}
}

func assertSameMap(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s = %q, want %q", what, k, got[k], v)
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected %s = %s", what, k, v)
		}
	}
}

func assertSameMultiset(t *testing.T, what string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, "\n") != strings.Join(w, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(g, "\n  "), strings.Join(w, "\n  "))
	}
}

// TestFindingTextFollowsState: an adapter's title, explanation and fix are
// written for the exposed state. A not_exposed or unknown finding must not
// repeat "is publicly readable" next to a check mark or question mark.
func TestFindingTextFollowsState(t *testing.T) {
	r := loadEmbedded(t)
	a := r.byName["supabase"]
	var table *kindSpec
	for _, k := range a.Kinds {
		if k.Kind == "table" {
			table = k
		}
	}
	if table == nil {
		t.Fatal("supabase adapter has no table kind")
	}
	parent := &scanned{childCtx: map[string]any{"db_schema": "public"}}
	mk := func(name string, rls bool, p *scanned) *scanned {
		return &scanned{
			res: contract.Resource{
				ID:   "supabase.table:abcdefghijklmnopqrst/public." + name,
				Kind: "supabase.table", Name: "public." + name,
				Attributes: map[string]any{
					"schema": "public", "table": name,
					"rls_enabled": rls, "anon_select": true, "authenticated_select": false,
				},
			},
			kind: table, parent: p,
		}
	}
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	s := &scanner{a: a, now: now, order: []*scanned{
		mk("customers", false, parent), // exposed
		mk("orders", true, parent),     // not exposed
		mk("events", false, nil),       // no parent: exposed schemas unknown
	}}
	s.evaluateRules()

	get := func(name string) contract.ExposureFinding {
		t.Helper()
		f, ok := s.findings["rls_disabled:supabase.table:abcdefghijklmnopqrst/public."+name]
		if !ok {
			t.Fatalf("no rls_disabled finding for %s; have %v", name, s.findings)
		}
		return f
	}

	exp := get("customers")
	if exp.State != contract.ExposureFindingStateExposed {
		t.Fatalf("customers state = %s, want exposed", exp.State)
	}
	if exp.Title != "RLS off: public.customers is publicly readable" {
		t.Errorf("exposed title changed: %q", exp.Title)
	}
	if !strings.Contains(exp.Fix.Text, "ENABLE ROW LEVEL SECURITY") {
		t.Errorf("exposed fix lost the SQL: %q", exp.Fix.Text)
	}

	ne := get("orders")
	if ne.State != contract.ExposureFindingStateNotExposed {
		t.Fatalf("orders state = %s, want not_exposed", ne.State)
	}
	if ne.Title != "Row level security: not exposed" {
		t.Errorf("not_exposed title = %q", ne.Title)
	}
	if ne.Explanation != NotExposedExplanation || ne.Fix.Text != "Nothing to fix." {
		t.Errorf("not_exposed explanation/fix = %q / %q", ne.Explanation, ne.Fix.Text)
	}
	if ne.CheckedAt == nil {
		t.Error("not_exposed finding without checked_at")
	}

	unk := get("events")
	if unk.State != contract.ExposureFindingStateUnknown {
		t.Fatalf("events state = %s, want unknown", unk.State)
	}
	if unk.Title != "Row level security: exposure unknown" {
		t.Errorf("unknown title = %q", unk.Title)
	}
	if !strings.Contains(unk.Explanation, "could not decide") {
		t.Errorf("unknown explanation = %q", unk.Explanation)
	}

	for _, f := range []contract.ExposureFinding{ne, unk} {
		for _, claim := range []string{"publicly readable", "Anyone with"} {
			if strings.Contains(f.Title+" "+f.Explanation, claim) {
				t.Errorf("%s finding repeats the exposed claim %q: %q / %q", f.State, claim, f.Title, f.Explanation)
			}
		}
	}
}
