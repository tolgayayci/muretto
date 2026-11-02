package adapters

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/broker"
)

// FormatVersion is the adapter file format this engine reads.
const FormatVersion = 1

// adapterSpec is one adapter file. The format is documented field by field in
// adapters/WRITING.md and adapters/_template/adapter.yaml.
type adapterSpec struct {
	Format      int                      `yaml:"format"`
	Provider    string                   `yaml:"provider"`
	DisplayName string                   `yaml:"display_name"`
	API         apiSpec                  `yaml:"api"`
	MCPServers  []string                 `yaml:"mcp_servers"`
	TokenHelp   string                   `yaml:"token_help"`
	Options     []optionSpec             `yaml:"options"`
	Endpoints   map[string]*endpointSpec `yaml:"endpoints"`
	Accounts    accountsSpec             `yaml:"accounts"`
	AccountSlug tmpl                     `yaml:"account_slug"` // fallback when Credentials.AccountSlug is empty
	Kinds       []*kindSpec              `yaml:"kinds"`
	Rules       []*ruleSpec              `yaml:"rules"`
	Signatures  []*signatureSpec         `yaml:"signatures"`
	Coverage    []coverageSpec           `yaml:"coverage"`
	Recorder    recorderSpec             `yaml:"recorder"`
	source      string                   // local adapter file; "" when built in
}

type apiSpec struct {
	BaseURL string   `yaml:"base_url"`
	Auth    authSpec `yaml:"auth"`
	host    string
}

type authSpec struct {
	Header string `yaml:"header"`
	Scheme string `yaml:"scheme"`
}

type optionSpec struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type endpointSpec struct {
	Method     string          `yaml:"method"`
	Path       string          `yaml:"path"`
	Params     map[string]tmpl `yaml:"params"`
	Query      map[string]tmpl `yaml:"query"`
	Pagination *paginationSpec `yaml:"pagination"`
	Strip      []string        `yaml:"strip"`
	PinnedBody string          `yaml:"pinned_body"`
	OnStatus   map[int]string  `yaml:"on_status"`
	// Headers are constant request headers, e.g. a pinned API version
	// (X-GitHub-Api-Version, Stripe-Version). Never auth or framing headers.
	Headers map[string]string `yaml:"headers"`
	name    string
}

// paginationSpec says how to get the next page. Styles:
//
//	cursor     (default) next: body path of a cursor sent back as param
//	link       follow the RFC 8288 Link header's rel="next" (same host and path);
//	           param names the query parameter the link changes, for the allowlist
//	page       send param = 2, 3, ... while total (body path of the page count)
//	           says there are more, or more (body path of a boolean) is true
//	last_item  send param = field of the last item (items: where they are) while
//	           more is true, or until a page has no items
type paginationSpec struct {
	Style    string     `yaml:"style"`
	Next     string     `yaml:"next"`
	Param    string     `yaml:"param"`
	Total    string     `yaml:"total"`
	More     string     `yaml:"more"`
	Items    stringList `yaml:"items"`
	Field    string     `yaml:"field"`
	Start    int        `yaml:"start"`
	MaxPages int        `yaml:"max_pages"`
}

type accountsSpec struct {
	// Ask, instead of an endpoint, makes `muretto init` ask the user for the
	// account id (an API whose list calls need e.g. an org slug but that has
	// no endpoint to list orgs).
	Ask      string `yaml:"ask"`
	Endpoint string `yaml:"endpoint"`
	// TokenModes names the token's mode from its shape (Stripe live vs test
	// keys reach the same account id): {token_mode} in the id, name and slug
	// templates is the first name whose pattern matches. The token itself
	// never reaches a template.
	TokenModes map[string]*rx `yaml:"token_modes"`
	Items      stringList     `yaml:"items"`
	ID         tmpl           `yaml:"id"`
	Name       tmpl           `yaml:"name"`
	Slug       tmpl           `yaml:"slug"`
}

