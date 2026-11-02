// Package adaptercheck runs an adapter's test fixtures offline (scan
// scenarios and extract cases, adapters/WRITING.md §9) and checks the
// rules every adapter shipped in adapters/ must meet. CI, `muretto adapter
// test` and contributors run the same code, through an offline broker, so a
// check can never reach a provider.
package adaptercheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

// FakeSecret is the marker fixtures put in every field an adapter must strip.
const FakeSecret = "MURETTO_FAKE_SECRET" // #nosec G101 -- a fixture marker, not a credential

// Route is one fixture response (routes.yaml).
type Route struct {
	Method string            `yaml:"method,omitempty"`
	Path   string            `yaml:"path"`
	Query  map[string]string `yaml:"query,omitempty"`
	Status int               `yaml:"status,omitempty"`
	File   string            `yaml:"file,omitempty"`
	Body   string            `yaml:"body,omitempty"`
}

// Account is the account a scenario scans.
type Account struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name,omitempty"`
	Slug string `yaml:"slug,omitempty"`
}

// Expectation is a scenario's expect.yaml.
type Expectation struct {
	Account   Account                   `yaml:"account"`
	Options   []string                  `yaml:"options,omitempty"`
	Resources []string                  `yaml:"resources"`
	Fields    map[string]map[string]any `yaml:"fields,omitempty"`
	Findings  map[string]string         `yaml:"findings"`
	Checks    []string                  `yaml:"checks"`
	Forbidden []string                  `yaml:"forbidden,omitempty"`
}

// ExpectedID is one identifier an extract case must yield.
type ExpectedID struct {
	Kind      string `yaml:"kind"`
	Value     string `yaml:"value"`
	Container string `yaml:"container,omitempty"`
	Role      string `yaml:"role"`
}

// ExtractCase is one tool call and the identifiers it must yield.
type ExtractCase struct {
	Name   string            `yaml:"name"`
	Tool   string            `yaml:"tool"`
	Status string            `yaml:"status,omitempty"`
	Input  any               `yaml:"input"`
	Output string            `yaml:"output"`
	MCP    map[string]string `yaml:"mcp,omitempty"`
	Guess  string            `yaml:"guess,omitempty"`
	Expect []ExpectedID      `yaml:"expect"`
	// Matches lists the scan-fixture resources (by id) that the case's
	// created identifiers must match through the adapter's match rules:
	// proof that attribution finds what the agent created.
	Matches []string `yaml:"matches,omitempty"`
}

// grandfathered adapters shipped before the matches rule (2026-11-01);
// their attribution is covered by internal/adapters contract tests.
var grandfathered = map[string]bool{"supabase": true, "vercel": true}

// Scenario is the result of one scan scenario.
type Scenario struct {
	Name      string
	Requests  int
	Resources int
	Findings  int
	Problems  []string
}

// Report is everything one check found.
type Report struct {
	Provider     string
	Dir          string
	Scenarios    []Scenario
	Cases        int
	CaseProblems []string
	Rules        []string // shipping-rule problems (strict only)
}

// OK reports whether nothing failed.
func (r *Report) OK() bool {
	if len(r.CaseProblems) > 0 || len(r.Rules) > 0 {
		return false
	}
	for _, s := range r.Scenarios {
		if len(s.Problems) > 0 {
			return false
		}
	}
	return true
}

// Problems lists every failure as "where: what".
func (r *Report) Problems() []string {
	var out []string
	for _, s := range r.Scenarios {
		for _, p := range s.Problems {
			out = append(out, "scan/"+s.Name+": "+p)
		}
	}
	for _, p := range r.CaseProblems {
		out = append(out, "extract: "+p)
	}
	for _, p := range r.Rules {
		out = append(out, "rule: "+p)
	}
	return out
}

