package openapi

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

const info = "info: {title: t, version: \"1\"}\n"

func parse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src), Options{})
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return d
}

func op(t *testing.T, d *Doc, method, path string) *Operation {
	t.Helper()
	for _, o := range d.Operations {
		if o.Method == method && o.Path == path {
			return o
		}
	}
	t.Fatalf("no operation %s %s (have %d)", method, path, len(d.Operations))
	return nil
}

func hasWarning(d *Doc, sub string) bool {
	for _, w := range d.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// okSchema returns the success response schema of an operation.
func okSchema(t *testing.T, o *Operation) *Schema {
	t.Helper()
	r := o.Success()
	if r == nil || r.Schema == nil {
		t.Fatalf("%s %s: no success schema", o.Method, o.Path)
	}
	return r.Schema
}

func TestVersionDetection(t *testing.T) {
	ok := []struct {
		src          string
		major, minor int
		warn         string
	}{
		{"openapi: 3.0\n" + info, 3, 0, "no patch"},
		{"openapi: 3.1\n" + info, 3, 1, "no patch"},
		{"openapi: 3.2.1\n" + info, 3, 2, ""},
		{"openapi: 3.0.4\n" + info, 3, 0, ""},
		{"openapi: 3.3.0\n" + info, 3, 3, "newer than 3.2"},
		{"swagger: 2.0\n" + info, 2, 0, ""},
		{"swagger: \"2.0\"\n" + info, 2, 0, ""},
		{`{"openapi": 3.1, "info": {"title": "t", "version": "1"}}`, 3, 1, "no patch"},
	}
	for _, c := range ok {
		d := parse(t, c.src)
		if d.Major != c.major || d.Minor != c.minor {
			t.Errorf("%q: version %d.%d, want %d.%d", c.src, d.Major, d.Minor, c.major, c.minor)
		}
		if c.warn != "" && !hasWarning(d, c.warn) {
			t.Errorf("%q: no %q warning in %v", c.src, c.warn, d.Warnings)
		}
		if c.warn == "" && len(d.Warnings) > 0 {
			t.Errorf("%q: unexpected warnings %v", c.src, d.Warnings)
		}
	}
	bad := []string{
		"openapi: 4.0.0\n" + info,
		"openapi: 2.0\n" + info,
		"openapi: three\n" + info,
		"swagger: \"1.2\"\n" + info,
		"info: {title: t}\npaths: {}\n",
		"[1, 2]",
		"just a string",
		"",
		"   \n\t",
		`{"openapi": "3.0.0"} trailing`,
		"{\"openapi\": ",
		"openapi: [3, 0]\n",
	}
	for _, src := range bad {
		if _, err := Parse([]byte(src), Options{}); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}

func TestJSONTabsAndBOM(t *testing.T) {
	src := "\xef\xbb\xbf{\n\t\"openapi\": \"3.0.3\",\n\t\"info\": {\"title\": \"Tabbed\", \"version\": \"1\"},\n\t\"paths\": {\n\t\t\"/a\": {\"get\": {\"responses\": {\"200\": {\"description\": \"ok\"}}}}\n\t}\n}\n"
	d := parse(t, src)
	if d.Title != "Tabbed" || len(d.Operations) != 1 {
		t.Errorf("title %q, %d ops", d.Title, len(d.Operations))
	}
	// YAML with a BOM too.
	d = parse(t, "\xef\xbb\xbfopenapi: 3.0.3\n"+info)
	if d.Major != 3 {
		t.Errorf("YAML with BOM: major %d", d.Major)
	}
	// Deeply nested JSON must fail cleanly, not overflow the stack.
	deep := `{"openapi":"3.0.0","x":` + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + "}"
	if _, err := Parse([]byte(deep), Options{}); err == nil {
		t.Error("100000 nested arrays: no error")
	}
}

func TestAnchorsAliasesMergeKeys(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`x-defs:
  err: &err
    description: not found
  meta: &meta
    x-tier: gold
paths:
  /a:
    get:
      parameters:
        - &limit { name: limit, in: query, schema: { type: integer, maximum: 50 } }
      responses:
        "200": &ok
          description: ok
          content:
            application/json:
              schema: { type: object, properties: { id: { type: string } } }
        "404": *err
  /b:
    get:
      <<: *meta
      parameters: [*limit]
      responses:
        <<: { "200": *ok }
        "201":
          <<: *ok
          description: created
  /c:
    get:
      responses:
        <<: [{ "200": *ok }, { "200": *err, "404": *err }]
`)
	b := op(t, d, "get", "/b")
	if b.Param("query", "limit") == nil || b.Param("query", "limit").Schema.Maximum == nil {
		t.Errorf("/b: aliased limit param missing: %+v", b.Params)
	}
	if b.Ext["x-tier"] != "gold" {
		t.Errorf("/b: merged extension missing: %v", b.Ext)
	}
	codes := map[string]*Response{}
	for _, r := range b.Responses {
		codes[r.Code] = r
	}
	if codes["200"] == nil || codes["200"].Schema == nil || codes["200"].Schema.Properties["id"] == nil {
		t.Errorf("/b 200 from merge key: %+v", codes["200"])
	}
	if r := codes["201"]; r == nil || r.Description != "created" || r.Schema == nil {
		t.Errorf("/b 201 (merge then override): %+v", r)
	}
	// The first source of a merge sequence wins.
	c := op(t, d, "get", "/c")
	for _, r := range c.Responses {
		if r.Code == "200" && r.Schema == nil {
			t.Errorf("/c 200 took the later merge source")
		}
	}
	if len(c.Responses) != 2 {
		t.Errorf("/c: %d responses, want 2", len(c.Responses))
	}
}

func TestUnquotedResponseCodes(t *testing.T) {
	d := parse(t, `swagger: 2.0
`+info+`produces: [application/json]
paths:
  /projects:
    get:
      responses:
        200:
          description: ok
          schema: { type: array, items: { $ref: '#/definitions/Project' } }
        404: { description: nope }
definitions:
  Project: { type: object, properties: { id: { type: string } } }
`)
	o := op(t, d, "get", "/projects")
	r := o.Success()
	if r == nil || r.Code != "200" || !r.JSON || r.Schema == nil || r.Schema.Items == nil || r.Schema.Items.Name != "Project" {
		t.Fatalf("success = %+v", r)
	}
}

func TestJSONPointerEscaping(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a/{b}:
    get:
      parameters: [{ name: b, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: ok } }
  /c:
    $ref: '#/paths/~1a~1%7Bb%7D'
  /d:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  tilde: { $ref: '#/components/schemas/a~01b' }
                  slash: { $ref: '#/components/schemas/a~1b' }
                  pct: { $ref: '#/components/schemas/with%20space' }
components:
  schemas:
    "a~1b": { type: string, format: tilde-one }
    "a/b": { type: string, format: slash }
    "with space": { type: integer }
`)
	c := op(t, d, "get", "/c")
	if c.Param("path", "b") == nil {
		t.Errorf("/c: path item ref to /a/{b} not followed: %+v", c.Params)
	}
	s := okSchema(t, op(t, d, "get", "/d"))
	if p := s.Properties["tilde"]; p == nil || p.Format != "tilde-one" {
		t.Errorf("~01 should decode to the key a~1b: %+v", p)
	}
	if p := s.Properties["slash"]; p == nil || p.Format != "slash" {
		t.Errorf("~1 should decode to the key a/b: %+v", p)
	}
	if p := s.Properties["pct"]; p == nil || !p.Is("integer") {
		t.Errorf("%%20 should decode to a space: %+v", p)
	}
}

func TestRefChainsAndCycles(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  chain: { $ref: '#/components/schemas/A' }
                  node: { $ref: '#/components/schemas/Node' }
                  self: { $ref: '#/components/schemas/Self' }
                  loop: { $ref: '#/components/schemas/L1' }
                  mutual: { $ref: '#/components/schemas/M1' }
                  missing: { $ref: '#/components/schemas/Nope' }
                  anchor: { $ref: '#foo' }
components:
  schemas:
    A: { $ref: '#/components/schemas/B' }
    B: { $ref: '#/components/schemas/C' }
    C: { type: object, properties: { id: { type: string } } }
    Node:
      type: object
      properties:
        id: { type: string }
        children: { type: array, items: { $ref: '#/components/schemas/Node' } }
        parent: { $ref: '#/components/schemas/Node' }
    Self: { $ref: '#/components/schemas/Self' }
    L1: { $ref: '#/components/schemas/L2' }
    L2: { $ref: '#/components/schemas/L1' }
    M1: { type: object, properties: { m2: { allOf: [{ $ref: '#/components/schemas/M2' }] } } }
    M2: { type: object, properties: { m1: { allOf: [{ $ref: '#/components/schemas/M1' }] }, id: { type: string } } }
`)
	s := okSchema(t, op(t, d, "get", "/x"))
	if p := s.Properties["chain"]; p == nil || p.Properties["id"] == nil {
		t.Errorf("ref to ref to ref not resolved: %+v", p)
	}
	n := s.Properties["node"]
	if n == nil || n.Properties["children"] == nil || n.Properties["children"].Items != n || n.Properties["parent"] != n {
		t.Errorf("recursive Node should resolve to the same *Schema")
	}
	if p := s.Properties["mutual"]; p == nil || p.Properties["m2"] == nil || p.Properties["m2"].Properties["id"] == nil {
		t.Errorf("mutually recursive allOf: %+v", p)
	}
	if !hasWarning(d, "Nope") {
		t.Errorf("missing ref: no warning in %v", d.Warnings)
	}
	if !hasWarning(d, "#foo") {
		t.Errorf("anchor ref: no warning in %v", d.Warnings)
	}
}

