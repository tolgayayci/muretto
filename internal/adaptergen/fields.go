package adaptergen

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tolgayayci/muretto/internal/openapi"
)

// Secret-looking field names (compared in snake_case). Booleans and numbers
// are never secrets ("has_password: true" is a useful fact, not a secret).
var reSecretName = regexp.MustCompile(`^(?:.*_)?(?:secret|secrets|password|passwd|pwd|passphrase|private_key|privkey|api_key|apikey|access_key|secret_key|client_secret|token|tokens|access_token|refresh_token|id_token|auth_token|bearer|credential|credentials|connection_string|connection_uri|conn_str|connstr|dsn|database_url|db_url|jwt|jwt_secret|signing_key|signing_secret|webhook_secret|encryption_key|session_id|session_token|cookie|otp|totp|mfa_secret|seed|mnemonic|license_key|service_role_key|anon_key|private|key_value|secret_value|plaintext|sas|sas_token|hmac|salt|pem|keypair|ssh_key|recovery_codes?|kubeconfig|kube_config|primary_key|secondary_key|client_key|master_key|auth_key|shared_key|access_secret|secret_access_key|service_account_key|account_key)$`)

// Field names that hold personal data; never copied into attributes.
var rePII = regexp.MustCompile(`(?:^|_)(?:email|emails|e_mail|phone|phone_number|mobile|first_name|last_name|full_name|given_name|family_name|address|street|postal_code|zip|zip_code|birthday|birth_date|dob|ssn|avatar|avatar_url|ip|ip_address|user_agent|latitude|longitude|gravatar)$`)

// Long or free-text fields: not useful as attributes.
var reText = regexp.MustCompile(`^(?:description|body|content|text|readme|notes?|markdown|html|script|code|source|template|message|comment|bio|summary|details|payload|data|metadata|config|configuration|settings|spec|manifest|yaml|json|raw|logs?|output|query|sql)$`)

// Words that make a field likely to matter for exposure rules.
var reSecurity = regexp.MustCompile(`public|private|visib|protect|auth|access|password|sso|trusted|allow|firewall|cors|expos|enabled|ssl|tls|https|encrypt|verif|jwt|secure|restrict|role|permission|polic|acl|shar|internet|ingress|anonymous`)

// Fields that are timestamps, links, paths or hashes: noise as attributes.
var reNoise = regexp.MustCompile(`(?:_at|_on|_time|_date|_timestamp|_url|_uri|_link|_href|_path|_sha|_sha\d+|_hash|_md5|_etag|_checksum|_digest)$|^(?:etag|checksum|sha|hash|digest|href|self)$`)

func isSecretField(name string, s *openapi.Schema) bool {
	if s != nil && s.Ext["x-ms-secret"] == true {
		return true
	}
	if s != nil && (s.Is("boolean") || s.Is("integer") || s.Is("number")) && !s.Is("string") && !s.Is("object") {
		return false
	}
	// An object is walked, not stripped by its name: Render wraps resources
	// as {keyValue: {...}}, and a secret's own fields are caught one by one.
	if s != nil && len(s.Properties) > 0 && !s.WriteOnly {
		return false
	}
	if s != nil && (s.WriteOnly || s.Format == "password") {
		return true
	}
	sn := snake(name)
	return reSecretName.MatchString(sn) || reSecretName.MatchString(singular(sn))
}

// stripPaths lists every secret-looking field in a response schema, in the
// broker's strip syntax (a.b, a[].b, leading [] for a root array).
func stripPaths(s *openapi.Schema) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(s *openapi.Schema, prefix string, depth int, visiting map[*openapi.Schema]bool)
	walk = func(s *openapi.Schema, prefix string, depth int, visiting map[*openapi.Schema]bool) {
		if s == nil || depth > 5 || visiting[s] {
			return
		}
		visiting[s] = true
		defer delete(visiting, s)
		if s.Is("array") || (len(s.Types) == 0 && s.Items != nil) {
			walk(s.Items, prefix+"[]", depth+1, visiting)
		}
		for _, name := range s.Order {
			p := s.Properties[name]
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			if strings.HasPrefix(prefix, "[]") || prefix == "[]" {
				path = prefix + "." + name
			}
			if isSecretField(name, p) {
				if !seen[path] {
					seen[path] = true
					out = append(out, path)
				}
				continue
			}
			walk(p, path, depth+1, visiting)
		}
	}
	walk(s, "", 0, map[*openapi.Schema]bool{})
	sort.Strings(out)
	return out
}