// Run checks provider's fixtures in dir (adapters/<provider>/testdata):
// every scan scenario through an offline broker and every extract case.
// strict adds the rules an adapter shipped with Muretto must meet.
func Run(reg *adapters.Registry, provider, dir string, strict bool) (*Report, error) {
	rep := &Report{Provider: provider, Dir: dir}
	scenDirs, _ := filepath.Glob(filepath.Join(dir, "scan", "*"))
	sort.Strings(scenDirs)
	hasFailure, hasForbidden := false, false
	resources := map[string]contract.Resource{}
	for _, d := range scenDirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		sc, exp, res := runScenario(reg, provider, d)
		for _, r := range res {
			if _, ok := resources[r.ID]; !ok {
				resources[r.ID] = r
			}
		}
		rep.Scenarios = append(rep.Scenarios, sc)
		for _, c := range exp.Checks {
			if strings.Contains(c, "|error") {
				hasFailure = true
			}
		}
		for _, f := range exp.Forbidden {
			if f == FakeSecret && fixturesContain(d, FakeSecret) {
				hasForbidden = true
			}
		}
	}

	files, _ := filepath.Glob(filepath.Join(dir, "extract", "*.yaml"))
	sort.Strings(files)
	seenSig := map[string]bool{}
	matchedSig := map[string]bool{}
	negative := false
	for _, f := range files {
		var cases []ExtractCase
		if err := readYAML(f, &cases); err != nil {
			rep.CaseProblems = append(rep.CaseProblems, err.Error())
			continue
		}
		for _, c := range cases {
			rep.Cases++
			if len(c.Expect) == 0 {
				negative = true
			}
			for _, p := range runCase(reg, provider, c, seenSig, matchedSig, resources) {
				rep.CaseProblems = append(rep.CaseProblems, fmt.Sprintf("%s: %s", c.Name, p))
			}
		}
	}

	if strict {
		rule := func(format string, args ...any) { rep.Rules = append(rep.Rules, fmt.Sprintf(format, args...)) }
		if len(rep.Scenarios) == 0 {
			rule("no scan scenarios in %s (muretto adapter new --fixtures writes a starter set)", filepath.Join(dir, "scan"))
		} else if !hasFailure {
			rule("no failure scenario: add one where an endpoint answers 403 or 500 and expect.yaml lists the error check")
		}
		strips := false
		for _, r := range reg.Allowlist() {
			if r.Provider == provider && len(r.Strip) > 0 {
				strips = true
			}
		}
		if strips && !hasForbidden {
			rule("the adapter strips secret fields, but no scenario puts %s in them and lists it under forbidden", FakeSecret)
		}
		sigs := reg.SignatureIDs(provider)
		if len(sigs) > 0 && rep.Cases == 0 {
			rule("no extract cases in %s", filepath.Join(dir, "extract"))
		}
		for _, id := range sigs {
			if !seenSig[id] && rep.Cases > 0 {
				rule("signature %s is not exercised by any extract case", id)
			}
		}
		if rep.Cases > 0 && !negative {
			rule("no extract case shows a call that proves nothing (expect: []), such as a failed or read-only call")
		}
		if !grandfathered[provider] {
			for _, id := range sigs {
				if reg.SignatureRole(provider, id) == "created" && seenSig[id] && !matchedSig[id] {
					rule("signature %s has no extract case with matches: (the fixture resource its call creates)", id)
				}
			}
		}
	}
	return rep, nil
}

func fixturesContain(dir, s string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return nil //nolint:nilerr // skip unreadable entries; best-effort search
		}
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(s)) { // #nosec G304 G122 -- read-only scan of a fixture directory
			found = true
		}
		return nil
	})
	return found
}