type kindSpec struct {
	Kind    string `yaml:"kind"`
	ForEach string `yaml:"for_each"`
	// ForEachWhere skips parents for which it is not true (no calls, no
	// checks): e.g. only Vitess databases have deploy requests.
	ForEachWhere *predicate    `yaml:"for_each_where"`
	Sources      []*sourceSpec `yaml:"sources"`
	Enrich       []*enrichSpec `yaml:"enrich"`
	Match        *matchSpec    `yaml:"match"`
	Remove       *fixSpec      `yaml:"remove"`
}

type sourceSpec struct {
	Endpoint      string     `yaml:"endpoint"`
	Items         stringList `yaml:"items"`
	Where         *predicate `yaml:"where"`
	UnavailableIf *predicate `yaml:"unavailable_if"`
	SeenVia       string     `yaml:"seen_via"`
	// IncompleteIf is a body path that, when present and non-empty, means the
	// provider left items out (Neon's unavailable_project_ids): the check fails.
	IncompleteIf   string     `yaml:"incomplete_if"`
	RequiresOption string     `yaml:"requires_option"`
	ChecksRules    []string   `yaml:"checks_rules"`
	ID             tmpl       `yaml:"id"`
	Name           tmpl       `yaml:"name"`
	Status         tmpl       `yaml:"status"`
	URL            tmpl       `yaml:"url"`
	Parent         tmpl       `yaml:"parent"`
	ParentKind     string     `yaml:"parent_kind"`
	CreatedAt      *timeSpec  `yaml:"created_at"`
	Attributes     []attrSpec `yaml:"attributes"`
}

type enrichSpec struct {
	Endpoint       string     `yaml:"endpoint"`
	RequiresOption string     `yaml:"requires_option"`
	Attributes     []attrSpec `yaml:"attributes"`
}

type timeSpec struct {
	Value  tmpl   `yaml:"value"`
	Format string `yaml:"format"` // rfc3339, epoch_ms, epoch_s, auto
}

// attrSpec is one attribute in the explicit allowlist. A bare string copies
// the item field of that name; a mapping renames (field), computes (template)
// or defaults a value.
type attrSpec struct {
	Name     string `yaml:"name"`
	Field    string `yaml:"field"`
	Template tmpl   `yaml:"template"`
	Default  *any   `yaml:"default"`
}

func (a *attrSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		a.Name, a.Field = n.Value, n.Value
		return nil
	}
	if err := strictKeys(n, "name", "field", "template", "default"); err != nil {
		return err
	}
	type plain attrSpec
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*a = attrSpec(p)
	if a.Name == "" {
		return fmt.Errorf("line %d: attribute needs a name", n.Line)
	}
	if a.Field == "" && a.Template.empty() {
		a.Field = a.Name
	}
	if a.Field != "" && !a.Template.empty() {
		return fmt.Errorf("line %d: attribute %q: use field or template, not both", n.Line, a.Name)
	}
	return nil
}

type ruleSpec struct {
	ID          string     `yaml:"id"`
	Kind        string     `yaml:"kind"`
	Severity    string     `yaml:"severity"`
	Signal      string     `yaml:"signal"`
	Title       tmpl       `yaml:"title"`
	Explanation tmpl       `yaml:"explanation"`
	Caveat      string     `yaml:"caveat"`
	Applies     *predicate `yaml:"applies"`
	Exposed     *predicate `yaml:"exposed"`
	NotExposed  *predicate `yaml:"not_exposed"`
	Fix         fixSpec    `yaml:"fix"`
}

type fixSpec struct {
	Kind    string `yaml:"kind"`
	Text    tmpl   `yaml:"text"`
	Note    tmpl   `yaml:"note"`
	DocsURL string `yaml:"docs_url"`
}

type signatureSpec struct {
	ID      string         `yaml:"id"`
	Kind    string         `yaml:"kind"`
	Role    string         `yaml:"role"`
	When    whenSpec       `yaml:"when"`
	Extract []*extractSpec `yaml:"extract"`
}