func TestExternalFileRefs(t *testing.T) {
	fsys := fstest.MapFS{
		"api/openapi.yaml": {Data: []byte(`openapi: 3.1.0
` + info + `paths:
  /projects:
    $ref: './paths/projects.yaml'
  /evil:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  up: { $ref: '../../etc/passwd' }
                  up2: { $ref: '../secret.yaml#/X' }
                  url: { $ref: 'https://example.com/common.yaml#/X' }
                  file: { $ref: 'file:///etc/passwd' }
                  gone: { $ref: './schemas/nope.yaml#/X' }
components:
  schemas:
    User: { type: object, properties: { id: { type: string }, email: { type: string } } }
`)},
		"api/paths/projects.yaml": {Data: []byte(`get:
  responses:
    "200":
      description: ok
      content:
        application/json:
          schema: { type: array, items: { $ref: '../schemas/project.yaml#/Project' } }
`)},
		"api/schemas/project.yaml": {Data: []byte(`Project:
  allOf:
    - $ref: 'common.json#/Base'
    - type: object
      properties:
        owner: { $ref: '#/Owner' }
        user: { $ref: '../openapi.yaml#/components/schemas/User' }
Owner: { type: object, properties: { login: { type: string } } }
`)},
		"api/schemas/common.json": {Data: []byte("{\n\t\"Base\": {\"type\": \"object\", \"properties\": {\"id\": {\"type\": \"string\"}}}\n}")},
		"secret.yaml":             {Data: []byte("X: { type: string, description: outside }\n")},
	}
	data, _ := fsys.ReadFile("api/openapi.yaml")
	d, err := Parse(data, Options{FS: fsys, Path: "api/openapi.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	p := op(t, d, "get", "/projects")
	item := okSchema(t, p).Items
	if item == nil || item.Properties["id"] == nil {
		t.Fatalf("Project from a split file: %+v", item)
	}
	if o := item.Properties["owner"]; o == nil || o.Properties["login"] == nil {
		t.Errorf("#/Owner inside project.yaml must resolve in that file: %+v", o)
	}
	if u := item.Properties["user"]; u == nil || u.Properties["email"] == nil {
		t.Errorf("ref back to the entry document: %+v", u)
	}
	ev := okSchema(t, op(t, d, "get", "/evil"))
	// The FS is the sandbox: ../ inside it is fine, out of it is refused.
	if p := ev.Properties["up2"]; p == nil || p.Description != "outside" {
		t.Errorf("up2: a ref inside the FS should resolve: %+v", p)
	}
	for _, k := range []string{"up", "url", "file", "gone"} {
		if ev.Properties[k] != nil {
			t.Errorf("%s: ref should not resolve, got %+v", k, ev.Properties[k])
		}
	}
	if !hasWarning(d, "outside") || !hasWarning(d, "URL") {
		t.Errorf("warnings %v", d.Warnings)
	}
	// Without an FS, refs to other files only warn.
	d2, err := Parse(data, Options{Path: "api/openapi.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(d2, "other files") {
		t.Errorf("no FS: warnings %v", d2.Warnings)
	}
}

func TestSwaggerServers(t *testing.T) {
	cases := map[string]string{
		"host: api.example.com\nbasePath: /v1\nschemes: [http, https]\n": "https://api.example.com/v1",
		"host: api.example.com\nbasePath: /v1/\nschemes: [http]\n":       "http://api.example.com/v1",
		"host: api.example.com\n":                                        "https://api.example.com",
		"host: api.example.com\nbasePath: /\n":                           "https://api.example.com",
		"host: api.example.com:8443\nbasePath: v2\n":                     "https://api.example.com:8443/v2",
		"basePath: /api\n":                                               "/api",
		"schemes: [HTTPS]\nhost: API.example.com\n":                      "https://API.example.com",
	}
	for src, want := range cases {
		d := parse(t, "swagger: \"2.0\"\n"+info+src)
		if len(d.Servers) != 1 || d.Servers[0].URL != want {
			t.Errorf("%q: servers %+v, want %s", src, d.Servers, want)
		}
	}
	if d := parse(t, "swagger: \"2.0\"\n"+info); len(d.Servers) != 0 {
		t.Errorf("no host: servers %+v", d.Servers)
	}
}

func TestServerVariables(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`servers:
  - url: https://{region}.api.example.com/{ver}/
    variables:
      region: { default: eu, enum: [eu, us] }
      ver: { default: v2 }
  - url: https://{env}.example.com
    variables:
      env: { enum: [prod, staging] }
  - url: https://{tenant}.example.com/api
    variables:
      tenant: { description: your tenant }
  - url: /relative/
`)
	want := []string{"https://eu.api.example.com/v2", "https://prod.example.com", "https://{tenant}.example.com/api", "/relative"}
	if len(d.Servers) != len(want) {
		t.Fatalf("servers %+v", d.Servers)
	}
	for i, w := range want {
		if d.Servers[i].URL != w {
			t.Errorf("server %d = %s, want %s", i, d.Servers[i].URL, w)
		}
	}
	if !hasWarning(d, "{tenant} has no default") {
		t.Errorf("warnings %v", d.Warnings)
	}
}

func TestParamOverride(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`paths:
  /projects/{id}:
    parameters:
      - { name: id, in: path, required: true, schema: { type: string } }
      - { name: id, in: query, schema: { type: string } }
      - { name: X-Trace, in: header, schema: { type: string } }
      - { name: shared, in: query, description: path level }
    get:
      parameters:
        - { name: id, in: path, required: true, schema: { type: integer } }
        - { name: shared, in: query, description: op level }
      responses: { "200": { description: ok } }
`)
	o := op(t, d, "get", "/projects/{id}")
	if p := o.Param("path", "id"); p == nil || !p.Schema.Is("integer") {
		t.Errorf("op-level path id should replace the path-level one: %+v", p)
	}
	if o.Param("query", "id") == nil {
		t.Errorf("same name, other location is not an override")
	}
	if p := o.Param("query", "shared"); p == nil || p.Description != "op level" {
		t.Errorf("shared: %+v", p)
	}
	n := 0
	for _, p := range o.Params {
		if p.Name == "id" || p.Name == "shared" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d id/shared params, want 3: %+v", n, o.Params)
	}
}

// Header names are case-insensitive, so an operation's x-trace replaces the
// path item's X-Trace.
func TestParamOverrideHeaderCase(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`paths:
  /a:
    parameters:
      - { name: X-Trace, in: header, required: true, schema: { type: string } }
    get:
      parameters:
        - { name: x-trace, in: header, required: false, schema: { type: string } }
      responses: { "200": { description: ok } }
`)
	o := op(t, d, "get", "/a")
	n := 0
	for _, p := range o.Params {
		if strings.EqualFold(p.Name, "x-trace") {
			n++
			if p.Required {
				t.Errorf("the op-level (optional) header should win")
			}
		}
	}
	if n != 1 {
		t.Errorf("%d trace headers, want 1", n)
	}
}