func prop(s *openapi.Schema, names ...string) (string, *openapi.Schema) {
	if s == nil {
		return "", nil
	}
	for _, n := range names {
		if p, ok := s.Properties[n]; ok && p != nil {
			return n, p
		}
	}
	// Case- and spelling-insensitive second pass (createdAt vs created_at).
	for _, n := range names {
		for _, k := range s.Order {
			if snake(k) == snake(n) {
				return k, s.Properties[k]
			}
		}
	}
	return "", nil
}

func idLike(p *openapi.Schema) bool {
	return p != nil && (p.Is("string") || p.Is("integer") || p.Is("number") || len(p.Types) == 0 && len(p.Properties) == 0 && p.Items == nil)
}

// pickID chooses the item field that identifies a resource. hints are path
// parameter names that address one item (e.g. "ref" for /projects/{ref}).
func pickID(item *openapi.Schema, kind string, hints []string, avoid ...string) string {
	for _, h := range hints {
		for _, cand := range paramFieldCandidates(h, kind) {
			if n, p := prop(item, cand); n != "" && idLike(p) && !isSecretField(n, p) {
				return n
			}
		}
	}
	// Real ids first (even when not marked required), then natural keys.
	tiers := [][]string{
		{"id", "uid", "uuid", kind + "_id", snakeToCamel(kind + "_id"), "ref", kind + "_ref"},
		{"slug", "key", "name", "hostname", "domain", "number", kind + "_name", snakeToCamel(kind + "_name")},
	}
	for _, names := range tiers {
		for _, required := range []bool{true, false} {
			for _, n := range names {
				k, p := prop(item, n)
				if k == "" || !idLike(p) || isSecretField(k, p) || (required && !item.Required[k]) || !reTmplSafe.MatchString(k) || ctxKeys[k] {
					continue
				}
				return k
			}
		}
	}
	// Last resort: a required <thing>_id (member lists: user_id). Child ids
	// are prefixed with the parent's, so it only has to be unique there.
	// Never a field naming one of the path's parents (connector_id on
	// /connectors/{connectorId}/projects is the same for every item).
	skip := map[string]bool{}
	for _, a := range avoid {
		skip[snake(a)] = true
	}
	for _, k := range item.Order {
		p := item.Properties[k]
		if item.Required[k] && strings.HasSuffix(snake(k), "_id") && !skip[snake(k)] && idLike(p) && reTmplSafe.MatchString(k) && !ctxKeys[k] {
			return k
		}
	}
	return ""
}