type whenSpec struct {
	Tool        *rx            `yaml:"tool"`
	MCPServer   stringList     `yaml:"mcp_server"`
	MCPTools    stringList     `yaml:"mcp_tools"`
	Command     []*rx          `yaml:"command"`
	InputMatch  map[string]*rx `yaml:"input_matches"`
	NotCommand  []*rx          `yaml:"not_command"`
	Output      *rx            `yaml:"output"`
	Status      string         `yaml:"status"`
	ResultLacks []string       `yaml:"result_lacks"`
	// ResultHas: the output must parse as a JSON object with at least one of
	// these top-level keys (a positive sign of success, e.g. success/result).
	ResultHas []string `yaml:"result_has"`
}

type extractSpec struct {
	Kind      string         `yaml:"kind"`
	From      string         `yaml:"from"`
	Pattern   *rx            `yaml:"pattern"`
	Each      string         `yaml:"each"`
	Split     *splitSpec     `yaml:"split"`
	Value     tmpl           `yaml:"value"`
	Container tmpl           `yaml:"container"`
	Match     *matchSpec     `yaml:"match"`
	Capture   []*captureSpec `yaml:"capture"`
	// Preprocess rewrites the text the pattern runs on (preprocess.go):
	// shell_args, sql.
	Preprocess stringList `yaml:"preprocess"`
	// ReferencedIf names a pattern group: a match where it is non-empty
	// yields role referenced, whatever the signature's role (e.g. CREATE TABLE
	// IF NOT EXISTS does not prove the table was created).
	ReferencedIf string `yaml:"referenced_if"`
}

// captureSpec adds the named groups of the first match of Pattern in From
// (output, command or input.<path>) to the extract's template context, e.g. a
// --project-ref flag used as the container.
type captureSpec struct {
	From    string `yaml:"from"`
	Pattern *rx    `yaml:"pattern"`
}

type splitSpec struct {
	Field string `yaml:"field"`
	Sep   string `yaml:"sep"`
}

// matchSpec says how an Identifier is compared with a Resource: Value and
// Container are templates over the resource.
type matchSpec struct {
	Value     tmpl `yaml:"value"`
	Container tmpl `yaml:"container"`
}

type coverageSpec struct {
	Kind   string `yaml:"kind"`
	Status string `yaml:"status"`
	Note   string `yaml:"note"`
}

type recorderSpec struct {
	Pseudonymize []string `yaml:"pseudonymize"` // keys ("name") or parent.key ("metadata.name")
	Keep         []string `yaml:"keep"`         // values never replaced (e.g. lint names)
}

// stringList accepts a scalar or a list of scalars.
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = []string{n.Value}
		return nil
	}
	var l []string
	if err := n.Decode(&l); err != nil {
		return err
	}
	*s = l
	return nil
}

// rx is a compiled RE2 regular expression from an adapter file.
type rx struct{ *regexp.Regexp }

func (r *rx) UnmarshalYAML(n *yaml.Node) error {
	re, err := regexp.Compile(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	r.Regexp = re
	return nil
}

func strictKeys(n *yaml.Node, allowed ...string) error {
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i].Value
		ok := false
		for _, a := range allowed {
			ok = ok || a == k
		}
		if !ok {
			return fmt.Errorf("line %d: unknown key %q", n.Content[i].Line, k)
		}
	}
	return nil
}

