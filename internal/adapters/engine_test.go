package adapters

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/broker"
)

// engineAdapter is a minimal adapter around one paginated endpoint.
func engineAdapter(t *testing.T, endpoint, kind string) *Registry {
	t.Helper()
	y := `format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
token_help: test
endpoints:
  me: { path: /v1/me }
` + endpoint + `
accounts: { endpoint: me, items: $, id: "{id}" }
kinds:
` + kind + `
coverage: [{ kind: thing, status: full }]
`
	r, err := LoadFile("acme.yaml", []byte(y))
	if err != nil {
		t.Fatalf("%v\n%s", err, y)
	}
	return r
}

type call struct{ path, query string }

func scanOffline(t *testing.T, r *Registry, respond func(path string, q url.Values) (int, string, string)) (ScanResult, []call) {
	t.Helper()
	var calls []call
	var headers []string
	b := broker.NewOffline(r.Allowlist(), func(method, path, rawQuery string) (int, []byte) {
		calls = append(calls, call{path, rawQuery})
		q, _ := url.ParseQuery(rawQuery)
		st, body, _ := respond(path, q)
		return st, []byte(body)
	})
	_ = headers
	res, err := r.Scan(context.Background(), b, "acme", Credentials{Token: "t", AccountID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	return res, calls
}

const thingKind = `  - kind: thing
    sources:
      - { endpoint: things, items: data, id: "{id}" }`

func failed(res ScanResult) string {
	for _, c := range res.Checks {
		if c.Error != nil {
			return *c.Error
		}
	}
	return ""
}

func TestPaginationPage(t *testing.T) {
	r := engineAdapter(t, `  things:
    path: /v1/things
    query: { per_page: "2" }
    pagination: { style: page, param: page, total: result_info.total_pages }`, thingKind)
	res, calls := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		p := q.Get("page")
		if p == "" {
			p = "1"
		}
		return 200, fmt.Sprintf(`{"data":[{"id":"t%s"}],"result_info":{"page":%s,"total_pages":3}}`, p, p), ""
	})
	if len(res.Resources) != 3 || len(calls) != 3 || calls[2].query != "page=3&per_page=2" {
		t.Errorf("resources %d, calls %v; %s", len(res.Resources), calls, failed(res))
	}
}

func TestPaginationLastItem(t *testing.T) {
	// Stripe: starting_after = last id while has_more.
	r := engineAdapter(t, `  things:
    path: /v1/things
    query: { limit: "2" }
    pagination: { style: last_item, param: starting_after, items: data, field: id, more: has_more }`, thingKind)
	res, calls := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		switch q.Get("starting_after") {
		case "":
			return 200, `{"data":[{"id":"a"},{"id":"b"}],"has_more":true}`, ""
		case "b":
			return 200, `{"data":[{"id":"c"}],"has_more":false}`, ""
		}
		return 500, `{}`, ""
	})
	if len(res.Resources) != 3 || len(calls) != 2 {
		t.Errorf("resources %d, calls %v; %s", len(res.Resources), calls, failed(res))
	}
	// Render: a cursor on each item, until a page is empty.
	r = engineAdapter(t, `  things:
    path: /v1/things
    pagination: { style: last_item, param: cursor, field: cursor }`, `  - kind: thing
    sources:
      - { endpoint: things, items: $, id: "{service.id}" }`)
	res, calls = scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		switch q.Get("cursor") {
		case "":
			return 200, `[{"cursor":"c1","service":{"id":"s1"}},{"cursor":"c2","service":{"id":"s2"}}]`, ""
		case "c2":
			return 200, `[]`, ""
		}
		return 500, `{}`, ""
	})
	if len(res.Resources) != 2 || len(calls) != 2 {
		t.Errorf("render: resources %d, calls %v; %s", len(res.Resources), calls, failed(res))
	}
}

