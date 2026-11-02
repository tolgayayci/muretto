// Package openapi reads OpenAPI descriptions (Swagger 2.0, OpenAPI 3.0, 3.1
// and 3.2, JSON or YAML) into one normalised model: servers, security
// schemes, operations, parameters, and response and request schemas with
// $refs resolved and allOf/oneOf/anyOf merged.
//
// It is a reader for code generation, not a validator: it accepts what real
// specs do (lenient where the spec is strict) and reports what it skipped as
// warnings. It never touches the network: $refs to other files are read from
// the fs.FS given in Options, and URL refs are reported and left empty.
package openapi

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Doc is one OpenAPI description.
type Doc struct {
	Version     string // the swagger/openapi field as written, e.g. "2.0", "3.1.0"
	Major       int    // 2 or 3
	Minor       int    // 3.x minor version (0 for 2.0)
	Title       string
	Description string
	APIVersion  string   // info.version
	Servers     []Server // base URLs, variables replaced by their defaults
	// Security is the global requirement list; nil when the document does not
	// declare one. An empty Requirement means "no auth needed".
	Security        []Requirement
	SecuritySchemes map[string]*SecurityScheme
	Operations      []*Operation // in document order
	Warnings        []string
}

// Server is one base URL.
type Server struct {
	URL         string // may be relative ("/api/v1") when the spec gives no host
	Description string
}

// Requirement maps security scheme names to required scopes. All schemes in
// one requirement apply together; the doc or operation lists alternatives.
type Requirement map[string][]string

// SecurityScheme is a normalised security scheme. Swagger 2.0 "basic" is
// returned as Type "http", Scheme "basic".
type SecurityScheme struct {
	Name         string
	Type         string // apiKey, http, oauth2, openIdConnect, mutualTLS
	In           string // apiKey: header, query or cookie
	ParamName    string // apiKey: the header, query or cookie name
	Scheme       string // http: bearer, basic, ... (lower case)
	BearerFormat string
	Description  string
	Scopes       map[string]string // oauth2: every scope of every flow
	Deprecated   bool              // 3.2
}

// Operation is one HTTP operation.
type Operation struct {
	Method      string // lower case: get, post, ..., query (3.2), or an additionalOperations key
	Path        string // as written in the spec
	ID          string // operationId
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Params      []*Param // path-level params merged with operation params
	Body        *Schema  // JSON request body (3.x requestBody, 2.0 in: body)
	Responses   []*Response
	// Security is the operation's own requirement list; SecuritySet reports
	// whether it declared one (an empty list then means "no auth").
	Security    []Requirement
	SecuritySet bool
	Servers     []Server // operation- or path-level servers, if any
	Ext         map[string]any
}

// Param is one parameter. Swagger 2.0 inline type/format/items are returned
// as Schema.
type Param struct {
	Name        string
	In          string // path, query, header, cookie, querystring (3.2)
	Required    bool
	Deprecated  bool
	Description string
	Schema      *Schema
}

// Response is one declared response.
type Response struct {
	Code        string // "200", "2XX", "default"
	Description string
	MediaType   string // the chosen media type, "" when the response has no content
	JSON        bool   // MediaType is JSON (application/json, +json, */*)
	Schema      *Schema
	Headers     []string // response header names, e.g. Link
}

// Schema is a normalised JSON schema. Shared refs resolve to the same
// *Schema, so the graph can contain cycles: walk it with a depth limit.
type Schema struct {
	Name        string   // component name when the schema came from a $ref
	Types       []string // without "null"; empty means unconstrained
	Format      string
	Title       string
	Description string
	Properties  map[string]*Schema
	Order       []string // property names in declaration order
	Required    map[string]bool
	Items       *Schema
	Additional  *Schema // additionalProperties schema
	Enum        []string
	Default     any
	Example     any
	Nullable    bool
	ReadOnly    bool
	WriteOnly   bool
	Deprecated  bool
	Maximum     *float64
	Pattern     string
	// Variants are the non-null oneOf/anyOf alternatives (already merged
	// into this schema as a union).
	Variants []*Schema
	Ext      map[string]any
}