func TestSwaggerBodyAndFormData(t *testing.T) {
	d := parse(t, `swagger: "2.0"
`+info+`host: api.example.com
paths:
  /things:
    parameters:
      - { name: body, in: body, schema: { type: object, properties: { inherited: { type: string } } } }
    post:
      parameters:
        - { name: limit, in: query, type: integer, maximum: 100 }
        - { name: tags, in: query, type: array, items: { type: string } }
      responses: { "201": { description: ok } }
    put:
      parameters:
        - { name: thing, in: body, required: true, schema: { $ref: '#/definitions/Thing' } }
      responses: { "200": { description: ok } }
  /upload:
    post:
      consumes: [multipart/form-data]
      parameters:
        - { name: file, in: formData, type: file }
        - { name: note, in: formData, type: string }
      responses: { "200": { description: ok } }
definitions:
  Thing: { type: object, properties: { name: { type: string } } }
`)
	post := op(t, d, "post", "/things")
	if post.Body == nil || post.Body.Properties["inherited"] == nil {
		t.Errorf("path-level body param not inherited: %+v", post.Body)
	}
	if p := post.Param("query", "limit"); p == nil || !p.Schema.Is("integer") || p.Schema.Maximum == nil || *p.Schema.Maximum != 100 {
		t.Errorf("2.0 inline param schema: %+v", p)
	}
	if p := post.Param("query", "tags"); p == nil || p.Schema.Items == nil || !p.Schema.Items.Is("string") {
		t.Errorf("2.0 array param: %+v", p)
	}
	put := op(t, d, "put", "/things")
	if put.Body == nil || put.Body.Name != "Thing" {
		t.Errorf("op body should replace the path body: %+v", put.Body)
	}
	for _, p := range append(put.Params, post.Params...) {
		if p.In == "body" {
			t.Errorf("body param left in Params: %+v", p)
		}
	}
	up := op(t, d, "post", "/upload")
	if len(up.Params) != 0 || up.Body != nil {
		t.Errorf("formData: params %+v body %+v", up.Params, up.Body)
	}
}

