package adaptergen

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
	"github.com/tolgayayci/muretto/internal/openapi"
)

func edge(t *testing.T, name string, opt Options) *Result {
	t.Helper()
	return generate(t, "edge/"+name, opt)
}

func edgeErr(t *testing.T, name string, opt Options) error {
	t.Helper()
	res, err := Generate(loadSpec(t, "edge/"+name), opt)
	if err == nil {
		t.Logf("%s", res.YAML)
	}
	return err
}

func edgeScan(t *testing.T, name string, res *Result) scanOut {
	t.Helper()
	return scan(t, loadSpec(t, "edge/"+name), res, "")
}

// adapter decodes the generated YAML into plain values.
func adapter(t *testing.T, res *Result) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(res.YAML, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func digS(v any, keys ...string) string {
	s, _ := dig(v, keys...).(string)
	return s
}

func kindDef(t *testing.T, a map[string]any, kind string) map[string]any {
	t.Helper()
	ks, _ := a["kinds"].([]any)
	for _, k := range ks {
		if m, _ := k.(map[string]any); m["kind"] == kind {
			return m
		}
	}
	t.Fatalf("no kind %q", kind)
	return nil
}

// source returns a kind's first source.
func source(t *testing.T, a map[string]any, kind string) map[string]any {
	t.Helper()
	srcs, _ := kindDef(t, a, kind)["sources"].([]any)
	if len(srcs) == 0 {
		t.Fatalf("kind %s has no sources", kind)
	}
	return srcs[0].(map[string]any)
}

// attrs maps attribute name to field (or template).
func attrs(t *testing.T, a map[string]any, kind string) map[string]string {
	t.Helper()
	out := map[string]string{}
	list, _ := source(t, a, kind)["attributes"].([]any)
	for _, at := range list {
		switch v := at.(type) {
		case string:
			out[v] = v
		case map[string]any:
			n, _ := v["name"].(string)
			if f, ok := v["field"].(string); ok {
				out[n] = f
			} else {
				out[n], _ = v["template"].(string)
			}
		}
	}
	return out
}

func strips(a map[string]any, ep string) map[string]bool {
	out := map[string]bool{}
	list, _ := dig(a, "endpoints", ep, "strip").([]any)
	for _, s := range list {
		out[fmt.Sprint(s)] = true
	}
	return out
}

func hasReview(r *Report, sub string) bool {
	for _, l := range r.Review {
		if strings.Contains(strings.ToLower(l), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func skipped(r *Report, path string) string {
	for _, s := range r.Skipped {
		if s.Path == path {
			return s.Reason
		}
	}
	return ""
}

func kindInfo(r *Report, kind string) *KindInfo {
	for i := range r.Kinds {
		if r.Kinds[i].Kind == kind {
			return &r.Kinds[i]
		}
	}
	return nil
}

func coverage(a map[string]any, kind string) string {
	list, _ := a["coverage"].([]any)
	for _, c := range list {
		if m, _ := c.(map[string]any); m["kind"] == kind {
			return fmt.Sprint(m["status"])
		}
	}
	return ""
}

// ---- base URL ----

func TestEdgeBaseURL(t *testing.T) {
	for _, name := range []string{"base-http.yaml", "base-none.yaml"} {
		if err := edgeErr(t, name, Options{}); err == nil || !strings.Contains(err.Error(), "--base-url") {
			t.Errorf("%s: err = %v, want a hint to pass --base-url", name, err)
		}
	}
	if err := edgeErr(t, "base-port.yaml", Options{}); err == nil {
		t.Error("base-port.yaml: a server on port 8443 was accepted")
	}
	res := edge(t, "base-none.yaml", Options{BaseURL: "https://api.edge.dev/v1/"})
	if res.Report.BaseURL != "https://api.edge.dev/v1" || digS(adapter(t, res), "endpoints", "thing", "path") != "/v1/things" {
		t.Errorf("--base-url with a path: %s\n%s", res.Report.BaseURL, res.YAML)
	}
	for _, bad := range []string{"http://api.edge.dev", "https://api.edge.dev:8443", "api.edge.dev", "/v1"} {
		if err := edgeErr(t, "base-none.yaml", Options{BaseURL: bad}); err == nil {
			t.Errorf("--base-url %s accepted", bad)
		}
	}
	res = edge(t, "base-vars.yaml", Options{})
	if res.Report.BaseURL != "https://eu.edge.dev/v2" {
		t.Errorf("server variables: %s", res.Report.BaseURL)
	}
}

func TestEdgeRelativeServer(t *testing.T) {
	if err := edgeErr(t, "base-relative.yaml", Options{}); err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Errorf("relative server without --base-url: %v", err)
	}
	res := edge(t, "base-relative.yaml", Options{BaseURL: "https://api.edge.dev"})
	a := adapter(t, res)
	if res.Report.BaseURL != "https://api.edge.dev/api/v1" || digS(a, "endpoints", "thing", "path") != "/api/v1/things" {
		t.Fatalf("relative server path not folded in: %s\n%s", res.Report.BaseURL, res.YAML)
	}
	out := edgeScan(t, "base-relative.yaml", res)
	if kindsOf(out.res)["thing"] != 2 {
		t.Errorf("scan: %v %v", kindsOf(out.res), out.calls)
	}
}

// https://host:443 is the default https port written out.
func TestEdgeExplicitPort443(t *testing.T) {
	res := edge(t, "base-port443.yaml", Options{})
	if res.Report.BaseURL != "https://api.edge.dev/v1" {
		t.Errorf("base = %s", res.Report.BaseURL)
	}
}

// ---- auth ----

func authOf(t *testing.T, res *Result) (string, string) {
	a := adapter(t, res)
	return digS(a, "api", "auth", "header"), digS(a, "api", "auth", "scheme")
}

func TestEdgeAuth(t *testing.T) {
	cases := []struct {
		spec, header, scheme, review string
	}{
		{"things.yaml", "Authorization", "Bearer", ""},
		{"auth-none.yaml", "Authorization", "Bearer", "no security"},
		{"auth-oauth2.yaml", "Authorization", "Bearer", "oauth"},
		{"auth-basic.yaml", "Authorization", "Basic", "basic"},
		{"auth-header.yaml", "X-API-Key", "", ""},
		{"auth-authz-token.yaml", "Authorization", "Token", "prefix"},
		{"auth-authz-nodesc.yaml", "Authorization", "Bearer", "prefix"},
		{"auth-query-or-bearer.yaml", "Authorization", "Bearer", ""},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			res := edge(t, c.spec, Options{})
			h, s := authOf(t, res)
			if h != c.header || s != c.scheme {
				t.Errorf("auth = %s %q, want %s %q", h, s, c.header, c.scheme)
			}
			if c.review != "" && !hasReview(res.Report, c.review) {
				t.Errorf("no review line about %q: %v", c.review, res.Report.Review)
			}
		})
	}
	res := edge(t, "auth-oauth2.yaml", Options{})
	if help := digS(adapter(t, res), "token_help"); !strings.Contains(help, "things:read") || strings.Contains(help, "things:write") {
		t.Errorf("token_help should list read scopes only: %q", help)
	}
	for spec, word := range map[string]string{"auth-query.yaml": "query", "auth-cookie.yaml": "cookie"} {
		if err := edgeErr(t, spec, Options{}); err == nil || !strings.Contains(err.Error(), word) {
			t.Errorf("%s: err = %v, want a clear error about %s auth", spec, err, word)
		}
	}
}

// The only scheme the spec defines is an API key in the query string. No
// operation lists it in `security`, but assuming a bearer header instead is
// guessing against what the spec says.
func TestEdgeAuthQueryUndeclared(t *testing.T) {
	if err := edgeErr(t, "auth-query-undeclared.yaml", Options{}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Errorf("err = %v", err)
	}
}

// "Token generated in Settings. Send it as Bearer <token>." names Bearer as
// the prefix; the first capitalised "Token" is prose.
func TestEdgeAuthAuthorizationProse(t *testing.T) {
	res := edge(t, "auth-authz-prose.yaml", Options{})
	if _, s := authOf(t, res); s != "Bearer" {
		t.Errorf("scheme = %q, want Bearer", s)
	}
}

// ---- accounts ----

func TestEdgeAccountsOrgList(t *testing.T) {
	res := edge(t, "acct-orgs.yaml", Options{})
	a := adapter(t, res)
	if digS(a, "accounts", "endpoint") != "accounts" || digS(a, "endpoints", "accounts", "path") != "/v1/orgs" || digS(a, "accounts", "id") != "{id}" {
		t.Fatalf("accounts:\n%s", res.YAML)
	}
	if digS(a, "accounts", "slug") != "{slug}" {
		t.Errorf("slug not mapped")
	}
	if digS(a, "endpoints", "project", "path") != "/v1/orgs/{org_id}/projects" || digS(a, "endpoints", "project", "params", "org_id") != "{account}" {
		t.Errorf("project endpoint:\n%s", res.YAML)
	}
	out := edgeScan(t, "acct-orgs.yaml", res)
	if kindsOf(out.res)["project"] != 2 {
		t.Errorf("scan: %v %v", kindsOf(out.res), out.calls)
	}
}

func TestEdgeAccountsMe(t *testing.T) {
	res := edge(t, "acct-me.yaml", Options{})
	a := adapter(t, res)
	if digS(a, "endpoints", "accounts", "path") != "/v1/me" || digS(a, "accounts", "items") != "$" || digS(a, "accounts", "id") != "{id}" {
		t.Fatalf("accounts:\n%s", res.YAML)
	}
	if !strips(a, "accounts")["api_token"] {
		t.Errorf("api_token on /me not stripped:\n%s", res.YAML)
	}
	if strings.Contains(string(res.YAML), "{email}") {
		t.Errorf("email used as an account field")
	}
	out := edgeScan(t, "acct-me.yaml", res)
	if kindsOf(out.res)["thing"] != 2 {
		t.Errorf("scan: %v", kindsOf(out.res))
	}
}

func TestEdgeAccountsMeWrapped(t *testing.T) {
	res := edge(t, "acct-me-wrapped.yaml", Options{})
	a := adapter(t, res)
	if digS(a, "endpoints", "accounts", "path") != "/v2/account" || digS(a, "accounts", "items") != "account" || digS(a, "accounts", "id") != "{uuid}" {
		t.Fatalf("accounts:\n%s", res.YAML)
	}
	out := edgeScan(t, "acct-me-wrapped.yaml", res)
	if kindsOf(out.res)["droplet"] != 2 {
		t.Errorf("scan: %v %v", kindsOf(out.res), out.calls)
	}
}

func TestEdgeAccountsAsk(t *testing.T) {
	res := edge(t, "acct-ask.yaml", Options{})
	a := adapter(t, res)
	if ask := digS(a, "accounts", "ask"); !strings.Contains(ask, "org_slug") {
		t.Fatalf("accounts.ask = %q\n%s", ask, res.YAML)
	}
	if digS(a, "endpoints", "app", "query", "org_slug") != "{account}" {
		t.Errorf("app query:\n%s", res.YAML)
	}
	out := edgeScan(t, "acct-ask.yaml", res)
	if kindsOf(out.res)["app"] != 2 {
		t.Errorf("scan: %v", kindsOf(out.res))
	}
	for _, c := range out.calls {
		if strings.HasPrefix(c, "GET /v1/apps") && !strings.Contains(c, "org_slug=typed-org") {
			t.Errorf("app list without the typed org: %s", c)
		}
	}
}

func TestEdgeAccountsImplicit(t *testing.T) {
	for _, opt := range []Options{{}, {Accounts: "none"}} {
		res := edge(t, "things.yaml", opt)
		if digS(adapter(t, res), "accounts", "id") != "default" || !hasReview(res.Report, "one account") {
			t.Errorf("%+v: implicit account:\n%s", opt, res.YAML)
		}
		edgeScan(t, "things.yaml", res)
	}
	if err := edgeErr(t, "things.yaml", Options{Accounts: "/v1/nope"}); err == nil {
		t.Error("--accounts with an unknown path accepted")
	}
	res := edge(t, "acct-orgs.yaml", Options{Accounts: "none"})
	if digS(adapter(t, res), "accounts", "id") != "default" {
		t.Errorf("--accounts none ignored")
	}
}

// A required team_id is a team, not the org the scan runs for: sending the
// org id as team_id lists nothing (or the wrong team).
func TestEdgeAccountParamOfAnotherKind(t *testing.T) {
	res := edge(t, "acct-team-param.yaml", Options{})
	a := adapter(t, res)
	if v := digS(a, "endpoints", "member", "query", "team_id"); v == "{account}" {
		t.Errorf("members?team_id is filled with the org id:\n%s", res.YAML)
	}
}

// ---- nesting ----

func TestEdgeThreeLevels(t *testing.T) {
	res := edge(t, "nest3.yaml", Options{})
	if got := strings.Join(selectedKinds(res.Report), ","); got != "deployment,project,service" {
		t.Fatalf("kinds = %s", got)
	}
	a := adapter(t, res)
	if p := dig(a, "endpoints", "deployment", "params"); digS(p, "project_id") != "{parent.project_id}" || digS(p, "service_id") != "{parent.id}" {
		t.Errorf("deployment params = %v", p)
	}
	if digS(kindDef(t, a, "deployment"), "for_each") != "service" || attrs(t, a, "service")["project_id"] != "{parent.id}" {
		t.Errorf("service must carry project_id for its children:\n%s", res.YAML)
	}
	if da := attrs(t, a, "deployment"); da["project_id"] != "{parent.project_id}" || da["service_id"] != "{parent.id}" {
		t.Errorf("deployment ancestors = %v", da)
	}
	out := edgeScan(t, "nest3.yaml", res)
	if k := kindsOf(out.res); k["project"] != 2 || k["service"] != 4 || k["deployment"] != 8 {
		t.Errorf("scan: %v\n%v", k, out.calls)
	}
	// Every deployment request names a scanned project and service.
	for _, c := range out.calls {
		if strings.Contains(c, "/deployments") && (strings.Contains(c, "{") || strings.Contains(c, "<")) {
			t.Errorf("unfilled template in %s", c)
		}
	}
}

func TestEdgeMaxDepthAndKinds(t *testing.T) {
	res := edge(t, "nest3.yaml", Options{MaxDepth: 1})
	if got := strings.Join(selectedKinds(res.Report), ","); got != "project,service" {
		t.Errorf("MaxDepth 1: kinds = %s", got)
	}
	if ki := kindInfo(res.Report, "deployment"); ki == nil || !strings.Contains(ki.Note, "max-depth") {
		t.Errorf("deployment note: %+v", ki)
	}
	if coverage(adapter(t, res), "deployment") != "planned" {
		t.Errorf("deployment should be planned in coverage")
	}
	res = edge(t, "nest3.yaml", Options{MaxDepth: 1, Kinds: []string{"deployment"}})
	if got := strings.Join(selectedKinds(res.Report), ","); got != "deployment,project,service" {
		t.Errorf("Kinds pulls parents: %s", got)
	}
	edgeScan(t, "nest3.yaml", res)
}

func TestEdgeMCPServer(t *testing.T) {
	res := edge(t, "nest3.yaml", Options{MCPServer: "edge"})
	y := string(res.YAML)
	for _, want := range []string{"mcp_servers: [edge]", "mcp_tools: [createDeployment, create_deployment]", "container: '{input.service_id}'"} {
		if !strings.Contains(y, want) {
			t.Errorf("missing %q:\n%s", want, y)
		}
	}
	reg, err := adapters.LoadFile("edge.yaml", res.YAML)
	if err != nil {
		t.Fatal(err)
	}
	act := observe.RawAction{
		Action: contract.AgentAction{ID: "toolu_X", SessionID: "00000000-0000-4000-8000-00000000000b", ToolName: "mcp__edge__createDeployment", Status: contract.AgentActionStatusOk, Source: contract.AgentActionSourceHook},
		Input:  map[string]any{"project_id": "p1", "service_id": "s1"},
		Output: `{"id":"d1","state":"READY"}`,
	}
	ids := reg.Extract(act)
	if len(ids) != 1 || ids[0].Value != "d1" || ids[0].Container == nil || *ids[0].Container != "s1" {
		t.Errorf("MCP identifiers = %+v", ids)
	}
}

func TestEdgePathConstant(t *testing.T) {
	res := edge(t, "path-const.yaml", Options{})
	a := adapter(t, res)
	if digS(a, "endpoints", "thing", "path") != "/2024-01/things" || dig(a, "endpoints", "thing", "params") != nil {
		t.Fatalf("constant path param not folded:\n%s", res.YAML)
	}
	out := edgeScan(t, "path-const.yaml", res)
	if kindsOf(out.res)["thing"] != 2 {
		t.Errorf("scan: %v %v", kindsOf(out.res), out.calls)
	}
}

// A path or required query parameter with several enum values and no
// default is not a constant: picking the first value lists only part of
// the items while coverage says "full".
func TestEdgeEnumIsNotAConstant(t *testing.T) {
	// {scope} in [public, private] and a required ?kind in [small, large]
	// are choices, not constants: the endpoints are skipped with the reason,
	// never fixed to the first value.
	res, err := Generate(loadSpec(t, "edge/path-enum-multi.yaml"), Options{})
	if err == nil {
		t.Fatalf("generated an adapter from choice-only endpoints:\n%s", res.YAML)
	}
	if res == nil || res.Report == nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range res.Report.Skipped {
		if strings.Contains(s.Reason, "several allowed values") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("skipped = %+v, want both endpoints skipped for several allowed values", res.Report.Skipped)
	}
}

func TestEdgeOddPaths(t *testing.T) {
	res := edge(t, "paths-odd.yaml", Options{})
	if got := strings.Join(selectedKinds(res.Report), ","); got != "thing" {
		t.Errorf("kinds = %s", got)
	}
	for path, word := range map[string]string{
		"/v1/secrets":                      "secret",
		"/v1/secrets/{secret_id}/versions": "parent",
		"/v1/things/{thing_id}:children":   "mixes",
		"/v1/gadgets":                      "header",
	} {
		if r := skipped(res.Report, path); !strings.Contains(r, word) {
			t.Errorf("%s: skipped reason %q, want it to mention %q (skipped: %+v)", path, r, word, res.Report.Skipped)
		}
	}
}

func TestEdgeTrailingSlash(t *testing.T) {
	res := edge(t, "trailing-slash.yaml", Options{})
	a := adapter(t, res)
	if p := digS(a, "endpoints", "thing", "path"); p != "/v1/things/" {
		t.Errorf("thing path = %q, want it exactly as written", p)
	}
	if p := digS(a, "endpoints", "part", "path"); p != "/v1/things/{thing_id}/parts/" {
		t.Errorf("part path = %q", p)
	}
	out := edgeScan(t, "trailing-slash.yaml", res)
	if k := kindsOf(out.res); k["thing"] != 2 || k["part"] != 4 {
		t.Errorf("scan: %v %v", k, out.calls)
	}
	reg, err := adapters.LoadFile("edge.yaml", res.YAML)
	if err != nil {
		t.Fatal(err)
	}
	act := observe.RawAction{
		Action: contract.AgentAction{ID: "toolu_Y", SessionID: "00000000-0000-4000-8000-00000000000c", ToolName: "Bash", Status: contract.AgentActionStatusOk, Source: contract.AgentActionSourceHook},
		Input:  map[string]any{"command": `curl -X POST https://api.edge.dev/v1/things/ -H "Authorization: Bearer $EDGE_CLOUD_TOKEN" -d '{"name":"a"}'`},
		Output: `{"id":"t1","name":"a"}`,
	}
	if ids := reg.Extract(act); len(ids) != 1 || ids[0].Value != "t1" {
		t.Errorf("curl to the trailing-slash path: %+v", ids)
	}
}

func TestEdgeVersionedDuplicates(t *testing.T) {
	res := edge(t, "versions.yaml", Options{})
	a := adapter(t, res)
	if digS(a, "endpoints", "thing", "path") != "/v2/things" {
		t.Errorf("the newest version should win:\n%s", res.YAML)
	}
	if r := skipped(res.Report, "/v1/things"); !strings.Contains(r, "older version") {
		t.Errorf("/v1/things: %q", r)
	}
	edgeScan(t, "versions.yaml", res)
}

func TestEdgeNameCollisionsAndReserved(t *testing.T) {
	res := edge(t, "collisions.yaml", Options{})
	kinds := selectedKinds(res.Report)
	var members []string
	for _, k := range kinds {
		if strings.Contains(k, "member") {
			members = append(members, k)
		}
	}
	if len(members) != 2 || members[0] == members[1] {
		t.Errorf("member kinds = %v (all %v)", members, kinds)
	}
	a := adapter(t, res)
	pa := attrs(t, a, "provider")
	for name := range pa {
		if reservedAttrs[name] {
			t.Errorf("provider attribute %q is reserved", name)
		}
	}
	if pa["public"] == "" {
		t.Errorf("provider lost its public attribute: %v", pa)
	}
	if ka := attrs(t, a, "keyset"); ka["keyset_name"] == "" {
		t.Errorf("keyset id attribute: %v", ka)
	}
	out := edgeScan(t, "collisions.yaml", res)
	k := kindsOf(out.res)
	for _, m := range members {
		if k[m] != 4 {
			t.Errorf("%s: %d resources, want 4 (%v)", m, k[m], k)
		}
	}
	if k["keyset"] != 4 {
		t.Errorf("keyset: %v", k)
	}
}

func TestEdgeTemplateUnsafeFields(t *testing.T) {
	res := edge(t, "unsafe-fields.yaml", Options{})
	a := adapter(t, res)
	if digS(source(t, a, "thing"), "id") != "{@id}" {
		t.Errorf("id = %v", source(t, a, "thing")["id"])
	}
	at := attrs(t, a, "thing")
	for name, field := range at {
		if strings.Contains(field, " ") {
			t.Errorf("attribute %s reads %q", name, field)
		}
	}
	if at["is_public"] != "is-public" {
		t.Errorf("is-public: %v", at)
	}
	out := edgeScan(t, "unsafe-fields.yaml", res)
	if kindsOf(out.res)["thing"] != 2 {
		t.Errorf("scan: %v", kindsOf(out.res))
	}
	if !strings.Contains(string(res.YAML), "field: is_public") {
		t.Errorf("no rule on is-public:\n%s", res.YAML)
	}
}

// "foo.bar" is one key; the engine reads a dotted field as a path (foo ->
// bar), so the attribute is never filled.
func TestEdgeDottedFieldName(t *testing.T) {
	res := edge(t, "unsafe-fields.yaml", Options{})
	if f, ok := attrs(t, adapter(t, res), "thing")["foo_bar"]; ok {
		t.Errorf("attribute foo_bar reads field %q, which the engine resolves as a nested path", f)
	}
}

// A rule must not refer to an attribute the kind does not emit.
func TestEdgeRuleOnUnsafeField(t *testing.T) {
	res := edge(t, "unsafe-rule.yaml", Options{})
	a := adapter(t, res)
	at := attrs(t, a, "thing")
	rules, _ := a["rules"].([]any)
	for _, r := range rules {
		f := digS(r, "exposed", "field")
		if _, ok := at[f]; !ok {
			t.Errorf("rule %v uses %s, which is not an attribute (%v)", digS(r, "id"), f, at)
		}
	}
}

func TestEdgeEnvelopes(t *testing.T) {
	res := edge(t, "envelopes.yaml", Options{})
	a := adapter(t, res)
	svc := source(t, a, "service")
	if svc["items"] != "$" || svc["id"] != "{service.id}" || svc["name"] != "{service.name}" {
		t.Errorf("service source = %v", svc)
	}
	zone := source(t, a, "zone")
	if zone["items"] != "result" {
		t.Errorf("zone items = %v (errors/messages are not the list)", zone["items"])
	}
	if digS(a, "endpoints", "zone", "query", "per_page") != "50" {
		t.Errorf("zone query: %v", dig(a, "endpoints", "zone"))
	}
	if coverage(a, "zone") != "partial" {
		t.Errorf("zone pages by number: coverage %s", coverage(a, "zone"))
	}
	out := edgeScan(t, "envelopes.yaml", res)
	if k := kindsOf(out.res); k["service"] != 2 || k["zone"] != 2 {
		t.Errorf("scan: %v", k)
	}
}

// /services takes a cursor parameter, but the cursor lives in each item
// (Render). The generator cannot follow it, so it must not claim the list
// is one page.
func TestEdgeCursorParamWithoutNextField(t *testing.T) {
	res := edge(t, "envelopes.yaml", Options{})
	if c := coverage(adapter(t, res), "service"); c != "partial" {
		t.Errorf("service coverage = %s, want partial (paging: %s)", c, kindInfo(res.Report, "service").Paging)
	}
}

func TestEdgePaging(t *testing.T) {
	res := edge(t, "paging.yaml", Options{})
	a := adapter(t, res)
	cursor := map[string][2]string{
		"page":      {"meta.next_page", "page"},
		"speakeasy": {"meta.next", "after"},
		"fern":      {"pagination.next_token", "starting_token"},
	}
	for kind, want := range cursor {
		pg := dig(a, "endpoints", kind, "pagination")
		if digS(pg, "next") != want[0] || digS(pg, "param") != want[1] {
			t.Errorf("%s pagination = %v, want next %s param %s", kind, pg, want[0], want[1])
		}
		if coverage(a, kind) != "full" {
			t.Errorf("%s coverage %s", kind, coverage(a, kind))
		}
	}
	for _, kind := range []string{"link", "speakeasy_offset", "offset", "pageable"} {
		if dig(a, "endpoints", kind, "pagination") != nil {
			t.Errorf("%s: unsupported paging emitted as cursor", kind)
		}
		ki := kindInfo(res.Report, kind)
		if coverage(a, kind) != "partial" || ki == nil || !strings.HasPrefix(ki.Paging, "first page only") {
			t.Errorf("%s: coverage %s, paging %+v", kind, coverage(a, kind), ki)
		}
	}
	if source(t, a, "pageable")["items"] != "value" {
		t.Errorf("pageable items")
	}
	if digS(a, "endpoints", "link", "query", "per_page") != "100" {
		t.Errorf("link per_page: %v", dig(a, "endpoints", "link"))
	}
	edgeScan(t, "paging.yaml", res)
}

func TestEdgeSecrets(t *testing.T) {
	res := edge(t, "secrets.yaml", Options{})
	a := adapter(t, res)
	st := strips(a, "database")
	for _, f := range []string{"admin_secret", "pin", "shared_access", "client_secret", "clientSecret", "rest_token", "connection_string", "apiKey", "password_protection.password", "nested.deeper.private_key"} {
		if !st["data[]."+f] {
			t.Errorf("data[].%s not stripped (strip: %v)", f, st)
		}
	}
	for _, f := range []string{"has_password", "token_count", "password_protection", "access", "nested"} {
		if st["data[]."+f] {
			t.Errorf("data[].%s stripped but is not a secret", f)
		}
	}
	at := attrs(t, a, "database")
	if at["has_password"] == "" || at["token_count"] == "" {
		t.Errorf("non-secret facts dropped: %v", at)
	}
	for name, field := range at {
		switch field {
		case "admin_secret", "pin", "shared_access", "client_secret", "clientSecret", "rest_token", "connection_string", "apiKey", "password_protection.password":
			t.Errorf("attribute %s copies secret field %s", name, field)
		}
	}
	if !strings.Contains(string(res.YAML), "public_database") {
		t.Errorf("no public rule for access: public")
	}
	edgeScan(t, "secrets.yaml", res)
}

func TestEdgeSecretNamesNotCovered(t *testing.T) {
	res := edge(t, "secrets-more.yaml", Options{})
	a := adapter(t, res)
	at := attrs(t, a, "cluster")
	st := strips(a, "cluster")
	for _, f := range []string{"kubeconfig", "primary_key", "client_key"} {
		if _, ok := at[f]; ok {
			t.Errorf("%s is an attribute", f)
		}
		if !st["[]."+f] {
			t.Errorf("%s not stripped (strip: %v)", f, st)
		}
	}
}

func TestEdgeSecretEndpoints(t *testing.T) {
	res := edge(t, "secret-endpoints.yaml", Options{})
	if got := strings.Join(selectedKinds(res.Report), ","); got != "project" {
		t.Errorf("kinds = %s", got)
	}
	for _, p := range []string{"/v1/api-keys", "/v1/secrets", "/v1/widgets", "/v1/projects/{project_id}/credentials"} {
		if r := skipped(res.Report, p); !strings.Contains(r, "secret") {
			t.Errorf("%s: skipped reason %q", p, r)
		}
	}
	if _, err := Generate(loadSpec(t, "edge/secret-endpoints.yaml"), Options{Kinds: []string{"api_key"}}); err == nil {
		t.Error("a secret endpoint could be selected with Kinds")
	}
}

// ---- size and options ----

func manySpec(n int) []byte {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: { title: Big Cloud, version: \"1\" }\nservers: [{ url: https://api.big.dev }]\nsecurity: [{ bearer: [] }]\npaths:\n")
	item := `{ type: array, items: { type: object, required: [id], properties: { id: { type: string }, name: { type: string }, public: { type: boolean } } } }`
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  /r%dthings:\n    get:\n      responses: { \"200\": { description: ok, content: { application/json: { schema: %s } } } }\n    post: { responses: { \"201\": { description: ok } } }\n", i, item)
		fmt.Fprintf(&b, "  /r%dthings/{id}/subs:\n    parameters: [{ name: id, in: path, required: true, schema: { type: string } }]\n    get:\n      responses: { \"200\": { description: ok, content: { application/json: { schema: %s } } } }\n    post: { responses: { \"201\": { description: ok } } }\n", i, item)
	}
	b.WriteString("components:\n  securitySchemes: { bearer: { type: http, scheme: bearer } }\n")
	return []byte(b.String())
}

