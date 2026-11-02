package adaptergen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/adaptercheck"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
)

// Fixtures writes starter test fixtures for a generated adapter, in the
// layout of adapters/<provider>/testdata/ (adapters/WRITING.md §9):
//
//	scan/basic/          one scan answered with fake bodies built from the spec
//	scan/failures/       the same scan with one endpoint answering 403
//	extract/cases.yaml   a create call per signature, a failed one, a read-only one
//
// The expectations are what the adapter does with those bodies, so the
// fixtures pass as written and pin today's behaviour. Replace the bodies with
// recorded, sanitized responses before relying on them. Keys are paths
// relative to the fixtures directory.
func Fixtures(res *Result) (map[string][]byte, error) {
	g := res.g
	if g == nil {
		return nil, errors.New("fixtures need a result from Generate")
	}
	reg, err := adapters.LoadFile(res.Provider+".yaml", res.YAML)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	files := map[string][]byte{}
	header := fmt.Sprintf("# Starter fixtures from `muretto adapter new --fixtures` (%s).\n"+
		"# Bodies are fake values built from the spec's schemas, and the expectations\n"+
		"# are what the adapter does with them. Replace them with recorded, sanitized\n"+
		"# responses (adapters/FIXTURES.md) before relying on them.\n", g.rep.Spec)

	acct := adaptercheck.Account{ID: "example-account", Name: "Example account"}
	fa := NewFakeAPI(g.doc, g.base)
	accts, err := reg.Accounts(ctx, broker.NewOffline(reg.Allowlist(), fa.Respond), res.Provider, "fake-test-token")
	switch {
	case err == nil && len(accts) > 0:
		acct = adaptercheck.Account{ID: accts[0].ID, Name: accts[0].Name, Slug: accts[0].Slug}
	case err != nil && !errors.Is(err, adapters.ErrAccountInput):
		return nil, fmt.Errorf("accounts: %w", err)
	}

	type rec struct {
		method, path, query string
		status              int
		body                []byte
	}
	var basicRes []contract.Resource
	scenario := func(name, failPath string) ([]rec, error) {
		fake := NewFakeAPI(g.doc, g.base)
		var recs []rec
		respond := func(m, p, q string) (int, []byte) {
			var st int
			var b []byte
			if p == failPath {
				st, b = 403, []byte(`{"error":{"code":"forbidden","message":"example: the token cannot read this"}}`)
			} else {
				st, b = fake.Respond(m, p, q)
			}
			recs = append(recs, rec{m, p, q, st, b})
			return st, b
		}
		sr, err := reg.Scan(ctx, broker.NewOffline(reg.Allowlist(), respond), res.Provider, adapters.Credentials{
			Token: "fake-test-token", AccountID: acct.ID, AccountName: acct.Name, AccountSlug: acct.Slug,
		})
		if err != nil {
			return nil, err
		}
		if name == "basic" {
			basicRes = sr.Resources
		}
		if len(fake.Unmatched) > 0 {
			return nil, fmt.Errorf("the adapter requested endpoints the spec does not have: %s", strings.Join(fake.Unmatched, ", "))
		}
		dir := "scan/" + name + "/"
		var routes []adaptercheck.Route
		marker := false
		for i, r := range recs {
			q := map[string]string{}
			vals, _ := url.ParseQuery(r.query)
			for k := range vals {
				q[k] = vals.Get(k)
			}
			rt := adaptercheck.Route{Path: r.path, Query: q}
			if r.status != 200 {
				rt.Status, rt.Body = r.status, string(r.body)
			} else {
				// Pretty-print small bodies for review; keep big ones as
				// served (indenting can push them past the broker's cap).
				out := r.body
				if len(r.body) < 256<<10 {
					var v any
					if json.Unmarshal(r.body, &v) == nil {
						out, _ = json.MarshalIndent(v, "", "  ")
					}
				}
				fname := fmt.Sprintf("%02d-%s.json", i+1, lastStatic(r.path))
				files[dir+fname] = append(append([]byte(nil), out...), '\n')
				rt.File = fname
			}
			if bytes.Contains(r.body, []byte(adaptercheck.FakeSecret)) {
				marker = true
			}
			routes = append(routes, rt)
		}
		exp := adaptercheck.Expectation{Account: acct, Findings: map[string]string{}, Resources: []string{}, Checks: []string{}}
		for _, r := range sr.Resources {
			exp.Resources = append(exp.Resources, r.ID)
		}
		sort.Strings(exp.Resources)
		for _, f := range sr.Findings {
			exp.Findings[f.ID] = string(f.State)
		}
		for _, c := range sr.Checks {
			exp.Checks = append(exp.Checks, adaptercheck.CheckKey(c))
		}
		sort.Strings(exp.Checks)
		if marker {
			exp.Forbidden = []string{adaptercheck.FakeSecret}
		}
		files[dir+"routes.yaml"] = yamlFile(header, routes)
		files[dir+"expect.yaml"] = yamlFile(header, exp)
		return recs, nil
	}
	basic, err := scenario("basic", "")
	if err != nil {
		return nil, err
	}
	if len(basic) == 0 {
		return nil, errors.New("the adapter made no requests")
	}
	// Fail the last request: a child list when there is one, so the rest of
	// the scan still succeeds and the error check is scoped.
	if _, err := scenario("failures", basic[len(basic)-1].path); err != nil {
		return nil, err
	}

	cases, err := g.extractCases(reg, basicRes)
	if err != nil {
		return nil, err
	}
	files["extract/cases.yaml"] = yamlFile(header, cases)
	return files, nil
}