func TestSuccessChoice(t *testing.T) {
	cases := []struct {
		codes []string
		want  string
	}{
		{[]string{"201", "200", "default"}, "200"},
		{[]string{"default", "204", "201"}, "201"},
		{[]string{"2XX", "default"}, "2XX"},
		{[]string{"2xx", "202"}, "202"},
		{[]string{"default", "404"}, "default"},
		{[]string{"404", "500"}, ""},
		{[]string{"301", "default"}, "default"},
	}
	for _, c := range cases {
		o := &Operation{}
		for _, code := range c.codes {
			o.Responses = append(o.Responses, &Response{Code: code})
		}
		got := ""
		if r := o.Success(); r != nil {
			got = r.Code
		}
		if got != c.want {
			t.Errorf("%v: success %q, want %q", c.codes, got, c.want)
		}
	}
}

func TestMediaTypeChoice(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`paths:
  /charset:
    get:
      responses:
        "200": { description: ok, content: { "application/json; charset=utf-8": { schema: { type: object, properties: { a: { type: string } } } } } }
  /vnd:
    get:
      responses:
        "200":
          description: ok
          content:
            text/html: { schema: { type: string } }
            application/vnd.api+json: { schema: { type: object, properties: { data: { type: array, items: { type: object } } } } }
  /plainjson:
    get:
      responses:
        "200":
          description: ok
          content:
            application/hal+json: { schema: { type: string } }
            Application/JSON: { schema: { type: object, properties: { b: { type: string } } } }
  /star:
    get:
      responses:
        "200": { description: ok, content: { "*/*": { schema: { type: object, properties: { c: { type: string } } } } } }
  /csv:
    get:
      responses:
        "200": { description: ok, content: { text/csv: { schema: { type: string } } } }
  /ndjson:
    get:
      responses:
        "200": { description: ok, content: { application/x-ndjson: { schema: { type: object } } } }
  /nocontent:
    get:
      responses: { "204": { description: gone } }
`)
	check := func(path, mt string, json bool) {
		t.Helper()
		r := op(t, d, "get", path).Success()
		if r == nil || r.MediaType != mt || r.JSON != json {
			t.Errorf("%s: %+v, want %s json=%v", path, r, mt, json)
		}
	}
	check("/charset", "application/json; charset=utf-8", true)
	check("/vnd", "application/vnd.api+json", true)
	check("/plainjson", "Application/JSON", true)
	check("/star", "*/*", true)
	check("/csv", "text/csv", false)
	check("/ndjson", "application/x-ndjson", false)
	check("/nocontent", "", false)
}