// Is reports whether t is one of the schema's types.
func (s *Schema) Is(t string) bool {
	if s == nil {
		return false
	}
	for _, x := range s.Types {
		if x == t {
			return true
		}
	}
	return false
}

// Scalar reports whether the schema is a string, number, integer or boolean.
func (s *Schema) Scalar() bool {
	return s.Is("string") || s.Is("number") || s.Is("integer") || s.Is("boolean")
}

// Param returns the parameter with that location and name, or nil.
func (o *Operation) Param(in, name string) *Param {
	for _, p := range o.Params {
		if p.In == in && p.Name == name {
			return p
		}
	}
	return nil
}

// Success returns the response a successful call returns: 200, then other
// 2xx codes in order, then 2XX, then default when no 2xx is declared.
func (o *Operation) Success() *Response {
	var best *Response
	rank := func(r *Response) int {
		switch {
		case r.Code == "200":
			return 0
		case len(r.Code) == 3 && r.Code[0] == '2' && r.Code[1] >= '0' && r.Code[1] <= '9':
			n, _ := strconv.Atoi(r.Code)
			return n - 199 // 201 -> 2, ...
		case strings.EqualFold(r.Code, "2XX"):
			return 200
		case r.Code == "default":
			return 300
		}
		return -1
	}
	br := 1 << 30
	hasTwo := false
	for _, r := range o.Responses {
		k := rank(r)
		if k >= 0 && k < 300 {
			hasTwo = true
		}
		if k >= 0 && k < br {
			best, br = r, k
		}
	}
	if best != nil && best.Code == "default" && hasTwo {
		return nil
	}
	return best
}

// Options locates the document for relative $refs.
type Options struct {
	FS   fs.FS  // where other files of a split spec are read from; optional
	Path string // the document's path inside FS
}

// MaxFileSize caps each file read for a $ref.
const MaxFileSize = 64 << 20

// Parse reads one OpenAPI description.
func Parse(data []byte, opt Options) (*Doc, error) {
	root, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	if !isMap(root) {
		return nil, errors.New("not an OpenAPI document: the top level is not an object")
	}
	l := &loader{
		fsys:     opt.FS,
		files:    map[string]*yaml.Node{},
		schemas:  map[*yaml.Node]*Schema{},
		warned:   map[string]bool{},
		mainFile: opt.Path,
	}
	l.files[opt.Path] = root
	d := &Doc{SecuritySchemes: map[string]*SecurityScheme{}}
	l.doc = d
	if v := str(get(root, "swagger")); v != "" {
		if !strings.HasPrefix(v, "2") {
			return nil, fmt.Errorf("unsupported swagger version %q", v)
		}
		d.Version, d.Major = v, 2
	} else if v := str(get(root, "openapi")); v != "" {
		parts := strings.Split(v, ".")
		major, err1 := strconv.Atoi(parts[0])
		minor := 0
		var err2 error
		if len(parts) > 1 {
			minor, err2 = strconv.Atoi(parts[1])
		}
		if err1 != nil || err2 != nil || major != 3 {
			return nil, fmt.Errorf("unsupported openapi version %q", v)
		}
		if len(parts) < 3 {
			l.warn("openapi version %q has no patch number; reading it as %d.%d", v, major, minor)
		}
		if minor > 2 {
			l.warn("openapi %s is newer than 3.2; reading it as 3.2", v)
		}
		d.Version, d.Major, d.Minor = v, 3, minor
	} else {
		return nil, errors.New("not an OpenAPI document: no swagger or openapi version field")
	}

	info := get(root, "info")
	d.Title = strings.TrimSpace(str(get(info, "title")))
	d.Description = strings.TrimSpace(str(get(info, "description")))
	d.APIVersion = str(get(info, "version"))

	if d.Major == 2 {
		d.Servers = swaggerServers(root)
	} else {
		d.Servers = l.servers(get(root, "servers"))
	}
	l.securitySchemes(root)
	if sec := get(root, "security"); sec != nil {
		d.Security = requirements(sec)
		if d.Security == nil {
			d.Security = []Requirement{}
		}
	}
	l.paths(root)
	return d, nil
}