// paramFieldCandidates maps a path parameter name to item fields it may
// name: {ref} -> ref, {project_ref} -> ref, {projectId} -> id, {idOrName} -> id.
func paramFieldCandidates(param, kind string) []string {
	p := snake(param)
	out := []string{param, p}
	for _, pre := range []string{kind + "_", strings.Split(kind, "_")[len(strings.Split(kind, "_"))-1] + "_"} {
		if strings.HasPrefix(p, pre) {
			out = append(out, strings.TrimPrefix(p, pre))
		}
	}
	if strings.HasSuffix(p, "_id") || p == "id" || strings.HasPrefix(p, "id_or") {
		out = append(out, "id")
	}
	return out
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func pickName(item *openapi.Schema) string {
	for _, n := range []string{"name", "title", "display_name", "displayName", "slug", "label", "hostname", "domain", "key", "handle", "login", "username"} {
		if k, p := prop(item, n); k != "" && p.Scalar() && !isSecretField(k, p) && !rePII.MatchString(snake(k)) {
			return k
		}
	}
	return ""
}

func pickStatus(item *openapi.Schema) string {
	k, p := prop(item, "status", "state", "readyState", "ready_state", "phase", "lifecycle_state", "lifecycleState", "health")
	if k != "" && p.Is("string") {
		return k
	}
	return ""
}

func pickURL(item *openapi.Schema) string {
	for _, n := range []string{"html_url", "web_url", "dashboard_url", "console_url", "app_url", "public_url", "url", "link", "href"} {
		k, p := prop(item, n)
		if k == "" || !p.Is("string") {
			continue
		}
		ex, _ := p.Example.(string)
		if p.Format == "uri" || p.Format == "url" || strings.HasPrefix(ex, "https://") {
			return k
		}
	}
	return ""
}

// pickCreated returns the creation-time field and its created_at.format.
func pickCreated(item *openapi.Schema) (string, string) {
	k, p := prop(item, "created_at", "createdAt", "created", "creation_date", "creationDate", "created_on", "createdOn",
		"inserted_at", "insertedAt", "create_time", "createTime", "creation_time", "creationTime", "date_created",
		"dateCreated", "time_created", "timeCreated", "creation_timestamp", "creationTimestamp")
	if k == "" {
		return "", ""
	}
	switch {
	case p.Is("string") && strings.EqualFold(p.Format, "date-time"), p.Is("string") && strings.EqualFold(p.Format, "datetime"):
		return k, "rfc3339"
	case p.Is("string") && p.Format == "date":
		return "", ""
	case p.Is("string"):
		return k, "auto"
	case p.Is("integer") || p.Is("number"):
		// auto reads milliseconds or seconds by magnitude, which is right
		// whatever unit the description claims (often none, or a wrong one).
		return k, "auto"
	}
	return "", ""
}

type attr struct {
	name   string // attribute name
	field  string // dotted source path
	schema *openapi.Schema
	score  int
	order  int
}

// ctxKeys are template names the engine reserves for the scan context, so
// item fields with these names are not reachable in templates.
var ctxKeys = map[string]bool{"parent": true, "account": true, "account_name": true, "account_slug": true}

// reserved attribute names (internal/adapters/spec.go reservedAttrs).
var reservedAttrs = map[string]bool{
	"id": true, "provider_id": true, "name": true, "kind": true, "account_id": true,
	"account_name": true, "url": true, "status": true, "parent_id": true, "parent": true,
	"created_at": true, "account_slug": true,
}

// alwaysStripped mirrors broker.AlwaysStripped: fields the broker drops.
var alwaysStripped = map[string]bool{
	"value": true, "vsmValue": true, "legacyValue": true, "protectionBypass": true, "deployHooks": true,
	"jwt_secret": true, "db_pass": true, "service_role_key": true, "smtp_pass": true, "claimUrl": true,
}

const maxAttrs = 12

// pickAttrs chooses the attribute allowlist: scalar fields first by how
// likely they matter for exposure (booleans, enums, security words), then
// in declaration order. skip lists fields already used as core fields.
func pickAttrs(item *openapi.Schema, skip map[string]bool) []attr {
	var cands []attr
	order := 0
	consider := func(field, name string, p *openapi.Schema, bonus int) {
		order++
		sn := snake(name)
		if skip[field] || p == nil || p.Deprecated || alwaysStripped[name] || ctxKeys[name] {
			return
		}
		if isSecretField(name, p) || rePII.MatchString(sn) || reText.MatchString(sn) || reNoise.MatchString(sn) && !reSecurity.MatchString(sn) {
			return
		}
		score := 0
		switch {
		case p.Is("boolean"):
			score = 4
		case len(p.Enum) > 0:
			score = 4
		case p.Is("string") && (p.Format == "date-time" || p.Format == "date"):
			return
		case p.Is("string"):
			score = 1
		case p.Is("integer") || p.Is("number"):
			score = 1
		case p.Is("array") && p.Items != nil && p.Items.Scalar():
			score = 1
			if len(p.Items.Enum) > 0 {
				score = 3
			}
		default:
			return
		}
		if reSecurity.MatchString(sn) {
			score += 3
		}
		cands = append(cands, attr{field: field, name: sn, schema: p, score: score + bonus, order: order})
	}
	for _, k := range item.Order {
		p := item.Properties[k]
		if p == nil || !reTmplSafe.MatchString(k) {
			continue // a key templates cannot address ("foo.bar", "is public")
		}
		if p.Is("object") && len(p.Properties) > 0 && !isSecretField(k, p) && !alwaysStripped[k] {
			// One level of nesting, only where the object or its field names
			// an exposure-relevant setting (e.g. ssoProtection.deploymentType).
			parentSec := reSecurity.MatchString(snake(k))
			for _, ck := range p.Order {
				cp := p.Properties[ck]
				if cp == nil || !reTmplSafe.MatchString(ck) || (!cp.Is("boolean") && len(cp.Enum) == 0 && !cp.Is("string")) {
					continue
				}
				if !parentSec && !reSecurity.MatchString(snake(ck)) {
					continue
				}
				before := len(cands)
				consider(k+"."+ck, snake(k)+"_"+snake(ck), cp, 0)
				if len(cands) > before {
					cands[len(cands)-1].name = snake(k) + "_" + snake(ck)
				}
			}
			continue
		}
		consider(k, k, p, 0)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	var out []attr
	seen := map[string]bool{}
	for _, c := range cands {
		if len(out) >= maxAttrs {
			break
		}
		if reservedAttrs[c.name] || seen[c.name] {
			continue
		}
		seen[c.name] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}

// ---- list shapes ----

// Arrays beside the real list in envelope responses (Cloudflare's
// {result, errors, messages}).
var notItems = map[string]bool{"errors": true, "error": true, "messages": true, "warnings": true, "links": true, "included": true}

var wrapperNames = []string{"data", "items", "results", "result", "records", "entries", "values", "list", "content", "nodes", "rows", "objects", "resources", "elements"}

// listItems finds where the list of items is in a response: the root array
// ("$") or an array-of-objects property. It returns every alternative (an
// API answering either {envs: [...]} or [...] gives [envs, $]).
func listItems(resp *openapi.Schema, lastSeg string) ([]string, *openapi.Schema) {
	if resp == nil {
		return nil, nil
	}
	var paths []string
	var item *openapi.Schema
	if (resp.Is("array") || len(resp.Types) == 0) && objectLike(resp.Items) {
		paths = append(paths, "$")
		item = resp.Items
	}
	if len(resp.Properties) > 0 {
		var arrays []string
		for _, k := range resp.Order {
			p := resp.Properties[k]
			if notItems[snake(k)] {
				continue
			}
			if p != nil && (p.Is("array") || len(p.Types) == 0) && objectLike(p.Items) {
				arrays = append(arrays, k)
			}
		}
		pick := ""
		switch {
		case len(arrays) == 1:
			pick = arrays[0]
		case len(arrays) > 1:
			want := map[string]bool{snake(lastSeg): true, singular(snake(lastSeg)): true}
			for _, a := range arrays {
				if want[snake(a)] || want[singular(snake(a))] {
					pick = a
					break
				}
			}
			if pick == "" {
				for _, w := range wrapperNames {
					for _, a := range arrays {
						if a == w {
							pick = a
							break
						}
					}
					if pick != "" {
						break
					}
				}
			}
		}
		if pick != "" {
			paths = append([]string{pick}, paths...)
			if item == nil {
				item = resp.Properties[pick].Items
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return paths, item
}

func objectLike(s *openapi.Schema) bool {
	return s != nil && (s.Is("object") || len(s.Types) == 0) && len(s.Properties) > 0
}

// ---- pagination ----

type paging struct {
	next, param string // engine-supported cursor pagination
	style       string // cursor, link, next_url, offset, page, starting_after, none
	note        string
}

var reCursorField = regexp.MustCompile(`^(?:next_?cursor|next_?page_?token|next_?token|cursor|next|continuation_?token|end_?cursor|next_?key|next_?marker|next_?page_?cursor|until|next_?page|next_?page_?number)$`)
var reCursorParam = regexp.MustCompile(`^(?:cursor|after|until|page_?token|next_?token|continuation_?token|marker|next|page_?cursor|start_?cursor|next_?page_?token|starting_?token|from_?cursor|page)$`)
var reSizeParam = regexp.MustCompile(`^(?:limit|per_?page|page_?size|size|max_?results|max_?items|count|take|top|first|page_?limit)$`)
var reMaxInText = regexp.MustCompile(`(?i)max(?:imum)?[^0-9]{0,30}(\d{2,4})`)

func detectPaging(op *openapi.Operation, resp *openapi.Schema, itemsProp string) paging {
	if pg, ok := extensionPaging(op); ok {
		return pg
	}
	var params []string
	for _, p := range op.Params {
		if p.In == "query" {
			params = append(params, p.Name)
		}
	}
	// Cursor fields in the response (depth 2), skipping the items array.
	type cf struct{ path, name string }
	var fields []cf
	var nextURL bool
	hasMore := false
	if resp != nil {
		var walk func(s *openapi.Schema, prefix string, depth int)
		walk = func(s *openapi.Schema, prefix string, depth int) {
			if s == nil || depth > 2 {
				return
			}
			for _, k := range s.Order {
				if prefix == "" && k == itemsProp {
					continue
				}
				p := s.Properties[k]
				if p == nil {
					continue
				}
				path := k
				if prefix != "" {
					path = prefix + "." + k
				}
				sn := strings.ReplaceAll(snake(k), "_", "")
				if snake(k) == "has_more" || sn == "hasmore" || sn == "hasnextpage" {
					hasMore = true
				}
				if p.Scalar() && !p.Is("boolean") && reCursorField.MatchString(snake(k)) {
					ex, _ := p.Example.(string)
					if p.Is("string") && (p.Format == "uri" || p.Format == "url" || strings.HasPrefix(ex, "http://") || strings.HasPrefix(ex, "https://") || strings.HasPrefix(ex, "/")) {
						nextURL = true
						continue
					}
					fields = append(fields, cf{path, snake(k)})
				}
				if p.Is("object") {
					walk(p, path, depth+1)
				}
			}
		}
		walk(resp, "", 0)
	}
	var cursorParams []string
	for _, q := range params {
		if reCursorParam.MatchString(snake(q)) {
			cursorParams = append(cursorParams, q)
		}
	}
	if len(fields) > 0 && len(cursorParams) > 0 {
		// Prefer a pair whose names agree (next_page_token <-> page_token).
		best, bestScore := 0, -1
		pbest := 0
		for i, f := range fields {
			for j, q := range cursorParams {
				score := 0
				fq := strings.ReplaceAll(snake(q), "_", "")
				ff := strings.ReplaceAll(f.name, "_", "")
				switch {
				case strings.Contains(ff, fq):
					score = 3
				case strings.Contains(fq, "cursor") && strings.Contains(ff, "cursor"):
					score = 2
				case strings.Contains(fq, "token") && strings.Contains(ff, "token"):
					score = 2
				case ff == "next":
					score = 1
				}
				if score > bestScore {
					best, pbest, bestScore = i, j, score
				}
			}
		}
		return paging{next: fields[best].path, param: cursorParams[pbest], style: "cursor"}
	}
	for _, r := range op.Responses {
		for _, h := range r.Headers {
			if strings.EqualFold(h, "Link") {
				return paging{style: "link", note: "pages are linked with a Link header, which the engine does not follow yet"}
			}
		}
	}
	has := func(names ...string) bool {
		for _, q := range params {
			for _, n := range names {
				if snake(q) == n {
					return true
				}
			}
		}
		return false
	}
	switch {
	case nextURL:
		return paging{style: "next_url", note: "the next page is a URL, which the engine does not follow yet"}
	case has("starting_after", "after_id", "after") && hasMore:
		return paging{style: "starting_after", note: "pages continue from the last item's id, which the engine does not support yet"}
	case has("page", "page_number", "page_num"):
		return paging{style: "page", note: "page-number pagination is not supported by the engine yet"}
	case has("offset", "skip", "start"):
		return paging{style: "offset", note: "offset pagination is not supported by the engine yet"}
	case len(cursorParams) > 0 && !has("page"):
		return paging{style: "unknown", note: "the API takes a " + cursorParams[0] + " parameter, but the response names no next cursor"}
	case hasMore:
		return paging{style: "unknown", note: "the response says there are more items, but no cursor parameter was found"}
	}
	return paging{style: "none"}
}

// extensionPaging reads vendor pagination extensions that name a cursor.
func extensionPaging(op *openapi.Operation) (paging, bool) {
	jp := func(v any) string {
		s, _ := v.(string)
		s = strings.TrimPrefix(strings.TrimPrefix(s, "$response."), "$.")
		s = strings.TrimPrefix(s, "$response.body#/")
		s = strings.TrimPrefix(s, "$")
		return strings.Trim(strings.ReplaceAll(s, "/", "."), ".")
	}
	if x, ok := op.Ext["x-speakeasy-pagination"].(map[string]any); ok {
		typ, _ := x["type"].(string)
		outs, _ := x["outputs"].(map[string]any)
		next := jp(outs["nextCursor"])
		param := ""
		if ins, ok := x["inputs"].([]any); ok {
			for _, in := range ins {
				m, _ := in.(map[string]any)
				if t, _ := m["type"].(string); t == "cursor" {
					if where, _ := m["in"].(string); where == "parameters" {
						param, _ = m["name"].(string)
					}
				}
			}
		}
		if typ == "cursor" && next != "" && param != "" && !strings.ContainsAny(next, "[]*?") {
			return paging{next: next, param: param, style: "cursor", note: "from x-speakeasy-pagination"}, true
		}
		if typ != "" {
			return paging{style: typ, note: "x-speakeasy-pagination type " + typ + " is not supported by the engine yet"}, true
		}
	}
	if x, ok := op.Ext["x-fern-pagination"].(map[string]any); ok {
		cur, _ := x["cursor"].(string)
		next := jp(x["next_cursor"])
		param := strings.TrimPrefix(cur, "$request.")
		if strings.HasPrefix(cur, "$request.") && next != "" && !strings.Contains(param, ".") {
			return paging{next: next, param: param, style: "cursor", note: "from x-fern-pagination"}, true
		}
		return paging{style: "unknown", note: "x-fern-pagination style is not supported by the engine yet"}, true
	}
	if x, ok := op.Ext["x-ms-pageable"].(map[string]any); ok {
		if n, _ := x["nextLinkName"].(string); n != "" {
			return paging{style: "next_url", note: "x-ms-pageable nextLink is a URL, which the engine does not follow yet"}, true
		}
	}
	return paging{}, false
}

// pageSize returns the page-size query parameter and the largest value the
// spec allows, or "" when the spec gives no maximum (sending a guess could
// make every call fail).
func pageSize(op *openapi.Operation) (string, string) {
	for _, p := range op.Params {
		if p.In != "query" || !reSizeParam.MatchString(snake(p.Name)) || p.Schema == nil {
			continue
		}
		if !p.Schema.Is("integer") && !p.Schema.Is("number") && len(p.Schema.Types) != 0 {
			continue
		}
		if p.Schema.Maximum != nil && *p.Schema.Maximum >= 1 {
			m := int(*p.Schema.Maximum)
			if m > 1000 {
				m = 1000
			}
			return p.Name, strconv.Itoa(m)
		}
		if m := reMaxInText.FindStringSubmatch(p.Description + " " + p.Schema.Description); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 10 && n <= 1000 {
				return p.Name, m[1]
			}
		}
	}
	return "", ""
}

// unwrapEnvelope handles list items that wrap the resource in one object
// property next to a cursor (Render: [{cursor, service: {...}}]). It returns
// the field prefix ("service.") and the inner schema, or "" and item.
func unwrapEnvelope(item *openapi.Schema, kind string) (string, *openapi.Schema) {
	if item == nil || pickID(item, kind, nil) != "" {
		return "", item
	}
	var only string
	for _, k := range item.Order {
		p := item.Properties[k]
		if objectLike(p) && pickID(p, kind, nil) != "" {
			if only != "" {
				return "", item
			}
			only = k
		}
	}
	if only == "" {
		return "", item
	}
	return only + ".", item.Properties[only]
}