func TestNullableSpellings(t *testing.T) {
	d2 := parse(t, `swagger: "2.0"
`+info+`paths:
  /a:
    get:
      responses:
        "200": { description: ok, schema: { type: object, properties: { name: { type: string, x-nullable: true } } } }
`)
	if p := okSchema(t, op(t, d2, "get", "/a")).Properties["name"]; p == nil || !p.Nullable || !p.Is("string") {
		t.Errorf("x-nullable: %+v", p)
	}
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  n30: { type: string, nullable: true }
                  n31: { type: [string, "null"] }
                  any: { anyOf: [{ $ref: '#/components/schemas/User' }, { type: "null" }] }
                  one: { oneOf: [{ type: "null" }, { $ref: '#/components/schemas/User' }] }
                  en: { type: string, enum: [a, b, null] }
                  cn: { const: null }
                  plain: { $ref: '#/components/schemas/User' }
components:
  schemas:
    User: { type: object, required: [id], properties: { id: { type: string } } }
`)
	s := okSchema(t, op(t, d, "get", "/a"))
	for _, k := range []string{"n30", "n31", "en", "cn"} {
		if p := s.Properties[k]; p == nil || !p.Nullable {
			t.Errorf("%s not nullable: %+v", k, p)
		}
	}
	if p := s.Properties["n31"]; len(p.Types) != 1 || p.Types[0] != "string" {
		t.Errorf("n31 types %v", p.Types)
	}
	if p := s.Properties["en"]; len(p.Enum) != 2 {
		t.Errorf("en enum %v", p.Enum)
	}
	for _, k := range []string{"any", "one"} {
		p := s.Properties[k]
		if p == nil || !p.Nullable || p.Properties["id"] == nil || !p.Required["id"] || !p.Is("object") {
			t.Errorf("%s: %+v", k, p)
		}
	}
	if p := s.Properties["plain"]; p == nil || p.Nullable {
		t.Errorf("the shared User component became nullable: %+v", p)
	}
}

func TestRefSiblings(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  described: { $ref: '#/components/schemas/User', description: The owner }
                  nullable: { $ref: '#/components/schemas/User', nullable: true }
                  ro: { $ref: '#/components/schemas/User', readOnly: true }
                  plain: { $ref: '#/components/schemas/User' }
components:
  schemas:
    User: { type: object, description: A user, properties: { id: { type: string } } }
`)
	s := okSchema(t, op(t, d, "get", "/a"))
	if p := s.Properties["nullable"]; p == nil || !p.Nullable || p.Properties["id"] == nil || p.Name != "User" {
		t.Errorf("$ref + nullable: %+v", p)
	}
	if p := s.Properties["ro"]; p == nil || !p.ReadOnly {
		t.Errorf("$ref + readOnly: %+v", p)
	}
	plain := s.Properties["plain"]
	if plain == nil || plain.Nullable || plain.ReadOnly || plain.Description != "A user" {
		t.Errorf("siblings mutated the shared component: %+v", plain)
	}
	if p := s.Properties["described"]; p == nil || p.Properties["id"] == nil {
		t.Errorf("$ref + description: %+v", p)
	}
}

