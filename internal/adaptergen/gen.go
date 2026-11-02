// Package adaptergen turns an OpenAPI description into a draft Muretto
// adapter (adapters/WRITING.md): read-only list endpoints, accounts,
// kinds with parent/child fan-out, field mappings, secret stripping,
// pagination, printed remove commands, curl creation signatures and
// candidate exposure rules.
//
// Everything it writes comes from the spec; what a spec cannot say (CLI
// output, MCP tool names, what "public" means) is left out or marked REVIEW.
// The output is validated with the real adapter engine before it is returned.
package adaptergen

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/openapi"
)

// Options tune generation. The zero value is valid.
type Options struct {
	Provider    string   // slug; default from info.title or the host
	DisplayName string   // default from info.title
	BaseURL     string   // https://host[/base/path]; default: the spec's first https server
	Kinds       []string // generate only these kinds (and their parents); default: all
	MaxDepth    int      // nesting levels below top-level kinds; default 2
	// Accounts is the path of the accounts endpoint (an org/team list or a
	// current-user object), or "none" for one implicit account per token.
	// Default: detected.
	Accounts          string
	MCPServer         string // MCP server name whose tools are named after operationIds
	NoRules           bool   // do not suggest exposure rules
	IncludeDeprecated bool
	SpecName          string // shown in the header comment
}

// Result is a generated adapter.
type Result struct {
	Provider string
	YAML     []byte
	Report   *Report
	g        *gen // for Fixtures
}

// Report explains what was generated and what was not.
type Report struct {
	Spec       string
	Provider   string
	BaseURL    string
	Auth       string
	Accounts   string
	Kinds      []KindInfo // every candidate list endpoint, selected or not
	Skipped    []Skipped
	Signatures []string
	Rules      []string
	Review     []string // lines a human must check
	Warnings   []string // spec reading problems
}

// KindInfo is one candidate kind.
type KindInfo struct {
	Kind       string
	Path       string
	Parent     string
	Paging     string // "cursor", "single page", or why pages are not followed
	Attributes int
	Remove     bool
	Create     bool
	Selected   bool
	Note       string // why it is not selected by default
}

// Skipped is a list endpoint that could not become a kind.
type Skipped struct {
	Path   string
	Reason string
}

type opInfo struct {
	op   *openapi.Operation
	path string   // full request path (base path + spec path), as sent
	segs []string // path segments
	norm string
}

type col struct {
	*opInfo
	items   []string // items paths
	resp    *openapi.Schema
	item    *openapi.Schema // the resource schema (inside an envelope, if any)
	wrap    string          // envelope prefix, e.g. "service."
	base    string          // base kind name from the path
	kind    string
	parent  *col
	viaQ    string // query param that names the parent (instead of the path)
	owners  []any  // per path param: *col, acctOwner, or literal string
	depth   int
	hints   []string
	idField string
	hard    string // cannot be generated
	soft    string // not selected unless asked for
	order   int
	sel     bool
	paging  paging
	isAcct  bool
	// filled at emit time (prepare)
	idAttr     string
	ancestors  []ancestorAttr
	nameF      string
	statusF    string
	urlF       string
	createdF   string
	createdFmt string
	attrs      []attr // exactly the attributes the kind emits
}

type acctOwner struct{}

type ancestorAttr struct {
	of   *col
	name string
	tmpl string
}

type account struct {
	op       *opInfo
	col      *col // when the accounts endpoint is a list
	single   bool
	kind     string // singular name (organization, team, ...)
	idF      string
	nameF    string
	slugF    string
	items    []string
	none     bool   // implicit account
	ask      string // the user types the account id (an org slug ...)
	askParam string // snake name of the parameter the typed id fills
	orgish   bool   // a list of orgs/teams: account-named params refer to it
}

type gen struct {
	doc     *openapi.Doc
	opt     Options
	rep     *Report
	slug    string
	display string
	host    string
	base    string
	authH   string
	authS   string
	authNm  string
	ops     []*opInfo
	cols    []*col
	byNorm  map[string]*col
	acct    *account
	env     string
	sigs    []sigInfo
}

// sigInfo is what Fixtures needs to write an extract case for a signature.
type sigInfo struct {
	id, kind     string
	post         *opInfo
	idPath       string   // where the create response has the id (result.id)
	errKeys      []string // result_lacks
	tools        []string // MCP tool names (MCP signatures only)
	param        string   // MCP input naming the container
	col          *col
	capture      int    // path param position the container is read from, or -1
	resultParent string // create-response field naming the parent, or ""
}