func TestPaginationLinkAndLimits(t *testing.T) {
	r := engineAdapter(t, `  things:
    path: /v1/things
    query: { per_page: "100" }
    pagination: { style: link, param: page, max_pages: 3 }`, `  - kind: thing
    sources:
      - { endpoint: things, items: $, id: "{id}" }`)
	link := func(page int) string {
		return `<https://api.acme.dev/v1/things?per_page=100&page=` + strconv.Itoa(page) + `>; rel="next", <https://api.acme.dev/v1/things?page=9>; rel="last"`
	}
	// The offline responder cannot set headers, so drive linkNext and
	// nextPage directly for the header part.
	if got := linkNext([]string{link(2)}); got != "https://api.acme.dev/v1/things?per_page=100&page=2" {
		t.Errorf("linkNext = %q", got)
	}
	if got := linkNext([]string{`<https://x/a>; rel="prev last"`}); got != "" {
		t.Errorf("non-next rel followed: %q", got)
	}
	a := r.byName["acme"]
	ep := a.Endpoints["things"]
	s := &scanner{a: a}
	hdr := map[string][]string{"Link": {link(2)}}
	nq, _, more, err := s.nextPage(ep, nil, hdr, url.Values{"per_page": {"100"}}, 0, "https://api.acme.dev/v1/things?per_page=100")
	if err != nil || !more || nq.Get("page") != "2" {
		t.Errorf("next = %v %v %v", nq, more, err)
	}
	// GitHub links per-repo lists to /repositories/{id}/…: only the page
	// value is taken, and it is sent on the original path.
	nq, _, more, err = s.nextPage(ep, nil, map[string][]string{"Link": {`<https://api.acme.dev/repositories/42/things?page=3>; rel="next"`}}, url.Values{"per_page": {"100"}}, 1, "https://api.acme.dev/v1/things?per_page=100")
	if err != nil || !more || nq.Get("page") != "3" || nq.Get("per_page") != "100" || len(nq) != 2 {
		t.Errorf("other-path link: %v %v %v", nq, more, err)
	}
	for _, bad := range []string{
		`<https://evil.example.com/v1/things?page=2>; rel="next"`,
		`<http://api.acme.dev/v1/things?page=2>; rel="next"`,
		`<https://api.acme.dev/v1/things?cursor=2>; rel="next"`, // no page value
	} {
		if _, _, _, err := s.nextPage(ep, nil, map[string][]string{"Link": {bad}}, url.Values{}, 0, "https://api.acme.dev/v1/things"); err == nil {
			t.Errorf("followed %s", bad)
		}
	}
	// A cursor that repeats, and too many pages, are errors, never silent truncation.
	r = engineAdapter(t, `  things:
    path: /v1/things
    pagination: { next: next, param: cursor, max_pages: 3 }`, thingKind)
	res, _ := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		return 200, `{"data":[{"id":"x` + q.Get("cursor") + `"}],"next":"same"}`, ""
	})
	if !strings.Contains(failed(res), "repeated") {
		t.Errorf("repeated cursor: %q", failed(res))
	}
	n := 0
	res, _ = scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		n++
		return 200, fmt.Sprintf(`{"data":[{"id":"x%d"}],"next":"c%d"}`, n, n), ""
	})
	if !strings.Contains(failed(res), "more than 3 pages") {
		t.Errorf("max pages: %q", failed(res))
	}
}

func TestEndpointHeadersAndValidation(t *testing.T) {
	r := engineAdapter(t, `  things:
    path: /v1/things
    headers: { X-Acme-Api-Version: "2026-10-01" }`, thingKind)
	a := r.byName["acme"]
	if a.Endpoints["things"].Headers["X-Acme-Api-Version"] != "2026-10-01" {
		t.Fatal("header not loaded")
	}
	for _, h := range []string{"Authorization", "Cookie", "X-HTTP-Method-Override", "Proxy-Authorization", "Host", "X-Api-Key"} {
		y := strings.Replace(`format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
endpoints:
  me: { path: /v1/me, headers: { HEADER: "x" } }
accounts: { endpoint: me, items: $, id: "{id}" }
kinds: []
`, "HEADER", h, 1)
		if _, err := LoadFile("x.yaml", []byte(y)); err == nil {
			t.Errorf("header %s accepted", h)
		}
	}
	for _, v := range []string{"{account}", "a\r\nInjected: 1", ""} {
		y := `format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
endpoints:
  me: { path: /v1/me, headers: { X-V: ` + strconv.Quote(v) + ` } }
accounts: { endpoint: me, items: $, id: "{id}" }
kinds: []
`
		if _, err := LoadFile("x.yaml", []byte(y)); err == nil {
			t.Errorf("header value %q accepted", v)
		}
	}
	for _, pg := range []string{"{ style: page, param: page }", "{ style: last_item, param: after }", "{ style: nope, param: x }", "{ style: link }"} {
		y := `format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
endpoints:
  me: { path: /v1/me, pagination: ` + pg + ` }
accounts: { endpoint: me, items: $, id: "{id}" }
kinds: []
`
		if _, err := LoadFile("x.yaml", []byte(y)); err == nil {
			t.Errorf("pagination %s accepted", pg)
		}
	}
}