func lastStatic(p string) string {
	segs := segments(p)
	for i := len(segs) - 1; i >= 0; i-- {
		if !strings.HasPrefix(segs[i], "example-") && !isVersionSeg(segs[i]) {
			return strings.ReplaceAll(ident(segs[i]), "_", "-")
		}
	}
	return "root"
}

func yamlFile(header string, v any) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	_ = enc.Encode(v)
	_ = enc.Close()
	return b.Bytes()
}

// exampleURLWith is exampleURL with the parameter at position pos set to val.
func exampleURLWith(path string, pos int, val string) string {
	var out []string
	i := 0
	for _, s := range strings.Split(path, "/") {
		if isParam(s) {
			if i == pos {
				s = val
			} else {
				s = "example-" + strings.ReplaceAll(snake(paramName(s)), "_", "-")
			}
			i++
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// exampleURL fills a path's parameters with fake values.
func exampleURL(path string) string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if isParam(s) {
			s = "example-" + strings.ReplaceAll(snake(paramName(s)), "_", "-")
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

func (g *gen) curlAuth() string {
	if g.authS != "" {
		return fmt.Sprintf(`-H "%s: %s [REDACTED]"`, g.authH, g.authS)
	}
	return fmt.Sprintf(`-H "%s: [REDACTED]"`, g.authH)
}

// extractCases writes one case per signature (its own create call), a failed
// create, and a read-only call. Each case's expectation is what the adapter
// extracts; a signature that does not fire on its own call is a bug.
func (g *gen) extractCases(reg *adapters.Registry, resources []contract.Resource) ([]adaptercheck.ExtractCase, error) {
	fake := NewFakeAPI(g.doc, g.base)
	extract := func(c adaptercheck.ExtractCase) []adaptercheck.ExpectedID {
		var ids []contract.Identifier
		for _, id := range reg.Extract(adaptercheck.Action(c)) {
			if id.Provider == g.slug {
				ids = append(ids, id)
			}
		}
		return adaptercheck.Expected(ids)
	}
	cases := []adaptercheck.ExtractCase{}
	for _, si := range g.sigs {
		// Build the create call around a resource the basic scan found, so
		// the case can prove attribution matches it (matches:).
		var target *contract.Resource
		for i := range resources {
			if resources[i].Kind == g.slug+"."+si.kind {
				target = &resources[i]
				break
			}
		}
		rawID, container := "example-new-"+strings.ReplaceAll(si.kind, "_", "-"), ""
		if target != nil {
			rawID = target.ProviderID
			if si.col != nil && si.col.parent != nil {
				if v, ok := target.Attributes[si.col.idAttr]; ok {
					rawID = fmt.Sprint(v)
				}
				if len(si.col.ancestors) > 0 {
					container = fmt.Sprint(target.Attributes[si.col.ancestors[0].name])
				}
			}
		}
		var body any = map[string]any{}
		if r := si.post.op.Success(); r != nil && r.Schema != nil {
			body = fake.Value(r.Schema)
		}
		setPath(body, strings.TrimPrefix(si.idPath, "result."), rawID)
		if si.resultParent != "" && container != "" {
			setPath(body, si.resultParent, container)
		}
		out, _ := json.Marshal(body)
		var c adaptercheck.ExtractCase
		if len(si.tools) == 0 {
			u := exampleURL(si.post.path)
			if si.capture >= 0 && container != "" {
				u = exampleURLWith(si.post.path, si.capture, container)
			}
			cmd := fmt.Sprintf(`curl -sS -X POST "https://%s%s" %s -H "Content-Type: application/json" -d '{"name":"example"}'`, g.host, u, g.curlAuth())
			c = adaptercheck.ExtractCase{Name: "curl " + opComment(si.post) + " creates a " + si.kind, Tool: "Bash", Input: map[string]any{"command": cmd}, Output: string(out)}
		} else {
			input := map[string]any{}
			if si.param != "" {
				input[si.param] = "example-" + strings.ReplaceAll(snake(si.param), "_", "-")
				if container != "" {
					input[si.param] = container
				}
			}
			c = adaptercheck.ExtractCase{Name: "MCP " + si.tools[0] + " creates a " + si.kind, Tool: "mcp__" + g.opt.MCPServer + "__" + si.tools[0], Input: input, Output: string(out)}
		}
		c.Expect = extract(c)
		if len(c.Expect) == 0 {
			return nil, fmt.Errorf("signature %s does not fire on its own create call (a generator bug; please report it)", si.id)
		}
		for _, id := range reg.Extract(adaptercheck.Action(c)) {
			if id.Provider != g.slug || id.Role != contract.IdentifierRoleCreated {
				continue
			}
			for _, r := range resources {
				if reg.Matches(id, r) && !contains(c.Matches, r.ID) {
					c.Matches = append(c.Matches, r.ID)
				}
			}
		}
		sort.Strings(c.Matches)
		cases = append(cases, c)
		if len(si.errKeys) > 0 {
			eb := map[string]any{}
			for _, k := range si.errKeys {
				eb[k] = map[string]any{"message": "example failure"}
			}
			eout, _ := json.Marshal(eb)
			failed := c
			failed.Matches = nil
			failed.Name = strings.Replace(c.Name, " creates a ", " that failed creates no ", 1)
			failed.Output = string(eout)
			failed.Expect = extract(failed)
			if len(failed.Expect) != 0 {
				return nil, fmt.Errorf("signature %s fires on an error response (a generator bug; please report it)", si.id)
			}
			cases = append(cases, failed)
		}
	}
	// A read-only call creates nothing.
	for _, c := range g.cols {
		if !c.sel || c.hard != "" {
			continue
		}
		out, _ := json.Marshal(fake.Value(c.resp))
		cmd := fmt.Sprintf(`curl -sS "https://%s%s" %s`, g.host, exampleURL(c.path), g.curlAuth())
		ro := adaptercheck.ExtractCase{Name: "curl " + opComment(c.opInfo) + " only reads", Tool: "Bash", Input: map[string]any{"command": cmd}, Output: string(out)}
		ro.Expect = extract(ro)
		if len(ro.Expect) != 0 {
			return nil, fmt.Errorf("a read-only GET %s yields identifiers (a generator bug; please report it)", c.path)
		}
		cases = append(cases, ro)
		break
	}
	return cases, nil
}

// setPath sets a dotted path in a JSON object, creating objects on the way.
func setPath(v any, path, val string) {
	m, ok := v.(map[string]any)
	if !ok || path == "" {
		return
	}
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = val
}