func TestEdgeManyKinds(t *testing.T) {
	doc, err := openapi.Parse(manySpec(150), openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Generate(doc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := selectedKinds(res.Report)
	if len(kinds) != 300 {
		t.Errorf("%d kinds, want 300", len(kinds))
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate kind %s", k)
		}
		seen[k] = true
	}
	res, err = Generate(doc, Options{Kinds: []string{"r7thing", "r42thing_sub"}})
	if err != nil {
		t.Fatalf("%v (candidates include %v)", err, kinds[:5])
	}
	got := selectedKinds(res.Report)
	sort.Strings(got)
	if strings.Join(got, ",") != "r42thing,r42thing_sub,r7thing" {
		t.Errorf("Kinds: %v", got)
	}
	var a map[string]any
	_ = yaml.Unmarshal(res.YAML, &a)
	if n := len(a["coverage"].([]any)); n > 3+40 {
		t.Errorf("coverage has %d rows; planned rows should be capped", n)
	}
}

func TestEdgeNoListEndpoints(t *testing.T) {
	doc, err := openapi.Parse([]byte(`openapi: 3.1.2
info: { title: Hooks, version: "1" }
servers: [{ url: https://api.hooks.dev }]
webhooks: { newPet: { post: { responses: { "200": { description: ok } } } } }
`), openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(doc, Options{}); err == nil || !strings.Contains(err.Error(), "no list endpoints") {
		t.Errorf("err = %v", err)
	}
}