// A 404 on an enrichment means "not there": the attributes are null, so a
// rule can say not_exposed instead of unknown.
func TestEnrichAbsentIsNull(t *testing.T) {
	r := engineAdapter(t, `  things: { path: /v1/things }
  data_api:
    path: /v1/things/{id}/data-api
    params: { id: "{id}" }
    on_status: { 404: empty }`, `  - kind: thing
    sources:
      - { endpoint: things, items: data, id: "{id}" }
    enrich:
      - { endpoint: data_api, attributes: [{ name: data_api_url, field: url }] }`)
	res, _ := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		switch path {
		case "/v1/things":
			return 200, `{"data":[{"id":"on"},{"id":"off"}]}`, ""
		case "/v1/things/on/data-api":
			return 200, `{"url":"https://on.example.com"}`, ""
		}
		return 404, `{"message":"not found"}`, ""
	})
	got := map[string]any{}
	for _, res := range res.Resources {
		v, ok := res.Attributes["data_api_url"]
		if !ok {
			v = "<absent>"
		}
		got[res.ProviderID] = v
	}
	if got["on"] != "https://on.example.com" || got["off"] != nil {
		t.Errorf("attributes = %v", got)
	}
}

func TestAgePredicate(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	p := func(y string) *predicate {
		var pr predicate
		if err := decodeYAML(y, &pr); err != nil {
			t.Fatal(err)
		}
		return &pr
	}
	ctx := map[string]any{nowKey: now, "old": "2026-09-01T00:00:00Z", "new": "2026-10-31T00:00:00Z", "ms": "1790000000000", "bad": "soon", "none": nil}
	for _, c := range []struct {
		y    string
		want tri
	}{
		{`{ field: old, older_than: 30d }`, triTrue},
		{`{ field: new, older_than: 30d }`, triFalse},
		{`{ field: new, newer_than: 2d }`, triTrue},
		{`{ field: ms, older_than: 1w }`, triTrue}, // 2026-09-21 in epoch ms
		{`{ field: bad, older_than: 1d }`, triUnknown},
		{`{ field: missing, older_than: 1d }`, triUnknown},
		{`{ field: none, older_than: 1d }`, triFalse},
	} {
		if got := p(c.y).eval(ctx); got != c.want {
			t.Errorf("%s = %v, want %v", c.y, got, c.want)
		}
	}
	var pr predicate
	for _, bad := range []string{`{ field: x, older_than: 30 }`, `{ field: x, older_than: 1y }`, `{ field: x, older_than: -1d }`} {
		if err := decodeYAML(bad, &pr); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func decodeYAML(s string, v any) error { return yaml.Unmarshal([]byte(s), v) }

func TestCursorAndPageStopOnEmptyPage(t *testing.T) {
	// Neon: a cursor on every page; an empty page ends the list.
	r := engineAdapter(t, `  things:
    path: /v1/things
    pagination: { next: pagination.cursor, param: cursor, items: data }`, thingKind)
	res, calls := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		if q.Get("cursor") == "" {
			return 200, `{"data":[{"id":"a"}],"pagination":{"cursor":"c1"}}`, ""
		}
		return 200, `{"data":[],"pagination":{"cursor":"c1"}}`, ""
	})
	if len(res.Resources) != 1 || len(calls) != 2 || failed(res) != "" {
		t.Errorf("cursor: resources %d, calls %v; %s", len(res.Resources), calls, failed(res))
	}
	// Cloudflare lists without total_pages: keep paging until a page is empty.
	r = engineAdapter(t, `  things:
    path: /v1/things
    pagination: { style: page, param: page, total: result_info.total_pages, items: data }`, thingKind)
	res, calls = scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		switch q.Get("page") {
		case "":
			return 200, `{"data":[{"id":"a"}],"result_info":{"page":1}}`, ""
		case "2":
			return 200, `{"data":[{"id":"b"}],"result_info":{"page":2}}`, ""
		}
		return 200, `{"data":[],"result_info":{"page":3}}`, ""
	})
	if len(res.Resources) != 2 || len(calls) != 3 {
		t.Errorf("page: resources %d, calls %v; %s", len(res.Resources), calls, failed(res))
	}
}