type loader struct {
	fsys     fs.FS
	mainFile string
	files    map[string]*yaml.Node
	doc      *Doc
	schemas  map[*yaml.Node]*Schema
	warned   map[string]bool
}

func (l *loader) warn(format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	if !l.warned[m] {
		l.warned[m] = true
		l.doc.Warnings = append(l.doc.Warnings, m)
	}
}

func swaggerServers(root *yaml.Node) []Server {
	host := str(get(root, "host"))
	base := str(get(root, "basePath"))
	if base != "" && !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	base = strings.TrimSuffix(base, "/")
	if host == "" {
		if base == "" {
			return nil
		}
		return []Server{{URL: base}}
	}
	schemes := strs(get(root, "schemes"))
	scheme := "https"
	if len(schemes) > 0 {
		scheme = schemes[0]
		for _, s := range schemes {
			if strings.EqualFold(s, "https") {
				scheme = "https"
			}
		}
	}
	return []Server{{URL: strings.ToLower(scheme) + "://" + host + base}}
}

func (l *loader) servers(n *yaml.Node) []Server {
	var out []Server
	for _, s := range items(n) {
		u := str(get(s, "url"))
		if u == "" {
			continue
		}
		for _, e := range entries(get(s, "variables")) {
			def := str(get(e.val, "default"))
			if def == "" {
				if en := strs(get(e.val, "enum")); len(en) > 0 {
					def = en[0]
				}
			}
			if def == "" {
				l.warn("server %s: variable {%s} has no default", u, e.key)
				continue
			}
			u = strings.ReplaceAll(u, "{"+e.key+"}", def)
		}
		out = append(out, Server{URL: strings.TrimSuffix(u, "/"), Description: str(get(s, "description"))})
	}
	return out
}

func requirements(n *yaml.Node) []Requirement {
	var out []Requirement
	for _, r := range items(n) {
		req := Requirement{}
		for _, e := range entries(r) {
			req[e.key] = strs(e.val)
		}
		out = append(out, req)
	}
	return out
}

func (l *loader) securitySchemes(root *yaml.Node) {
	var defs *yaml.Node
	if l.doc.Major == 2 {
		defs = get(root, "securityDefinitions")
	} else {
		defs = get(get(root, "components"), "securitySchemes")
	}
	for _, e := range entries(defs) {
		_, n := l.resolve(l.mainFile, deref(e.val))
		if n == nil {
			continue
		}
		s := &SecurityScheme{
			Name:         e.key,
			Type:         str(get(n, "type")),
			In:           strings.ToLower(str(get(n, "in"))),
			ParamName:    str(get(n, "name")),
			Scheme:       strings.ToLower(str(get(n, "scheme"))),
			BearerFormat: str(get(n, "bearerFormat")),
			Description:  strings.TrimSpace(str(get(n, "description"))),
			Deprecated:   isTrue(get(n, "deprecated")),
		}
		if s.Type == "basic" {
			s.Type, s.Scheme = "http", "basic"
		}
		if s.Type == "oauth2" {
			s.Scopes = map[string]string{}
			addScopes := func(f *yaml.Node) {
				for _, sc := range entries(get(f, "scopes")) {
					s.Scopes[sc.key] = str(sc.val)
				}
			}
			addScopes(n) // 2.0: scopes on the scheme
			for _, f := range entries(get(n, "flows")) {
				addScopes(f.val)
			}
		}
		l.doc.SecuritySchemes[e.key] = s
	}
}

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}