func TestAllOfDoesNotMutateComponents(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  base: { $ref: '#/components/schemas/Base' }
                  project: { $ref: '#/components/schemas/Project' }
                  other: { $ref: '#/components/schemas/Other' }
components:
  schemas:
    Base:
      type: object
      required: [id]
      properties:
        id: { type: string }
        meta: { type: object, properties: { a: { type: string } } }
    Project:
      allOf:
        - $ref: '#/components/schemas/Base'
        - type: object
          required: [name]
          properties:
            name: { type: string }
            meta: { type: object, properties: { b: { type: string } } }
    Other:
      allOf:
        - $ref: '#/components/schemas/Base'
        - properties: { extra: { type: integer } }
`)
	s := okSchema(t, op(t, d, "get", "/a"))
	p, base, other := s.Properties["project"], s.Properties["base"], s.Properties["other"]
	if p == nil || p.Properties["id"] == nil || p.Properties["name"] == nil || !p.Required["id"] || !p.Required["name"] {
		t.Fatalf("project: %+v", p)
	}
	if m := p.Properties["meta"]; m == nil || m.Properties["a"] == nil || m.Properties["b"] == nil {
		t.Errorf("project.meta should merge a and b: %+v", m)
	}
	if base.Properties["name"] != nil || base.Required["name"] || base.Properties["meta"].Properties["b"] != nil {
		t.Errorf("Base was mutated by Project's allOf: %+v", base)
	}
	if other.Properties["name"] != nil || other.Properties["meta"].Properties["b"] != nil || other.Properties["extra"] == nil {
		t.Errorf("Other: %+v", other)
	}
	if base.Properties["extra"] != nil {
		t.Errorf("Base was mutated by Other's allOf")
	}
}

// absorb clones a property it merges into, but the clone shares the Ext map
// with the shared component, so the merge writes into the component.
func TestAllOfDoesNotMutateComponentExtensions(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  base: { $ref: '#/components/schemas/Base' }
                  ext: { $ref: '#/components/schemas/Ext' }
components:
  schemas:
    Base:
      type: object
      properties:
        token: { type: string, x-origin: base }
    Ext:
      allOf:
        - $ref: '#/components/schemas/Base'
        - properties:
            token: { type: string, x-ms-secret: true }
`)
	s := okSchema(t, op(t, d, "get", "/a"))
	if e := s.Properties["ext"].Properties["token"]; e == nil || e.Ext["x-ms-secret"] != true {
		t.Errorf("Ext.token should carry x-ms-secret: %+v", e)
	}
	if b := s.Properties["base"].Properties["token"]; b.Ext["x-ms-secret"] != nil {
		t.Errorf("Base.token gained Ext's extension: %v", b.Ext)
	}
}