func TestArrayAttributeAndDueWithin(t *testing.T) {
	r := engineAdapter(t, `  things: { path: /v1/things }`, `  - kind: thing
    sources:
      - endpoint: things
        items: data
        checks_rules: [open_thing, expiring_thing]
        id: "{id}"
        attributes:
          - { name: cidrs, field: "ipAllowList[].cidrBlock" }
          - { name: expires_at, field: expiresAt }
`+"rules:\n"+`  - id: open_thing
    kind: thing
    severity: high
    signal: api_field
    exposed: { field: cidrs, contains_any: [0.0.0.0/0] }
    title: Open
    explanation: Open.
    fix: { kind: steps, text: Close it. }
  - id: expiring_thing
    kind: thing
    severity: low
    signal: api_field
    exposed: { field: expires_at, due_within: 7d }
    title: Expiring
    explanation: Expiring.
    fix: { kind: steps, text: Upgrade. }`)
	soon := time.Now().UTC().Add(3 * 24 * time.Hour).Format(time.RFC3339)
	later := time.Now().UTC().Add(60 * 24 * time.Hour).Format(time.RFC3339)
	res, _ := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		return 200, `{"data":[
			{"id":"open","ipAllowList":[{"cidrBlock":"10.0.0.0/8"},{"cidrBlock":"0.0.0.0/0"}],"expiresAt":"` + soon + `"},
			{"id":"closed","ipAllowList":[],"expiresAt":"` + later + `"},
			{"id":"nolist","ipAllowList":null,"expiresAt":null}]}`, ""
	})
	got := map[string]string{}
	for _, f := range res.Findings {
		got[f.ID] = string(f.State)
	}
	want := map[string]string{
		"open_thing:acme.thing:open": "exposed", "open_thing:acme.thing:closed": "not_exposed", "open_thing:acme.thing:nolist": "not_exposed",
		"expiring_thing:acme.thing:open": "exposed", "expiring_thing:acme.thing:closed": "not_exposed", "expiring_thing:acme.thing:nolist": "not_exposed",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	for _, res := range res.Resources {
		if res.ProviderID == "open" && res.Attributes["cidrs"] != "10.0.0.0/8,0.0.0.0/0" {
			t.Errorf("cidrs = %v", res.Attributes["cidrs"])
		}
	}
}

func TestForEachWhereAndIncompleteIf(t *testing.T) {
	r := engineAdapter(t, `  things: { path: /v1/things }
  parts:
    path: /v1/things/{id}/parts
    params: { id: "{parent.id}" }`, `  - kind: thing
    sources:
      - { endpoint: things, items: data, id: "{id}", incomplete_if: unavailable_ids, attributes: [engine] }
  - kind: part
    for_each: thing
    for_each_where: { field: engine, equals: vitess }
    sources:
      - { endpoint: parts, items: $, id: "{parent.provider_id}/{id}" }`)
	res, calls := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		if path == "/v1/things" {
			return 200, `{"data":[{"id":"v","engine":"vitess"},{"id":"p","engine":"postgres"}],"unavailable_ids":["x"]}`, ""
		}
		return 200, `[{"id":"1"}]`, ""
	})
	for _, c := range calls {
		if c.path == "/v1/things/p/parts" {
			t.Errorf("parts listed for a parent for_each_where excludes")
		}
	}
	if len(res.Resources) != 3 {
		t.Errorf("resources = %d", len(res.Resources))
	}
	if !strings.Contains(failed(res), "incomplete") {
		t.Errorf("incomplete list not reported: %q", failed(res))
	}
}

func TestTokenModes(t *testing.T) {
	y := `format: 1
provider: acme
display_name: Acme
api: { base_url: https://api.acme.dev, auth: { header: Authorization, scheme: Bearer } }
endpoints:
  me: { path: /v1/account }
accounts:
  endpoint: me
  items: $
  id: "{id}:{token_mode}"
  name: "{name} ({token_mode})"
  token_modes: { live: '^(sk|rk)_live_', test: '^(sk|rk)_test_' }
kinds: []
`
	r, err := LoadFile("acme.yaml", []byte(y))
	if err != nil {
		t.Fatal(err)
	}
	b := broker.NewOffline(r.Allowlist(), func(m, p, q string) (int, []byte) {
		return 200, []byte(`{"id":"acct_1","name":"Shop"}`)
	})
	for tok, want := range map[string]string{"rk_live_abc": "acct_1:live", "sk_test_abc": "acct_1:test"} {
		accts, err := r.Accounts(context.Background(), b, "acme", tok)
		if err != nil || len(accts) != 1 || accts[0].ID != want || !strings.Contains(accts[0].Name, "(") {
			t.Errorf("%s: %+v %v", tok[:8], accts, err)
		}
	}
}

func TestUserinfoNeverStored(t *testing.T) {
	r := engineAdapter(t, `  things: { path: /v1/things }`, `  - kind: thing
    sources:
      - { endpoint: things, items: data, id: "{id}", name: "{url}", url: "{url}" }`)
	res, _ := scanOffline(t, r, func(path string, q url.Values) (int, string, string) {
		return 200, `{"data":[{"id":"w","url":"https://user:hunter2@hooks.example.com/stripe"}]}`, ""
	})
	if len(res.Resources) != 1 || strings.Contains(res.Resources[0].Name, "hunter2") || strings.Contains(*res.Resources[0].Url, "hunter2") {
		t.Errorf("credentials kept: %+v", res.Resources)
	}
}