func (l *loader) paths(root *yaml.Node) {
	for _, pe := range entries(get(root, "paths")) {
		if !strings.HasPrefix(pe.key, "/") {
			continue // x- extensions
		}
		file, item := l.resolve(l.mainFile, deref(pe.val))
		if item == nil {
			continue
		}
		pathParams := l.params(file, get(item, "parameters"))
		var pathServers []Server
		if l.doc.Major == 3 {
			pathServers = l.servers(get(item, "servers"))
		}
		add := func(method string, on *yaml.Node) {
			if !isMap(on) {
				return
			}
			op := l.operation(file, method, pe.key, on, pathParams)
			if len(op.Servers) == 0 {
				op.Servers = pathServers
			}
			l.doc.Operations = append(l.doc.Operations, op)
		}
		for _, m := range methods {
			add(m, get(item, m))
		}
		for _, e := range entries(get(item, "additionalOperations")) {
			m := strings.ToLower(e.key)
			if get(item, m) != nil && contains(methods, m) {
				continue
			}
			add(m, deref(e.val))
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (l *loader) operation(file, method, path string, n *yaml.Node, pathParams []*Param) *Operation {
	op := &Operation{
		Method:      method,
		Path:        path,
		ID:          str(get(n, "operationId")),
		Summary:     strings.TrimSpace(str(get(n, "summary"))),
		Description: strings.TrimSpace(str(get(n, "description"))),
		Tags:        strs(get(n, "tags")),
		Deprecated:  isTrue(get(n, "deprecated")),
		Ext:         extensions(n),
	}
	own := l.params(file, get(n, "parameters"))
	seen := map[string]bool{}
	for _, p := range own {
		seen[paramKey(p)] = true
	}
	for _, p := range pathParams {
		if !seen[paramKey(p)] {
			op.Params = append(op.Params, p)
		}
	}
	for _, p := range own {
		if p.In == "body" {
			op.Body = p.Schema
			continue
		}
		if p.In == "formData" {
			continue
		}
		op.Params = append(op.Params, p)
	}
	// A body param inherited from the path item.
	for _, p := range pathParams {
		if p.In == "body" && op.Body == nil {
			op.Body = p.Schema
		}
	}
	op.Params = slicesFilter(op.Params, func(p *Param) bool { return p.In != "body" && p.In != "formData" })

	if l.doc.Major == 3 {
		if rb := get(n, "requestBody"); rb != nil {
			f, rbn := l.resolve(file, rb)
			if rbn != nil {
				if mt, sn := pickContent(get(rbn, "content")); sn != nil && isJSON(mt) {
					op.Body = l.schema(f, sn)
				}
			}
		}
		op.Servers = l.servers(get(n, "servers"))
	}
	if sec := get(n, "security"); sec != nil {
		op.SecuritySet = true
		op.Security = requirements(sec)
	}
	// 2.0: an operation's produces replaces the global one, even when empty.
	var produces []string
	if l.doc.Major == 2 {
		if pn := get(n, "produces"); pn != nil {
			produces = strs(pn)
		} else {
			produces = strs(get(l.files[l.mainFile], "produces"))
		}
	}
	for _, e := range entries(get(n, "responses")) {
		if strings.HasPrefix(e.key, "x-") {
			continue
		}
		f, rn := l.resolve(file, deref(e.val))
		if rn == nil {
			continue
		}
		r := &Response{Code: e.key, Description: strings.TrimSpace(str(get(rn, "description")))}
		for _, h := range entries(get(rn, "headers")) {
			r.Headers = append(r.Headers, h.key)
		}
		if l.doc.Major == 2 {
			if sn := get(rn, "schema"); sn != nil {
				r.Schema = l.schema(f, sn)
				r.MediaType = "application/json"
				r.JSON = true
				if len(produces) > 0 {
					r.MediaType, r.JSON = produces[0], false
					for _, p := range produces {
						if isJSON(p) {
							r.MediaType, r.JSON = p, true
							break
						}
					}
				}
			}
		} else if mt, sn := pickContent(get(rn, "content")); mt != "" {
			r.MediaType = mt
			r.JSON = isJSON(mt)
			if sn != nil {
				r.Schema = l.schema(f, sn)
			}
		}
		op.Responses = append(op.Responses, r)
	}
	return op
}

// paramKey identifies a parameter for overriding: (in, name), with header
// names compared case-insensitively.
func paramKey(p *Param) string {
	if p.In == "header" {
		return p.In + "\x00" + strings.ToLower(p.Name)
	}
	return p.In + "\x00" + p.Name
}

func slicesFilter[T any](s []T, keep func(T) bool) []T {
	out := s[:0]
	for _, x := range s {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

func extensions(n *yaml.Node) map[string]any {
	var out map[string]any
	for _, e := range entries(n) {
		if strings.HasPrefix(e.key, "x-") {
			if out == nil {
				out = map[string]any{}
			}
			out[e.key] = value(e.val)
		}
	}
	return out
}

func (l *loader) params(file string, n *yaml.Node) []*Param {
	var out []*Param
	for _, raw := range items(n) {
		f, pn := l.resolve(file, raw)
		if pn == nil {
			continue
		}
		p := &Param{
			Name:        str(get(pn, "name")),
			In:          str(get(pn, "in")),
			Required:    isTrue(get(pn, "required")),
			Deprecated:  isTrue(get(pn, "deprecated")),
			Description: strings.TrimSpace(str(get(pn, "description"))),
		}
		if p.Name == "" && p.In != "querystring" {
			continue
		}
		if p.In == "path" {
			p.Required = true
		}
		switch {
		case p.In == "body":
			p.Schema = l.schema(f, get(pn, "schema"))
		case get(pn, "schema") != nil:
			p.Schema = l.schema(f, get(pn, "schema"))
		case get(pn, "content") != nil:
			_, sn := pickContent(get(pn, "content"))
			p.Schema = l.schema(f, sn)
		default:
			// Swagger 2.0: type, format, items, enum, default inline.
			p.Schema = l.schema(f, pn)
		}
		out = append(out, p)
	}
	return out
}

// pickContent chooses the media type to read from a content map: JSON first
// (application/json, then +json types, then */*), else the first one.
func pickContent(content *yaml.Node) (string, *yaml.Node) {
	es := entries(content)
	if len(es) == 0 {
		return "", nil
	}
	best, bestRank := -1, 1<<30
	for i, e := range es {
		r := jsonRank(e.key)
		if r < bestRank {
			best, bestRank = i, r
		}
	}
	e := es[best]
	sn := get(e.val, "schema")
	if sn == nil {
		sn = get(e.val, "itemSchema") // 3.2 sequential media types
	}
	return e.key, sn
}

func mediaType(mt string) string {
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = mt[:i]
	}
	return strings.ToLower(strings.TrimSpace(mt))
}

func jsonRank(mt string) int {
	m := mediaType(mt)
	switch {
	case m == "application/json":
		return 0
	case strings.HasSuffix(m, "+json"):
		return 1
	case m == "text/json" || strings.HasSuffix(m, "/json"):
		return 2
	case m == "*/*" || m == "application/*":
		return 3
	}
	return 100
}

func isJSON(mt string) bool { return jsonRank(mt) < 100 }

// SortedSchemes returns security scheme names in a stable order.
func (d *Doc) SortedSchemes() []string {
	names := make([]string, 0, len(d.SecuritySchemes))
	for n := range d.SecuritySchemes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// EffectiveSecurity is the requirement list that applies to op.
func (d *Doc) EffectiveSecurity(op *Operation) []Requirement {
	if op.SecuritySet {
		return op.Security
	}
	return d.Security
}