func TestOneOfUnion(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  oneOf:
                    - $ref: '#/components/schemas/Cat'
                    - $ref: '#/components/schemas/Dog'
components:
  schemas:
    Cat: { type: object, required: [id, purrs], properties: { id: { type: string }, purrs: { type: boolean } } }
    Dog: { type: object, required: [id, barks], properties: { id: { type: string }, barks: { type: boolean } } }
`)
	it := okSchema(t, op(t, d, "get", "/a")).Items
	if it == nil || len(it.Variants) != 2 {
		t.Fatalf("items: %+v", it)
	}
	for _, k := range []string{"id", "purrs", "barks"} {
		if it.Properties[k] == nil {
			t.Errorf("union lacks %s", k)
		}
	}
	if !it.Required["id"] || it.Required["purrs"] || it.Required["barks"] {
		t.Errorf("required should be the intersection {id}: %v", it.Required)
	}
	cat := it.Variants[0]
	if cat.Properties["barks"] != nil || !cat.Required["purrs"] {
		t.Errorf("Cat was mutated: %+v", cat)
	}
}

func TestConstExamplesBooleanSchemas(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  kind: { const: project }
                  num: { const: 3 }
                  ex: { type: string, examples: [org_1, org_2] }
                  ex30: { type: string, example: one, examples: [two] }
                  anything: { type: array, items: true }
                  nothing: { type: array, items: false }
                  never: false
                  always: true
`)
	s := okSchema(t, op(t, d, "get", "/a"))
	if p := s.Properties["kind"]; p == nil || len(p.Enum) != 1 || p.Enum[0] != "project" {
		t.Errorf("const: %+v", p)
	}
	if p := s.Properties["num"]; p == nil || len(p.Enum) != 1 || p.Enum[0] != "3" {
		t.Errorf("numeric const: %+v", p)
	}
	if p := s.Properties["ex"]; p == nil || p.Example != "org_1" {
		t.Errorf("examples array: %+v", p)
	}
	if p := s.Properties["ex30"]; p == nil || p.Example != "one" {
		t.Errorf("example beats examples: %+v", p)
	}
	if p := s.Properties["anything"]; p == nil || p.Items == nil {
		t.Errorf("items: true should be an unconstrained schema: %+v", p)
	}
	if p := s.Properties["nothing"]; p == nil || p.Items != nil {
		t.Errorf("items: false: %+v", p)
	}
	if s.Properties["never"] != nil {
		t.Errorf("a false property schema should be dropped")
	}
	if s.Properties["always"] == nil {
		t.Errorf("a true property schema should be kept")
	}
}

func TestWebhooksOnly(t *testing.T) {
	d := parse(t, `openapi: 3.1.2
`+info+`webhooks:
  newPet:
    post:
      responses: { "200": { description: ok } }
`)
	if len(d.Operations) != 0 {
		t.Errorf("webhooks are not operations: %+v", d.Operations)
	}
}

func TestOpenAPI32(t *testing.T) {
	d := parse(t, `openapi: 3.2.0
`+info+`paths:
  /search:
    get:
      responses: { "200": { description: plain get } }
    query:
      parameters:
        - in: querystring
          name: q
          content: { application/x-www-form-urlencoded: { schema: { type: object, properties: { term: { type: string } } } } }
      responses: { "200": { description: ok } }
    additionalOperations:
      PURGE: { responses: { "204": { description: gone } } }
      GET: { responses: { "200": { description: duplicate get } } }
      link: { operationId: linkIt, responses: { "200": { description: ok } } }
  /stream:
    get:
      responses:
        "200":
          description: events
          content: { application/jsonl: { itemSchema: { type: object, properties: { id: { type: string } } } } }
`)
	q := op(t, d, "query", "/search")
	if p := q.Param("querystring", "q"); p == nil || p.Schema == nil || p.Schema.Properties["term"] == nil {
		t.Errorf("querystring param: %+v", q.Params)
	}
	op(t, d, "purge", "/search")
	if l := op(t, d, "link", "/search"); l.ID != "linkIt" {
		t.Errorf("additionalOperations lower-case key: %+v", l)
	}
	gets := 0
	for _, o := range d.Operations {
		if strings.EqualFold(o.Method, http.MethodGet) && o.Path == "/search" {
			gets++
			if o.Responses[0].Description != "plain get" {
				t.Errorf("additionalOperations GET replaced the real get")
			}
		}
	}
	if gets != 1 {
		t.Errorf("%d GET /search operations", gets)
	}
	if r := op(t, d, "get", "/stream").Success(); r == nil || r.JSON {
		t.Errorf("jsonl stream should not count as JSON: %+v", r)
	}
}