// Generate builds a draft adapter from doc.
func Generate(doc *openapi.Doc, opt Options) (*Result, error) {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 2
	}
	g := &gen{doc: doc, opt: opt, rep: &Report{Warnings: append([]string(nil), doc.Warnings...)}, byNorm: map[string]*col{}}
	g.rep.Spec = fmt.Sprintf("%s %s", specKind(doc), doc.Version)
	if doc.Title != "" {
		g.rep.Spec += ": " + doc.Title
		if doc.APIVersion != "" {
			g.rep.Spec += " " + doc.APIVersion
		}
	}
	if err := g.pickBase(); err != nil {
		return nil, err
	}
	g.slug = opt.Provider
	if g.slug == "" {
		g.slug = providerSlug(doc.Title, g.host)
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`).MatchString(g.slug) {
		return nil, fmt.Errorf("cannot derive a provider name from the spec title %q; pass --provider (lowercase letters, digits and dashes, 2-32 chars)", doc.Title)
	}
	g.display = opt.DisplayName
	if g.display == "" {
		g.display = displayName(doc.Title, g.slug)
	}
	g.env = envName(g.slug)
	g.rep.Provider = g.slug
	if err := g.pickAuth(); err != nil {
		return nil, err
	}
	g.collect()
	if err := g.pickAccounts(); err != nil {
		return nil, err
	}
	g.resolveParents()
	g.chooseIDs()
	g.nameKinds()
	if err := g.selectKinds(); err != nil {
		return nil, err
	}
	sel := g.selected()
	if len(sel) == 0 {
		return &Result{Provider: g.slug, Report: g.rep}, errors.New("no list endpoints could be turned into kinds (see the skipped list)")
	}
	data, err := g.emit(sel)
	if err != nil {
		return nil, err
	}
	if err := adapters.Validate(data, g.slug+".yaml"); err != nil {
		return &Result{Provider: g.slug, YAML: data, Report: g.rep}, fmt.Errorf("generated adapter does not validate (a generator bug; please report it): %w", err)
	}
	return &Result{Provider: g.slug, YAML: data, Report: g.rep, g: g}, nil
}

func specKind(d *openapi.Doc) string {
	if d.Major == 2 {
		return "Swagger"
	}
	return "OpenAPI"
}

func (g *gen) review(format string, args ...any) {
	g.rep.Review = append(g.rep.Review, fmt.Sprintf(format, args...))
}

func (g *gen) reviewOnce(format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	for _, r := range g.rep.Review {
		if r == m {
			return
		}
	}
	g.rep.Review = append(g.rep.Review, m)
}

func (g *gen) skip(path, format string, args ...any) {
	g.rep.Skipped = append(g.rep.Skipped, Skipped{Path: path, Reason: fmt.Sprintf(format, args...)})
}

// ---- base URL ----

func (g *gen) pickBase() error {
	parse := func(raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		return u, nil
	}
	setHost := func(u *url.URL, from string) error {
		if u.Scheme != "https" {
			return fmt.Errorf("%s %q is not https; Muretto only calls https APIs (pass --base-url https://...)", from, u.String())
		}
		switch u.Port() {
		case "", "443":
		default:
			return fmt.Errorf("%s %q has a port; the broker only calls the default https port (pass --base-url https://host)", from, u.String())
		}
		h, err := broker.CanonicalHost(u.Hostname())
		if err != nil {
			return fmt.Errorf("%s host %q: %w", from, u.Host, err)
		}
		if strings.Contains(u.Path, "{") {
			return fmt.Errorf("%s %q has a server variable with no default; pass --base-url", from, u.String())
		}
		g.host = h
		g.base = strings.TrimSuffix(u.Path, "/")
		return nil
	}
	relative := ""
	for _, s := range g.doc.Servers {
		if strings.HasPrefix(s.URL, "/") {
			relative = strings.TrimSuffix(s.URL, "/")
			break
		}
	}
	if g.opt.BaseURL != "" {
		u, err := parse(g.opt.BaseURL)
		if err != nil || u.Host == "" {
			return fmt.Errorf("--base-url %q is not an absolute URL", g.opt.BaseURL)
		}
		if err := setHost(u, "--base-url"); err != nil {
			return err
		}
		if g.base == "" && relative != "" {
			g.base = relative
		}
		g.rep.BaseURL = "https://" + g.host + g.base
		return nil
	}
	var firstErr error
	for _, s := range g.doc.Servers {
		u, err := parse(s.URL)
		if err != nil || u.Host == "" {
			continue
		}
		if err := setHost(u, "server"); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		g.rep.BaseURL = "https://" + g.host + g.base
		if len(g.doc.Servers) > 1 {
			g.review("the spec lists %d servers; using %s (pass --base-url for another)", len(g.doc.Servers), g.rep.BaseURL)
		}
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("the spec has no absolute https server URL; pass --base-url https://api.example.com")
}

// fullPath is the request path for op, or false when op is served from
// another host.
func (g *gen) fullPath(op *openapi.Operation) (string, bool) {
	prefix := g.base
	if len(op.Servers) > 0 {
		found := false
		for _, s := range op.Servers {
			u, err := url.Parse(s.URL)
			if err != nil {
				continue
			}
			if u.Host == "" && strings.HasPrefix(s.URL, "/") {
				prefix, found = strings.TrimSuffix(s.URL, "/"), true
				break
			}
			if h, err := broker.CanonicalHost(u.Host); err == nil && h == g.host && u.Scheme == "https" {
				prefix, found = strings.TrimSuffix(u.Path, "/"), true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	return prefix + op.Path, true
}

// ---- auth ----

func supported(s *openapi.SecurityScheme) (header, scheme string, rank int, ok bool) {
	switch {
	case s.Type == "http" && s.Scheme == "bearer":
		return "Authorization", "Bearer", 0, true
	case s.Type == "oauth2", s.Type == "openIdConnect":
		return "Authorization", "Bearer", 1, true
	case s.Type == "apiKey" && s.In == "header" && s.ParamName != "":
		scheme := "Bearer"
		if strings.EqualFold(s.ParamName, "Authorization") {
			// Only an explicit "<Scheme> <token>" form names the prefix;
			// prose like "Token generated in Settings" does not.
			if m := regexp.MustCompile(`\b(Bearer|Token|Api-Key|ApiKey|Key)\s+[<{$]`).FindStringSubmatch(s.Description); m != nil {
				scheme = m[1]
			}
		} else {
			scheme = ""
		}
		return s.ParamName, scheme, 2, true
	case s.Type == "http" && s.Scheme == "basic":
		return "Authorization", "Basic", 3, true
	}
	return "", "", 0, false
}

func (g *gen) pickAuth() error {
	counts := map[string]int{}
	for _, op := range g.doc.Operations {
		if !strings.EqualFold(op.Method, http.MethodGet) {
			continue
		}
		for _, req := range g.doc.EffectiveSecurity(op) {
			if len(req) == 1 {
				for name := range req {
					counts[name]++
				}
			}
		}
	}
	best, bestCount, bestRank := "", -1, 99
	var unsupported []string
	for _, name := range g.doc.SortedSchemes() {
		s := g.doc.SecuritySchemes[name]
		_, _, rank, ok := supported(s)
		if !ok {
			unsupported = append(unsupported, fmt.Sprintf("%s (%s %s%s)", name, s.Type, s.In, s.Scheme))
			continue
		}
		c := counts[name]
		if c > bestCount || c == bestCount && rank < bestRank {
			best, bestCount, bestRank = name, c, rank
		}
	}
	if best == "" {
		if len(unsupported) > 0 {
			return fmt.Errorf("no supported auth scheme: %s. Muretto sends the token in a request header (bearer, API key header, OAuth access token or basic); tokens in query strings or cookies are not supported", strings.Join(unsupported, ", "))
		}
		g.authH, g.authS = "Authorization", "Bearer"
		g.rep.Auth = authText("Authorization", "Bearer") + " (assumed: the spec declares no security)"
		g.review("the spec declares no security scheme; the adapter assumes a bearer token in Authorization")
		return nil
	}
	s := g.doc.SecuritySchemes[best]
	g.authH, g.authS, _, _ = supported(s)
	g.authNm = best
	g.rep.Auth = fmt.Sprintf("%s (security scheme %q)", authText(g.authH, g.authS), best)
	switch {
	case s.Type == "oauth2" || s.Type == "openIdConnect":
		g.review("auth is %s: the token must be an OAuth access token, sent as a bearer token", s.Type)
	case s.Type == "http" && s.Scheme == "basic":
		g.review("auth is HTTP basic: the stored token must be base64(username:password)")
	case s.Type == "apiKey" && strings.EqualFold(s.ParamName, "Authorization"):
		g.review("auth is an API key in the Authorization header; check the prefix (api.auth.scheme: %q)", g.authS)
	}
	return nil
}

// authOK reports whether op can be called with the chosen scheme.
func (g *gen) authOK(op *openapi.Operation) bool {
	reqs := g.doc.EffectiveSecurity(op)
	if len(reqs) == 0 || g.authNm == "" {
		return true
	}
	for _, r := range reqs {
		if len(r) == 0 {
			return true
		}
		if _, ok := r[g.authNm]; ok && len(r) == 1 {
			return true
		}
	}
	return false
}

// ---- collect list endpoints ----

// Endpoints that may return secret values or are not resources: never listed.
var reSecretSeg = regexp.MustCompile(`^(?:secrets?|passwords?|credentials?|keys|api_?keys?|tokens?|access_?tokens?|private_?keys?|ssh_?keys?|gpg_?keys?|signing_?keys?|certificates?|certs|decrypt\w*|reveal\w*|plaintext|password|key|env_?values?|connection_?strings?|kubeconfig|config_?values?)$`)
var reSecretOpID = regexp.MustCompile(`(?i)secret|password|credential|decrypt|reveal|plaintext|private.?key|api.?key|token|kubeconfig|connection.?string`)
var reNotResource = regexp.MustCompile(`^(?:logs?|events?|audit_?logs?|audit|metrics?|stats|statistics|usage|analytics|history|activity|activities|timeline|traces?|search|insights?|reports?|health|status|ping|version|openapi|schema|docs)$`)

func (g *gen) collect() {
	n := 0
	for _, op := range g.doc.Operations {
		full, ok := g.fullPath(op)
		if !ok {
			if strings.EqualFold(op.Method, http.MethodGet) {
				g.skip(op.Path, "served from another host")
			}
			continue
		}
		oi := &opInfo{op: op, path: full, segs: segments(full)}
		oi.norm = normKey(oi.segs)
		g.ops = append(g.ops, oi)
		if !strings.EqualFold(op.Method, http.MethodGet) {
			continue
		}
		resp := op.Success()
		if resp == nil || !resp.JSON || resp.Schema == nil {
			continue
		}
		last := ""
		for _, s := range oi.segs {
			if !isParam(s) && !isVersionSeg(s) {
				last = s
			}
		}
		items, item := listItems(resp.Schema, last)
		if items == nil {
			continue // a single-object endpoint
		}
		c := &col{opInfo: oi, items: items, resp: resp.Schema, item: item, order: n}
		n++
		c.base = singular(ident(last))
		if last == "" {
			c.base = "item"
		}
		c.wrap, c.item = unwrapEnvelope(item, c.base)
		for _, s := range oi.segs {
			if strings.ContainsAny(s, "{}") && !isParam(s) {
				c.hard = "a path segment mixes text and a parameter (" + s + "), which the broker cannot match"
			}
		}
		lastSeg := snake(last)
		switch {
		case c.hard != "":
		case op.Deprecated && !g.opt.IncludeDeprecated:
			c.hard = "deprecated"
		case !g.authOK(op):
			c.hard = "needs a different auth scheme"
		case reSecretSeg.MatchString(lastSeg) || reSecretOpID.MatchString(op.ID):
			c.hard = "may return secret values"
		case broker.DeniedPattern(g.host, full) != "":
			c.hard = "the broker denies it in code (" + broker.DeniedPattern(g.host, full) + "): it can return secrets"
		case reNotResource.MatchString(lastSeg):
			c.soft = "looks like events or metrics, not resources"
		}
		for _, p := range op.Params {
			if (p.In == "header" || p.In == "cookie" || p.In == "querystring") && p.Required && c.hard == "" {
				c.hard = fmt.Sprintf("needs the %s parameter %s, which adapters cannot send", p.In, p.Name)
			}
		}
		g.cols = append(g.cols, c)
	}
	// One collection per normalised path: the newest version wins.
	groups := map[string][]*col{}
	for _, c := range g.cols {
		groups[c.norm] = append(groups[c.norm], c)
	}
	for norm, cs := range groups {
		sort.SliceStable(cs, func(i, j int) bool {
			a, b := cs[i], cs[j]
			if (a.hard == "") != (b.hard == "") {
				return a.hard == ""
			}
			if va, vb := versionScore(a.segs), versionScore(b.segs); va != vb {
				return va > vb
			}
			return len(a.item.Properties) > len(b.item.Properties)
		})
		g.byNorm[norm] = cs[0]
		for _, c := range cs[1:] {
			if c.hard == "" {
				c.hard = "older version of " + cs[0].path
			}
		}
	}
}

// ---- accounts ----

var acctNames = []string{"organizations", "organisations", "orgs", "teams", "workspaces", "accounts", "tenants"}
var meNorms = []string{"/me", "/user", "/account", "/whoami", "/users/me", "/self", "/viewer", "/profile", "/auth/user", "/current_user", "/currentuser", "/users/current", "/identity", "/user/me"}
var reAcctParam = regexp.MustCompile(`^(?:organization|organisation|org|team|workspace|account|tenant|owner)(?:_?(?:id|slug|name|ref|id_or_slug|slug_or_id|id_or_name))?$`)

func (g *gen) pickAccounts() error {
	auto := false
	a := &account{}
	g.acct = a
	switch {
	case g.opt.Accounts == "none":
		a.none = true
	case g.opt.Accounts != "":
		want := strings.TrimSuffix(g.opt.Accounts, "/")
		for _, oi := range g.ops {
			if strings.EqualFold(oi.op.Method, http.MethodGet) && (strings.TrimSuffix(oi.path, "/") == want || strings.TrimSuffix(oi.op.Path, "/") == want) {
				a.op = oi
			}
		}
		if a.op == nil {
			return fmt.Errorf("--accounts %s: no GET operation with that path", g.opt.Accounts)
		}
		if c := g.colFor(a.op); c != nil {
			a.col = c
		} else {
			a.single = true
		}
	default:
		// The org/team list the most operations refer to: by a path
		// parameter right after it, or a query parameter named after it
		// (Vercel's teamId).
		best, bestScore := (*col)(nil), -1
		for _, name := range acctNames {
			c := g.byNorm["/"+name]
			if c == nil || c.hard != "" {
				continue
			}
			score := 0
			for _, oi := range g.ops {
				for i, s := range oi.segs {
					if isParam(s) && normKey(oi.segs[:i]) == c.norm {
						score++
					}
				}
				for _, p := range oi.op.Params {
					sn := snake(p.Name)
					if p.In == "query" && (sn == c.base || strings.HasPrefix(sn, c.base+"_") || c.base == "organization" && strings.HasPrefix(sn, "org_")) {
						score++
					}
				}
			}
			if score > bestScore {
				best, bestScore = c, score
			}
		}
		if bestScore < 1 {
			best = nil // nothing refers to it: likely every org on the service
		}
		if best != nil {
			a.col, a.op, a.orgish = best, best.opInfo, true
		}
		if a.op == nil {
			auto = true
			for _, n := range meNorms {
				for _, oi := range g.ops {
					if strings.EqualFold(oi.op.Method, http.MethodGet) && oi.norm == n && g.colFor(oi) == nil && g.authOK(oi.op) {
						if r := oi.op.Success(); r != nil && r.JSON && r.Schema != nil && len(r.Schema.Properties) > 0 {
							a.op, a.single = oi, true
							break
						}
					}
				}
				if a.op != nil {
					break
				}
			}
		}
		if a.op == nil {
			a.none = true
		}
	}
	if a.none && g.opt.Accounts != "none" {
		// No way to list accounts, but list calls need one (Fly's required
		// org_slug): the user types it at `muretto init`.
		var names []string
		for _, oi := range g.ops {
			if !strings.EqualFold(oi.op.Method, http.MethodGet) {
				continue
			}
			for _, p := range oi.op.Params {
				if (p.In == "query" || p.In == "path") && p.Required && reAcctParam.MatchString(snake(p.Name)) {
					names = append(names, p.Name)
				}
			}
		}
		if names = mostCommonFirst(names); len(names) > 0 {
			a.none, a.orgish = false, true
			a.askParam = snake(names[0])
			a.ask = fmt.Sprintf("Your %s %s (sent as %s)", g.display, strings.ReplaceAll(snake(names[0]), "_", " "), names[0])
			g.rep.Accounts = "typed by the user at muretto init: " + names[0]
			g.review("no endpoint lists accounts, but list calls need %s: muretto init asks for it", names[0])
			return nil
		}
	}
	if a.none {
		g.rep.Accounts = "one implicit account per token (no org, team or current-user endpoint found)"
		g.review("no org/team list or current-user endpoint: each token is one account. Pass --accounts <path> if the API has one")
		return nil
	}
	var item *openapi.Schema
	if a.col != nil {
		a.col.isAcct = true
		a.items = a.col.items
		item = a.col.item
		a.kind = a.col.base
		if a.col.wrap != "" {
			return fmt.Errorf("accounts endpoint %s wraps its items; pass --accounts with another endpoint or none", a.op.path)
		}
		if a.col.hard != "" {
			return fmt.Errorf("accounts endpoint %s: %s", a.op.path, a.col.hard)
		}
		for _, s := range a.op.segs {
			if isParam(s) {
				return fmt.Errorf("accounts endpoint %s needs path parameters", a.op.path)
			}
		}
		if g.opt.Accounts != "" {
			for _, n := range acctNames {
				if a.op.norm == "/"+n {
					a.orgish = true
				}
			}
		}
	} else {
		a.items = []string{"$"}
		item = a.op.op.Success().Schema
		a.kind = "account"
		if pickID(item, a.kind, nil) == "" {
			// {account: {...}} (DigitalOcean /v2/account)
			for _, k := range item.Order {
				if p := item.Properties[k]; objectLike(p) && pickID(p, a.kind, nil) != "" && reTmplSafe.MatchString(k) {
					a.items, item = []string{k}, p
					break
				}
			}
		}
	}
	// Id hints: params right after the accounts path, and account-named
	// query params (teamId -> id, org_slug -> slug).
	var hints []string
	for _, oi := range g.ops {
		for i, s := range oi.segs {
			if isParam(s) && a.col != nil && normKey(oi.segs[:i]) == a.op.norm {
				hints = append(hints, paramName(s))
			}
		}
		if a.orgish {
			for _, p := range oi.op.Params {
				if p.In == "query" && reAcctParam.MatchString(snake(p.Name)) {
					hints = append(hints, p.Name)
				}
			}
		}
	}
	hints = mostCommonFirst(hints)
	a.idF = pickID(item, a.kind, hints)
	if a.idF == "" && auto && a.single {
		g.acct = &account{none: true}
		g.rep.Accounts = "one implicit account per token (" + a.op.path + " has no id field)"
		g.review("no usable org/team or current-user endpoint: each token is one account. Pass --accounts <path> if the API has one")
		return nil
	}
	if a.idF == "" {
		return fmt.Errorf("accounts endpoint %s: items have no id field; pass --accounts with another endpoint or none", a.op.path)
	}
	a.nameF = pickName(item)
	if k, p := prop(item, "slug"); k != "" && p.Is("string") {
		a.slugF = k
	}
	g.rep.Accounts = fmt.Sprintf("GET %s (id: %s)", a.op.path, a.idF)
	if a.single {
		g.rep.Accounts += ", the token's own account"
	} else {
		g.review("accounts come from GET %s: check it lists only the orgs or teams the token belongs to", a.op.path)
	}
	return nil
}

func mostCommonFirst(xs []string) []string {
	count := map[string]int{}
	var uniq []string
	for _, x := range xs {
		if count[x] == 0 {
			uniq = append(uniq, x)
		}
		count[x]++
	}
	sort.SliceStable(uniq, func(i, j int) bool { return count[uniq[i]] > count[uniq[j]] })
	return uniq
}

func (g *gen) colFor(oi *opInfo) *col {
	for _, c := range g.cols {
		if c.opInfo == oi {
			return c
		}
	}
	return nil
}

// acctParam reports whether a parameter names the account: in ask mode the
// asked parameter only; else an account-shaped name of the account's own
// kind (org_id for /orgs, teamId for /teams, never team_id for /orgs).
func (g *gen) acctParam(name string) bool {
	if g.acct == nil || !g.acct.orgish {
		return false
	}
	sn := snake(name)
	if g.acct.askParam != "" {
		return sn == g.acct.askParam
	}
	if !reAcctParam.MatchString(sn) {
		return false
	}
	words := []string{g.acct.kind}
	switch g.acct.kind {
	case "org", "organization", "organisation":
		words = []string{"org", "organization", "organisation"}
	}
	for _, w := range words {
		if sn == w || strings.HasPrefix(sn, w+"_") {
			return true
		}
	}
	return false
}

// ---- parents ----

// literal is a parameter's constant value: its default, or its only enum
// value. Several allowed values are a choice, not a constant.
func literal(p *openapi.Param) (string, bool) {
	if p == nil || p.Schema == nil {
		return "", false
	}
	if d, ok := p.Schema.Default.(string); ok && d != "" {
		return d, true
	}
	if len(p.Schema.Enum) == 1 {
		return p.Schema.Enum[0], true
	}
	return "", false
}

// versionLiteral picks the stable value of an enum of API versions (Linode's
// {apiVersion}: v4, v4beta), which is the one sensible choice.
func versionLiteral(p *openapi.Param) (string, bool) {
	if p == nil || p.Schema == nil || len(p.Schema.Enum) < 2 {
		return "", false
	}
	best := ""
	for _, v := range p.Schema.Enum {
		if !isVersionSeg(v) {
			return "", false
		}
		if best == "" && regexp.MustCompile(`^v\d+$`).MatchString(strings.ToLower(v)) {
			best = v
		}
	}
	return best, best != ""
}

func (g *gen) resolveParents() {
	cs := append([]*col(nil), g.cols...)
	sort.SliceStable(cs, func(i, j int) bool { return countParams(cs[i].segs) < countParams(cs[j].segs) })
	for _, c := range cs {
		if c.isAcct || c.hard != "" || g.byNorm[c.norm] != c {
			continue
		}
		var chain []*col
		for i, s := range c.segs {
			if !isParam(s) {
				continue
			}
			name := paramName(s)
			prefix := normKey(c.segs[:i])
			p := c.op.Param("path", name)
			switch owner := g.byNorm[prefix]; {
			case g.acct.col != nil && prefix == g.acct.op.norm:
				c.owners = append(c.owners, acctOwner{})
			case owner != nil && owner != c && !owner.isAcct && owner.hard == "":
				c.owners = append(c.owners, owner)
				chain = append(chain, owner)
				owner.hints = append(owner.hints, name)
			default:
				if v, ok := literal(p); ok {
					c.owners = append(c.owners, v)
				} else if v, ok := versionLiteral(p); ok {
					c.owners = append(c.owners, v)
					g.reviewOnce("path parameter {%s} is fixed to %s, the stable one of its versions (%s)", name, v, strings.Join(p.Schema.Enum, ", "))
				} else if p != nil && p.Schema != nil && len(p.Schema.Enum) > 1 {
					c.hard = fmt.Sprintf("path parameter {%s} has several allowed values (%s) and no default", name, strings.Join(p.Schema.Enum, ", "))
				} else if g.acctParam(name) {
					c.owners = append(c.owners, acctOwner{})
				} else if owner != nil && owner.hard != "" {
					c.hard = fmt.Sprintf("its parent %s is skipped (%s)", owner.path, owner.hard)
				} else {
					c.hard = fmt.Sprintf("path parameter {%s} has no list endpoint to fill it from", name)
				}
			}
			if c.hard != "" {
				break
			}
		}
		if c.hard != "" {
			continue
		}
		if len(chain) > 0 {
			c.parent = chain[len(chain)-1]
			want := c.parent
			for j := len(chain) - 2; j >= 0; j-- {
				want = want.parent
				if want != chain[j] {
					c.hard = "its path parameters do not follow one parent chain"
					break
				}
			}
			if c.hard == "" && c.parent.hard != "" {
				c.hard = "its parent is skipped: " + c.parent.hard
			}
		}
		// Required query parameters: the account, a constant, or a parent.
		for _, p := range c.op.Params {
			if p.In != "query" || !p.Required || c.hard != "" {
				continue
			}
			if _, ok := literal(p); ok || g.acctParam(p.Name) {
				continue
			}
			if c.parent == nil {
				if par := g.queryParent(p.Name, c); par != nil {
					c.parent, c.viaQ = par, p.Name
					par.hints = append(par.hints, p.Name)
					continue
				}
			}
			if p.Schema != nil && len(p.Schema.Enum) > 1 {
				c.hard = fmt.Sprintf("needs the query parameter %s, which has several allowed values (%s) and no default", p.Name, strings.Join(p.Schema.Enum, ", "))
				continue
			}
			c.hard = fmt.Sprintf("needs the query parameter %s, which has no default", p.Name)
		}
		if c.hard != "" {
			continue
		}
		if c.parent != nil {
			c.depth = c.parent.depth + 1
		}
		if c.depth > g.opt.MaxDepth && c.soft == "" {
			c.soft = fmt.Sprintf("nested %d levels deep (more than --max-depth %d)", c.depth, g.opt.MaxDepth)
		}
	}
	// Item operations (GET/DELETE/PATCH /x/{param}) name the id field too.
	for _, oi := range g.ops {
		if len(oi.segs) == 0 || !isParam(oi.segs[len(oi.segs)-1]) {
			continue
		}
		if c := g.byNorm[normKey(oi.segs[:len(oi.segs)-1])]; c != nil {
			c.hints = append(c.hints, paramName(oi.segs[len(oi.segs)-1]))
		}
	}
}

func countParams(segs []string) int {
	n := 0
	for _, s := range segs {
		if isParam(s) {
			n++
		}
	}
	return n
}

// queryParent finds a top-level collection a required query parameter
// names (projectId -> project).
func (g *gen) queryParent(name string, c *col) *col {
	sn := snake(name)
	var best *col
	for _, k := range g.cols {
		if k == c || k.hard != "" || k.isAcct || countParams(k.segs) > 0 || g.byNorm[k.norm] != k {
			continue
		}
		if sn == k.base || strings.HasPrefix(sn, k.base+"_") {
			if best == nil || len(k.base) > len(best.base) {
				best = k
			}
		}
	}
	return best
}

func (g *gen) chooseIDs() {
	for _, c := range g.cols {
		if c.hard != "" || c.isAcct {
			continue
		}
		var avoid []string
		for _, s := range c.segs {
			if isParam(s) {
				avoid = append(avoid, paramName(s))
			}
		}
		for p := c.parent; p != nil; p = p.parent {
			avoid = append(avoid, p.base+"_id", p.kind+"_id")
		}
		if c.viaQ != "" {
			avoid = append(avoid, c.viaQ)
		}
		if g.acct != nil && g.acct.kind != "" {
			avoid = append(avoid, g.acct.kind+"_id")
		}
		c.idField = pickID(c.item, c.base, mostCommonFirst(c.hints), avoid...)
		if c.idField == "" {
			c.hard = "its items have no id-like field"
		}
	}
	// Children of skipped parents are skipped too (parents come first).
	for changed := true; changed; {
		changed = false
		for _, c := range g.cols {
			if c.hard == "" && c.parent != nil && c.parent.hard != "" {
				c.hard = "its parent is skipped: " + c.parent.hard
				changed = true
			}
		}
	}
}

var genericSegs = map[string]bool{"list": true, "all": true, "index": true, "items": true}

func (g *gen) nameKinds() {
	live := []*col{}
	for _, c := range g.cols {
		if c.hard == "" && !c.isAcct {
			live = append(live, c)
			c.kind = c.base
			if genericSegs[c.base] {
				// /projects/list -> project
				segs := c.segs
				for i := len(segs) - 2; i >= 0; i-- {
					if !isParam(segs[i]) && !isVersionSeg(segs[i]) {
						c.kind = singular(ident(segs[i]))
						break
					}
				}
			}
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].depth < live[j].depth })
	// The same items listed two ways (/projects and /orgs/{org}/projects):
	// keep the one other kinds hang off, else the account-scoped one.
	hasChildren := map[*col]bool{}
	for _, c := range g.cols {
		if c.parent != nil {
			hasChildren[c.parent] = true
		}
	}
	scoped := func(c *col) bool {
		for _, o := range c.owners {
			if _, ok := o.(acctOwner); ok {
				return true
			}
		}
		return g.acctQuery(c.op) != ""
	}
	for i, a := range live {
		for _, b := range live[i+1:] {
			if a.kind != b.kind || a.soft != "" || b.soft != "" || a.parent != b.parent || !sameSchema(a.item, b.item) {
				continue
			}
			keep, drop := a, b
			switch {
			case hasChildren[b] && !hasChildren[a]:
				keep, drop = b, a
			case hasChildren[a] && !hasChildren[b]:
			case scoped(b) && !scoped(a):
				keep, drop = b, a
			}
			drop.soft = "lists the same items as " + keep.path
		}
	}
	// Who keeps a contested name: a kind others hang off, then a selected
	// one, then the shortest path. The rest get a prefix from their parent,
	// the account, or the path segment before the name.
	prio := func(c *col) int {
		p := 0
		if hasChildren[c] {
			p += 100
		}
		if c.soft == "" {
			p += 50
		}
		return p - len(c.segs)
	}
	for round := 0; round < 4; round++ {
		byName := map[string][]*col{}
		var names []string
		for _, c := range live {
			if byName[c.kind] == nil {
				names = append(names, c.kind)
			}
			byName[c.kind] = append(byName[c.kind], c)
		}
		clash := false
		for _, name := range names {
			cs := byName[name]
			if len(cs) < 2 {
				continue
			}
			clash = true
			sort.SliceStable(cs, func(i, j int) bool { return prio(cs[i]) > prio(cs[j]) })
			for _, c := range cs[1:] {
				c.kind = g.prefixed(c, round)
			}
		}
		if !clash {
			break
		}
	}
}

// resourceLike reports whether the spec can create or delete the items of
// c: Muretto tracks things agents create, so read-only lists (advisors,
// entitlements, usage) are not kinds unless asked for.
func (g *gen) resourceLike(c *col) bool {
	if g.findOp("post", c.norm) != nil || g.findOp("delete", c.norm+"/{}") != nil {
		return true
	}
	// Exposure-relevant lists stay even when read-only (Supabase buckets
	// have a public flag but no create endpoint in the Management API).
	for _, a := range pickAttrs(c.item, map[string]bool{c.idField: true}) {
		if exposureField(a) {
			return true
		}
	}
	return false
}

func (g *gen) selectKinds() error {
	want := map[string]bool{}
	for _, k := range g.opt.Kinds {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	found := map[string]bool{}
	for _, c := range g.cols {
		if c.hard != "" || c.isAcct {
			continue
		}
		if c.soft == "" && !g.resourceLike(c) {
			c.soft = "read-only list: the spec has no create or delete operation for it"
		}
		if len(want) == 0 {
			c.sel = c.soft == ""
		} else if want[c.kind] {
			c.sel = true
			found[c.kind] = true
		}
	}
	for k := range want {
		if !found[k] {
			var names []string
			for _, c := range g.cols {
				if c.hard == "" && !c.isAcct {
					names = append(names, c.kind)
				}
			}
			sort.Strings(names)
			return fmt.Errorf("unknown kind %q (candidates: %s)", k, strings.Join(names, ", "))
		}
	}
	// Parents of selected kinds are needed for the fan-out.
	for _, c := range g.cols {
		if c.sel {
			for p := c.parent; p != nil; p = p.parent {
				p.sel = true
			}
		}
	}
	for _, c := range g.cols {
		if c.isAcct || g.byNorm[c.norm] != c && c.hard == "" {
			continue
		}
		if c.hard != "" {
			g.skip(c.path, "%s", c.hard)
			continue
		}
		ki := KindInfo{Kind: c.kind, Path: c.path, Selected: c.sel, Note: c.soft}
		if c.parent != nil {
			ki.Parent = c.parent.kind
		}
		g.rep.Kinds = append(g.rep.Kinds, ki)
	}
	return nil
}

// selected returns the selected collections with parents before children.
func (g *gen) selected() []*col {
	var out []*col
	for _, c := range g.cols {
		if c.sel && c.hard == "" && !c.isAcct {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].depth != out[j].depth {
			return out[i].depth < out[j].depth
		}
		return out[i].order < out[j].order
	})
	return out
}

func (g *gen) kindInfo(c *col) *KindInfo {
	for i := range g.rep.Kinds {
		if g.rep.Kinds[i].Kind == c.kind && g.rep.Kinds[i].Path == c.path {
			return &g.rep.Kinds[i]
		}
	}
	return &KindInfo{}
}

// findOp finds an in-scope operation by method and normalised path,
// preferring the newest version.
func (g *gen) findOp(method, norm string) *opInfo {
	var best *opInfo
	for _, oi := range g.ops {
		if oi.op.Method != method || oi.norm != norm || (oi.op.Deprecated && !g.opt.IncludeDeprecated) || !g.authOK(oi.op) {
			continue
		}
		if best == nil || versionScore(oi.segs) > versionScore(best.segs) {
			best = oi
		}
	}
	return best
}

func sameSchema(a, b *openapi.Schema) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b || a.Name != "" && a.Name == b.Name {
		return true
	}
	if len(a.Order) != len(b.Order) || len(a.Order) == 0 {
		return false
	}
	for i := range a.Order {
		if a.Order[i] != b.Order[i] {
			return false
		}
	}
	return true
}

// prefixed renames a kind whose name another kind kept.
func (g *gen) prefixed(c *col, round int) string {
	if round < 3 {
		var prev []string
		seenName := false
		for i := len(c.segs) - 1; i >= 0; i-- {
			s := c.segs[i]
			if isParam(s) || isVersionSeg(s) {
				continue
			}
			if !seenName {
				seenName = true
				continue
			}
			prev = append(prev, singular(ident(s)))
		}
		cands := []string{}
		if c.parent != nil {
			cands = append(cands, c.parent.kind)
		}
		for _, o := range c.owners {
			if _, ok := o.(acctOwner); ok && g.acct != nil && g.acct.kind != "" {
				cands = append(cands, g.acct.kind)
				break
			}
		}
		cands = append(cands, prev...)
		for _, p := range cands {
			if p != "" && !strings.HasPrefix(c.kind, p+"_") && p != c.kind {
				return p + "_" + c.kind
			}
		}
	}
	return fmt.Sprintf("%s_%d", c.kind, c.order+1)
}