func readYAML(path string, v any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- fixture file
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func runScenario(reg *adapters.Registry, provider, dir string) (Scenario, Expectation, []contract.Resource) {
	sc := Scenario{Name: filepath.Base(dir)}
	bad := func(format string, args ...any) { sc.Problems = append(sc.Problems, fmt.Sprintf(format, args...)) }
	var routes []Route
	var exp Expectation
	if err := readYAML(filepath.Join(dir, "routes.yaml"), &routes); err != nil {
		bad("%v", err)
		return sc, exp, nil
	}
	if err := readYAML(filepath.Join(dir, "expect.yaml"), &exp); err != nil {
		bad("%v", err)
		return sc, exp, nil
	}
	var unmatched []string
	respond := func(method, path, rawQuery string) (int, []byte) {
		sc.Requests++
		q, _ := url.ParseQuery(rawQuery)
		var best *Route
		bestScore := -1
		for i := range routes {
			rt := &routes[i]
			m := rt.Method
			if m == "" {
				m = "GET"
			}
			if m != method || rt.Path != path {
				continue
			}
			ok := true
			for k, v := range rt.Query {
				ok = ok && q.Get(k) == v
			}
			if ok && len(rt.Query) > bestScore {
				best, bestScore = rt, len(rt.Query)
			}
		}
		if best == nil {
			u := path
			if rawQuery != "" {
				u += "?" + rawQuery
			}
			unmatched = append(unmatched, method+" "+u)
			return 501, []byte(`{"error":"no fixture"}`)
		}
		body := []byte(best.Body)
		if best.File != "" {
			// Scenarios may share files (../basic/x.json), but only inside
			// the provider's testdata directory.
			full := filepath.Clean(filepath.Join(dir, best.File))
			root := filepath.Clean(filepath.Dir(filepath.Dir(dir))) + string(filepath.Separator)
			if filepath.IsAbs(best.File) || !strings.HasPrefix(full, root) {
				bad("route file %q must be inside %s", best.File, root)
				return 500, nil
			}
			b, err := os.ReadFile(full) // #nosec G304 -- inside the provider's testdata directory
			if err != nil {
				bad("%v", err)
			}
			body = b
		}
		status := best.Status
		if status == 0 {
			status = 200
		}
		return status, body
	}
	b := broker.NewOffline(reg.Allowlist(), respond)
	opts := map[string]bool{}
	for _, o := range exp.Options {
		opts[o] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := reg.Scan(ctx, b, provider, adapters.Credentials{
		Token: "fake-test-token", AccountID: exp.Account.ID, AccountName: exp.Account.Name, AccountSlug: exp.Account.Slug, Options: opts,
	})
	if err != nil {
		bad("scan: %v", err)
		return sc, exp, nil
	}
	for _, u := range unmatched {
		bad("request with no fixture: %s", u)
	}
	if sc.Requests == 0 {
		bad("the scan made no requests")
	}
	for _, c := range res.Checks {
		if c.Error != nil && strings.Contains(*c.Error, "request refused") {
			bad("the broker refused a request the adapter made (%s): %s", c.Kind, *c.Error)
		}
	}
	sc.Resources, sc.Findings = len(res.Resources), len(res.Findings)

	var got []string
	byID := map[string]contract.Resource{}
	for _, r := range res.Resources {
		got = append(got, r.ID)
		byID[r.ID] = r
	}
	sameSet(bad, "resources", got, exp.Resources)
	for id, fields := range exp.Fields {
		r, ok := byID[id]
		if !ok {
			bad("fields: resource %s missing", id)
			continue
		}
		for k, want := range fields {
			if g := FieldOf(r, k); fmt.Sprint(g) != fmt.Sprint(want) {
				bad("%s %s = %v, want %v", id, k, g, want)
			}
		}
	}
	gotF := map[string]string{}
	for _, f := range res.Findings {
		gotF[f.ID] = string(f.State)
	}
	if exp.Findings == nil {
		exp.Findings = map[string]string{}
	}
	for k, v := range exp.Findings {
		if gotF[k] != v {
			bad("findings: %s = %q, want %q", k, gotF[k], v)
		}
	}
	for k, v := range gotF {
		if _, ok := exp.Findings[k]; !ok {
			bad("findings: unexpected %s = %s", k, v)
		}
	}
	var gotC []string
	for _, c := range res.Checks {
		gotC = append(gotC, CheckKey(c))
	}
	sort.Strings(gotC)
	wantC := append([]string(nil), exp.Checks...)
	sort.Strings(wantC)
	if strings.Join(gotC, "\n") != strings.Join(wantC, "\n") {
		bad("checks:\n    got:  %s\n    want: %s", strings.Join(gotC, "\n          "), strings.Join(wantC, "\n          "))
	}
	raw, _ := json.Marshal(res)
	for _, f := range exp.Forbidden {
		if strings.Contains(string(raw), f) {
			bad("the scan result contains forbidden %q (a field that should be stripped is not)", f)
		}
	}
	return sc, exp, res.Resources
}

func sameSet(bad func(string, ...any), what string, got, want []string) {
	g, w := map[string]bool{}, map[string]bool{}
	for _, s := range got {
		if g[s] {
			bad("%s: duplicate %s", what, s)
		}
		g[s] = true
	}
	for _, s := range want {
		w[s] = true
	}
	var missing, extra []string
	for s := range w {
		if !g[s] {
			missing = append(missing, s)
		}
	}
	for s := range g {
		if !w[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	for _, s := range missing {
		bad("%s: missing %s", what, s)
	}
	for _, s := range extra {
		bad("%s: unexpected %s", what, s)
	}
}

// CheckKey is a check as expect.yaml writes it: kind|scope|rule|status.
func CheckKey(c contract.ProviderCheck) string {
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

// FieldOf reads a resource field as expect.yaml names it: _name, _status,
// _url, _parent_id, _seen_via, _created_at, or an attribute.
func FieldOf(r contract.Resource, k string) any {
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

// Action builds the tool call an extract case describes.
func Action(c ExtractCase) observe.RawAction {
	st := contract.AgentActionStatusOk
	if c.Status == "error" {
		st = contract.AgentActionStatusError
	}
	return observe.RawAction{
		Action: contract.AgentAction{ID: "toolu_FAKE", SessionID: "00000000-0000-4000-8000-00000000000a", ToolName: c.Tool, Status: st, Source: contract.AgentActionSourceHook},
		Input:  c.Input,
		Output: c.Output,
		MCP:    c.MCP,
	}
}

// Expected turns identifiers into the form extract cases write.
func Expected(ids []contract.Identifier) []ExpectedID {
	out := []ExpectedID{}
	for _, id := range ids {
		e := ExpectedID{Kind: id.Kind, Value: id.Value, Role: string(id.Role)}
		if id.Container != nil {
			e.Container = *id.Container
		}
		out = append(out, e)
	}
	return out
}

func runCase(reg *adapters.Registry, provider string, c ExtractCase, seen, matched map[string]bool, resources map[string]contract.Resource) []string {
	var problems []string
	a := Action(c)
	key := func(e ExpectedID) string { return e.Kind + " " + e.Value + " @" + e.Container + " " + e.Role }
	var got, want []string
	gotMatches := map[string]bool{}
	for _, id := range reg.Extract(a) {
		if id.Provider != provider {
			continue // another adapter's signature
		}
		seen[id.SignatureID] = true
		if id.Role == contract.IdentifierRoleCreated && len(c.Matches) > 0 {
			for rid, r := range resources {
				if reg.Matches(id, r) {
					gotMatches[rid] = true
					matched[id.SignatureID] = true
				}
			}
		}
		got = append(got, key(Expected([]contract.Identifier{id})[0]))
		if id.Excerpt == nil || *id.Excerpt == "" || len(*id.Excerpt) > 200 {
			problems = append(problems, "identifier "+id.Value+" needs a short excerpt")
		}
	}
	for _, e := range c.Expect {
		want = append(want, key(e))
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		problems = append(problems, fmt.Sprintf("identifiers:\n    got:  %s\n    want: %s", strings.Join(got, "\n          "), strings.Join(want, "\n          ")))
	}
	if c.Guess != "" {
		if g := reg.ProviderGuess(a); g != c.Guess {
			problems = append(problems, fmt.Sprintf("ProviderGuess = %q, want %q", g, c.Guess))
		}
	}
	if len(c.Matches) > 0 {
		for _, rid := range c.Matches {
			if _, ok := resources[rid]; !ok {
				problems = append(problems, "matches: "+rid+" is not a resource in any scan scenario")
			} else if !gotMatches[rid] {
				problems = append(problems, "matches: the created identifiers do not match "+rid+" (check the kind's match rule and the container)")
			}
		}
		want := map[string]bool{}
		for _, rid := range c.Matches {
			want[rid] = true
		}
		for rid := range gotMatches {
			if !want[rid] {
				problems = append(problems, "matches: the created identifiers also match "+rid+", which the case does not list")
			}
		}
	}
	return problems
}