func TestSecurity(t *testing.T) {
	d := parse(t, `openapi: 3.0.3
`+info+`security: [{ bearer: [] }]
paths:
  /health: { get: { security: [], responses: { "200": { description: ok } } } }
  /me: { get: { security: [{}, { bearer: [] }], responses: { "200": { description: ok } } } }
  /both: { get: { security: [{ bearer: [], key: [] }], responses: { "200": { description: ok } } } }
  /plain: { get: { responses: { "200": { description: ok } } } }
components:
  securitySchemes:
    bearer: { type: http, scheme: Bearer, bearerFormat: JWT }
    key: { type: apiKey, in: Header, name: X-API-Key }
    ref: { $ref: '#/components/securitySchemes/key' }
    oauth:
      type: oauth2
      flows:
        authorizationCode: { authorizationUrl: https://a/x, tokenUrl: https://a/t, scopes: { read: Read things } }
        clientCredentials: { tokenUrl: https://a/t, scopes: { write: Write things } }
        deviceAuthorization: { deviceAuthorizationUrl: https://a/d, tokenUrl: https://a/t, scopes: { device: Device } }
`)
	if h := op(t, d, "get", "/health"); !h.SecuritySet || len(d.EffectiveSecurity(h)) != 0 {
		t.Errorf("/health: %+v", h.Security)
	}
	if m := d.EffectiveSecurity(op(t, d, "get", "/me")); len(m) != 2 || len(m[0]) != 0 {
		t.Errorf("/me: %+v", m)
	}
	if b := d.EffectiveSecurity(op(t, d, "get", "/both")); len(b) != 1 || len(b[0]) != 2 {
		t.Errorf("/both: %+v", b)
	}
	if p := d.EffectiveSecurity(op(t, d, "get", "/plain")); len(p) != 1 || !hasKey(p[0], "bearer") {
		t.Errorf("/plain inherits global: %+v", p)
	}
	b := d.SecuritySchemes["bearer"]
	if b.Scheme != "bearer" || b.BearerFormat != "JWT" {
		t.Errorf("bearer: %+v", b)
	}
	if k := d.SecuritySchemes["key"]; k.In != "header" || k.ParamName != "X-API-Key" {
		t.Errorf("key: %+v", k)
	}
	if r := d.SecuritySchemes["ref"]; r == nil || r.Type != "apiKey" || r.Name != "ref" {
		t.Errorf("$ref scheme: %+v", r)
	}
	o := d.SecuritySchemes["oauth"]
	for _, s := range []string{"read", "write", "device"} {
		if _, ok := o.Scopes[s]; !ok {
			t.Errorf("oauth scope %s missing: %v", s, o.Scopes)
		}
	}
	// A document-level empty list means "no auth", not "undeclared".
	e := parse(t, "openapi: 3.0.3\n"+info+"security: []\n")
	if e.Security == nil || len(e.Security) != 0 {
		t.Errorf("security: [] = %#v", e.Security)
	}
	if n := parse(t, "openapi: 3.0.3\n"+info); n.Security != nil {
		t.Errorf("no security: %#v", n.Security)
	}
}

func TestSwaggerSecurity(t *testing.T) {
	d := parse(t, `swagger: "2.0"
`+info+`securityDefinitions:
  basic: { type: basic }
  key: { type: apiKey, in: query, name: api_key }
  oauth: { type: oauth2, flow: accessCode, authorizationUrl: https://a/x, tokenUrl: https://a/t, scopes: { "read:things": r, "write:things": w } }
`)
	if b := d.SecuritySchemes["basic"]; b.Type != "http" || b.Scheme != "basic" {
		t.Errorf("basic: %+v", b)
	}
	if k := d.SecuritySchemes["key"]; k.In != "query" || k.ParamName != "api_key" {
		t.Errorf("key: %+v", k)
	}
	if o := d.SecuritySchemes["oauth"]; len(o.Scopes) != 2 {
		t.Errorf("oauth scopes: %v", o.Scopes)
	}
}

func TestPathItemRefsAndExtensions(t *testing.T) {
	d := parse(t, `openapi: 3.1.0
`+info+`paths:
  x-internal: { get: { responses: { "200": { description: ok } } } }
  /users: { $ref: '#/components/pathItems/Users' }
  /a:
    get:
      x-null: null
      responses:
        x-foo: bar
        "200": { $ref: '#/components/responses/Ok' }
components:
  pathItems:
    Users: { get: { operationId: listUsers, responses: { "200": { description: ok } } } }
  responses:
    Ok:
      description: ok
      headers: { Link: { schema: { type: string } } }
      content: { application/json: { schema: { type: object, properties: { id: { type: string } } } } }
`)
	if len(d.Operations) != 2 {
		t.Fatalf("%d ops", len(d.Operations))
	}
	if u := op(t, d, "get", "/users"); u.ID != "listUsers" {
		t.Errorf("path item ref: %+v", u)
	}
	a := op(t, d, "get", "/a")
	if len(a.Responses) != 1 || a.Responses[0].Schema == nil || len(a.Responses[0].Headers) != 1 {
		t.Errorf("response ref: %+v", a.Responses)
	}
}

func hasKey(r Requirement, k string) bool { _, ok := r[k]; return ok }