var (
	reSlug     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	reKindName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reRuleID   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	rePathVar  = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// reservedAttrs are resource fields exposed to templates under these names;
// attributes may not shadow them.
var reHeaderName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// forbiddenHeader lists headers an adapter may never set: credentials,
// framing, method overrides and anything the broker or Go controls.
var forbiddenHeader = map[string]bool{
	"authorization": true, "cookie": true, "host": true, "content-type": true, "content-length": true,
	"transfer-encoding": true, "connection": true, "te": true, "trailer": true, "upgrade": true,
	"x-http-method-override": true, "x-http-method": true, "x-method-override": true,
	"accept-encoding": true, "user-agent": true, "x-api-key": true, "x-auth-token": true, "x-auth-key": true,
	"x-auth-email": true, "forwarded": true, "x-forwarded-for": true, "x-forwarded-host": true,
}

var reservedAttrs = map[string]bool{
	"id": true, "provider_id": true, "name": true, "kind": true, "account_id": true,
	"account_name": true, "url": true, "status": true, "parent_id": true, "parent": true,
	"created_at": true, "account_slug": true,
}

// parseAdapter decodes and validates one adapter file.
func parseAdapter(data []byte, file string) (*adapterSpec, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var a adapterSpec
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if err := a.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &a, nil
}

func (a *adapterSpec) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if a.Format != FormatVersion {
		bad("format must be %d", FormatVersion)
	}
	if !reSlug.MatchString(a.Provider) {
		bad("provider %q must match %s", a.Provider, reSlug)
	}
	if a.DisplayName == "" {
		bad("display_name is required")
	}
	u, err := url.Parse(a.API.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil ||
		u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		bad("api.base_url must be https://host with no path, query or credentials")
	} else if host, err := broker.CanonicalHost(u.Host); err != nil {
		// The broker compares hosts only in canonical form, so a port (even
		// :443), a trailing dot or an IP literal could dodge its deny list.
		bad("api.base_url host must be a plain DNS name (no port, trailing dot or IP): %v", err)
	} else {
		a.API.host = host
	}
	if a.API.Auth.Header == "" {
		bad("api.auth.header is required")
	}
	options := map[string]bool{}
	for _, o := range a.Options {
		options[o.Name] = true
	}
	checkOption := func(where, name string) {
		if name != "" && !options[name] {
			bad("%s: requires_option %q is not declared in options", where, name)
		}
	}

	for name, ep := range a.Endpoints {
		ep.name = name
		if ep.Method == "" {
			ep.Method = "GET"
		}
		where := "endpoint " + name
		switch ep.Method {
		case "GET":
			if ep.PinnedBody != "" {
				bad("%s: pinned_body is only for POST", where)
			}
		case "POST":
			if _, _, ok := broker.PinnedBody(ep.PinnedBody); !ok {
				bad("%s: POST needs pinned_body naming a body declared in Go (internal/broker/pinned.go)", where)
			}
		default:
			bad("%s: method must be GET or POST", where)
		}
		if !strings.HasPrefix(ep.Path, "/") || strings.ContainsAny(ep.Path, "?#") {
			bad("%s: path must start with / and have no query", where)
		}
		for _, m := range rePathVar.FindAllStringSubmatch(ep.Path, -1) {
			if _, ok := ep.Params[m[1]]; !ok {
				bad("%s: path variable {%s} has no params entry", where, m[1])
			}
		}
		for q := range ep.Query {
			if strings.Contains(strings.ToLower(q), "decrypt") {
				bad("%s: query %q is never allowed", where, q)
			}
		}
		if pg := ep.Pagination; pg != nil {
			if pg.Param == "" {
				bad("%s: pagination needs param (the query parameter that carries the next page)", where)
			}
			switch pg.Style {
			case "", "cursor":
				pg.Style = "cursor"
				if pg.Next == "" {
					bad("%s: cursor pagination needs next", where)
				}
			case "link":
			case "page":
				if pg.Total == "" && pg.More == "" && len(pg.Items) == 0 {
					bad("%s: page pagination needs total, more or items", where)
				}
				if pg.Start == 0 {
					pg.Start = 1
				}
			case "last_item":
				if pg.Field == "" {
					bad("%s: last_item pagination needs field", where)
				}
			default:
				bad("%s: pagination style must be cursor, link, page or last_item", where)
			}
			if pg.MaxPages == 0 {
				pg.MaxPages = 50
			}
		}
		for h, v := range ep.Headers {
			if !reHeaderName.MatchString(h) || forbiddenHeader[strings.ToLower(h)] || strings.HasPrefix(strings.ToLower(h), "proxy-") {
				bad("%s: header %q cannot be set by an adapter", where, h)
			}
			if strings.ContainsAny(v, "{}\r\n") || v == "" {
				bad("%s: header %q must be a constant, non-empty value", where, h)
			}
		}
		for code, action := range ep.OnStatus {
			if action != "empty" || code < 400 {
				bad("%s: on_status supports only 4xx/5xx: empty", where)
			}
		}
	}
	checkEndpoint := func(where, name string) {
		if _, ok := a.Endpoints[name]; !ok {
			bad("%s: unknown endpoint %q", where, name)
		}
	}

	for name, re := range a.Accounts.TokenModes {
		if !reRuleID.MatchString(name) || re == nil {
			bad("accounts.token_modes: %q needs a lowercase name and a pattern", name)
		}
	}
	switch {
	case a.Accounts.Ask != "":
		if a.Accounts.Endpoint != "" || !a.Accounts.ID.empty() {
			bad("accounts.ask cannot be combined with an endpoint or id")
		}
	case a.Accounts.ID.empty():
		bad("accounts.id is required")
	case a.Accounts.Endpoint != "":
		checkEndpoint("accounts", a.Accounts.Endpoint)
	default:
		// No accounts endpoint: one implicit account per token, so the id and
		// name are constants.
		for _, t := range []tmpl{a.Accounts.ID, a.Accounts.Name, a.Accounts.Slug} {
			for _, p := range t.parts {
				if p.path != "" {
					bad("accounts without an endpoint need a constant id, name and slug")
				}
			}
		}
	}

	kinds := map[string]*kindSpec{}
	rules := map[string]bool{}
	for _, r := range a.Rules {
		rules[r.ID] = true
	}
	for _, k := range a.Kinds {
		where := "kind " + k.Kind
		if !reKindName.MatchString(k.Kind) {
			bad("%s: kind must match %s", where, reKindName)
		}
		if kinds[k.Kind] != nil {
			bad("%s: duplicate kind", where)
		}
		if k.ForEach != "" && kinds[k.ForEach] == nil {
			bad("%s: for_each %q must name an earlier kind", where, k.ForEach)
		}
		if k.ForEachWhere != nil && k.ForEach == "" {
			bad("%s: for_each_where needs for_each", where)
		}
		kinds[k.Kind] = k
		if len(k.Sources) == 0 {
			bad("%s: at least one source is required", where)
		}
		for _, s := range k.Sources {
			checkEndpoint(where, s.Endpoint)
			checkOption(where, s.RequiresOption)
			if s.ID.empty() {
				bad("%s: source id is required", where)
			}
			if s.SeenVia == "" {
				s.SeenVia = "list"
			}
			if s.SeenVia != "list" && s.SeenVia != "advisor" {
				bad("%s: seen_via must be list or advisor", where)
			}
			if !s.Parent.empty() && s.ParentKind == "" {
				bad("%s: parent needs parent_kind", where)
			}
			if s.CreatedAt != nil {
				switch s.CreatedAt.Format {
				case "rfc3339", "epoch_ms", "epoch_s", "auto":
				default:
					bad("%s: created_at.format must be rfc3339, epoch_ms, epoch_s or auto", where)
				}
			}
			for _, r := range s.ChecksRules {
				if !rules[r] {
					bad("%s: checks_rules names unknown rule %q", where, r)
				}
			}
			checkAttrs(where, s.Attributes, bad)
		}
		for _, e := range k.Enrich {
			checkEndpoint(where+" enrich", e.Endpoint)
			checkOption(where+" enrich", e.RequiresOption)
			checkAttrs(where+" enrich", e.Attributes, bad)
		}
		if k.Remove != nil {
			checkFix(where+" remove", *k.Remove, bad)
		}
	}
	for _, k := range a.Kinds {
		for _, s := range k.Sources {
			if s.ParentKind != "" && kinds[s.ParentKind] == nil {
				bad("kind %s: parent_kind %q is unknown", k.Kind, s.ParentKind)
			}
		}
	}

	for _, r := range a.Rules {
		where := "rule " + r.ID
		if !reRuleID.MatchString(r.ID) {
			bad("%s: id must match %s", where, reRuleID)
		}
		if kinds[r.Kind] == nil {
			bad("%s: unknown kind %q", where, r.Kind)
		}
		switch r.Severity {
		case "critical", "high", "medium", "low", "info":
		default:
			bad("%s: bad severity %q", where, r.Severity)
		}
		switch r.Signal {
		case "official_advisor", "api_field", "catalog_query", "name_pattern":
		default:
			bad("%s: bad signal %q", where, r.Signal)
		}
		if r.Exposed == nil {
			bad("%s: exposed is required", where)
		}
		if r.Title.empty() || r.Explanation.empty() {
			bad("%s: title and explanation are required", where)
		}
		checkFix(where+" fix", r.Fix, bad)
	}

	sigIDs := map[string]bool{}
	for _, s := range a.Signatures {
		where := "signature " + s.ID
		if s.ID == "" || sigIDs[s.ID] {
			bad("%s: id must be set and unique", where)
		}
		sigIDs[s.ID] = true
		if s.Role != "created" && s.Role != "referenced" {
			bad("%s: role must be created or referenced", where)
		}
		if s.When.Tool == nil && len(s.When.MCPTools) == 0 {
			bad("%s: when needs tool or mcp_tools", where)
		}
		if s.When.Status == "" {
			s.When.Status = "ok"
		}
		if s.When.Status != "ok" && s.When.Status != "any" {
			bad("%s: when.status must be ok or any", where)
		}
		if len(s.When.MCPTools) > 0 && len(s.When.MCPServer) == 0 {
			s.When.MCPServer = a.MCPServers
			if len(s.When.MCPServer) == 0 {
				bad("%s: mcp_tools needs mcp_server or a top-level mcp_servers", where)
			}
		}
		if len(s.Extract) == 0 {
			bad("%s: at least one extract is required", where)
		}
		for _, e := range s.Extract {
			if e.Kind == "" {
				e.Kind = s.Kind
			}
			if kinds[e.Kind] == nil {
				bad("%s: unknown kind %q", where, e.Kind)
			}
			for _, c := range e.Capture {
				if c.Pattern == nil || !(c.From == "output" || c.From == "command" || strings.HasPrefix(c.From, "input.")) {
					bad("%s: capture needs from (output, command, input.<path>) and pattern", where)
				}
			}
			if e.Value.empty() {
				bad("%s: extract value is required", where)
			}
			switch {
			case e.From == "output" || e.From == "command" || strings.HasPrefix(e.From, "input."):
				if e.Pattern == nil {
					bad("%s: from %s needs a pattern", where, e.From)
					break
				}
				if err := checkPreprocess(e.Preprocess); err != nil {
					bad("%s: %v", where, err)
				}
				if e.ReferencedIf != "" && e.Pattern.SubexpIndex(e.ReferencedIf) < 0 {
					bad("%s: referenced_if %q is not a group of the pattern", where, e.ReferencedIf)
				}
			case e.From == "result" || e.From == "input":
				if e.Pattern != nil || len(e.Preprocess) > 0 || e.ReferencedIf != "" {
					bad("%s: from %s takes no pattern, preprocess or referenced_if (use templates)", where, e.From)
				}
			default:
				bad("%s: from must be output, command, input.<path>, result or input", where)
			}
		}
	}

	for _, c := range a.Coverage {
		switch c.Status {
		case "full", "partial", "planned":
		default:
			bad("coverage %s: status must be full, partial or planned", c.Kind)
		}
	}
	return errors.Join(errs...)
}

func checkAttrs(where string, attrs []attrSpec, bad func(string, ...any)) {
	seen := map[string]bool{}
	for _, at := range attrs {
		if reservedAttrs[at.Name] {
			bad("%s: attribute name %q is reserved", where, at.Name)
		}
		if broker.AlwaysStripped(at.Field) {
			bad("%s: attribute field %q is always stripped by the broker", where, at.Field)
		}
		if seen[at.Name] {
			bad("%s: duplicate attribute %q", where, at.Name)
		}
		seen[at.Name] = true
	}
}

func checkFix(where string, f fixSpec, bad func(string, ...any)) {
	switch f.Kind {
	case "sql", "command", "steps":
	default:
		bad("%s: kind must be sql, command or steps", where)
	}
	if f.Text.empty() {
		bad("%s: text is required", where)
	}
}
