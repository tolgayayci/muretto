package adaptergen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/tolgayayci/muretto/internal/adaptercheck"
	"github.com/tolgayayci/muretto/internal/openapi"
)

// FakeAPI answers GET requests with JSON built from a spec's response
// schemas, for dry runs (`muretto adapter test --openapi`) and starter
// fixtures. Values are obvious fakes ("example-..."), ids are unique,
// booleans alternate (so a public flag is both true and false), cursors are
// null (one page), and every secret-looking field holds
// adaptercheck.FakeSecret, so a missing strip shows up.
type FakeAPI struct {
	routes    []fakeRoute
	counter   int
	Calls     []string
	Unmatched []string
}

type fakeRoute struct {
	re       *regexp.Regexp
	resp     *openapi.Schema
	literals int
}

// NewFakeAPI serves doc's GET operations under basePath (the base URL's
// path, e.g. /v1), or under an operation's own server path.
func NewFakeAPI(doc *openapi.Doc, basePath string) *FakeAPI {
	f := &FakeAPI{}
	for _, op := range doc.Operations {
		if !strings.EqualFold(op.Method, http.MethodGet) {
			continue
		}
		r := op.Success()
		if r == nil || r.Schema == nil {
			continue
		}
		prefix := basePath
		if len(op.Servers) > 0 {
			if u, err := url.Parse(op.Servers[0].URL); err == nil {
				prefix = strings.TrimSuffix(u.Path, "/")
			}
		}
		var segs []string
		lit := 0
		for _, s := range strings.Split(prefix+op.Path, "/") {
			if isParam(s) {
				segs = append(segs, `[^/]+`)
			} else {
				segs = append(segs, regexp.QuoteMeta(s))
				lit++
			}
		}
		f.routes = append(f.routes, fakeRoute{re: regexp.MustCompile("^" + strings.Join(segs, "/") + "$"), resp: r.Schema, literals: lit})
	}
	// Most specific first: literal segments win over parameters.
	sort.SliceStable(f.routes, func(i, j int) bool { return f.routes[i].literals > f.routes[j].literals })
	return f
}

// Respond is a broker.Responder.
func (f *FakeAPI) Respond(method, path, rawQuery string) (int, []byte) {
	call := method + " " + path
	if rawQuery != "" {
		call += "?" + rawQuery
	}
	f.Calls = append(f.Calls, call)
	for _, rt := range f.routes {
		if method == "GET" && rt.re.MatchString(path) {
			b, _ := json.Marshal(f.Value(rt.resp))
			return 200, b
		}
	}
	f.Unmatched = append(f.Unmatched, call)
	return 404, []byte(`{"error":{"message":"no such endpoint in the spec"}}`)
}

// Value builds a fake JSON value for s.
func (f *FakeAPI) Value(s *openapi.Schema) any {
	return f.synth(s, "", 0, map[*openapi.Schema]int{})
}

func (f *FakeAPI) synth(s *openapi.Schema, name string, depth int, seen map[*openapi.Schema]int) any {
	if s == nil || depth > 10 {
		return nil
	}
	if !s.Scalar() {
		if seen[s] > 1 {
			return nil
		}
		seen[s]++
		defer func() { seen[s]-- }()
	}
	f.counter++
	n := f.counter
	sn := snake(name)
	if name != "" && isSecretField(name, s) {
		return adaptercheck.FakeSecret
	}
	if sn == "has_more" || sn == "has_next_page" {
		return false
	}
	if reCursorField.MatchString(sn) && !s.Is("object") && !s.Is("array") {
		return nil
	}
	switch {
	case s.Is("object") || len(s.Properties) > 0:
		m := map[string]any{}
		for _, k := range s.Order {
			m[k] = f.synth(s.Properties[k], k, depth+1, seen)
		}
		return m
	case s.Is("array"):
		if depth > 2 {
			// Nested arrays get one element: deep expandable schemas
			// (Stripe) would otherwise grow exponentially.
			return []any{f.synth(s.Items, singular(sn), depth+1, seen)}
		}
		return []any{f.synth(s.Items, singular(sn), depth+1, seen), f.synth(s.Items, singular(sn), depth+1, seen)}
	case len(s.Enum) > 0:
		return s.Enum[n%len(s.Enum)]
	case s.Is("boolean"):
		return n%2 == 0
	case s.Is("integer"), s.Is("number"):
		if strings.Contains(sn, "created") || strings.HasSuffix(sn, "_at") {
			return 1790000000000 + n
		}
		return n
	case s.Is("string"):
		switch s.Format {
		case "date-time":
			return fmt.Sprintf("2026-10-%02dT10:00:00Z", n%28+1)
		case "date":
			return fmt.Sprintf("2026-10-%02d", n%28+1)
		case "uri", "url":
			return fmt.Sprintf("https://example.com/%d", n)
		case "uuid":
			return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
		case "email":
			return fmt.Sprintf("user%d@example.com", n)
		}
		if strings.Contains(sn, "email") {
			return fmt.Sprintf("user%d@example.com", n)
		}
		if sn == "" {
			sn = "value"
		}
		return fmt.Sprintf("example-%s-%d", strings.ReplaceAll(sn, "_", "-"), n)
	}
	return fmt.Sprintf("example-%d", n)
}
