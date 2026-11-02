package adapters

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// action is a RawAction pre-digested for signature matching.
type action struct {
	raw       observe.RawAction
	output    string // ANSI-stripped
	command   string // Input.command for Bash-like tools
	mcpServer string // lower-case server name, "" when not an MCP call
	mcpTool   string // lower-case tool name
	results   []any  // JSON objects/arrays in the output (parseJSONResults)
}

func digest(a observe.RawAction) *action {
	d := &action{raw: a, output: ansi.ReplaceAllString(a.Output, "")}
	if m, ok := a.Input.(map[string]any); ok {
		if c, ok := m["command"].(string); ok {
			d.command = c
		}
	}
	name := a.Action.ToolName
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if i := strings.LastIndex(rest, "__"); i > 0 {
			d.mcpServer = strings.ToLower(rest[:i])
			d.mcpTool = strings.ToLower(rest[i+2:])
		}
		if a.Action.McpServer != nil && *a.Action.McpServer != "" {
			d.mcpServer = strings.ToLower(*a.Action.McpServer)
		}
	}
	d.results = parseJSONResults(d.output)
	return d
}

// maxResults caps how many JSON values one output yields.
const maxResults = 32

// parseJSONResults returns the JSON objects and arrays in s: s itself, or
// every one that starts a line. CLI tools print JSON on stdout next to
// stderr noise in the same Bash result, and an agent often chains several
// commands in one call (`supabase functions new x; supabase functions deploy
// x`), each printing its own JSON line. Values are returned in output order;
// a value's own lines are never parsed again as separate values.
func parseJSONResults(s string) []any {
	decode := func(t string) (any, int, bool) {
		dec := json.NewDecoder(strings.NewReader(t))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, 0, false
		}
		switch v.(type) {
		case map[string]any, []any:
			return v, int(dec.InputOffset()), true
		}
		return nil, 0, false
	}
	var out []any
	i := len(s) - len(strings.TrimLeft(s, " \t\r\n"))
	for i < len(s) && len(out) < maxResults {
		if s[i] == '{' || s[i] == '[' {
			if v, n, ok := decode(s[i:]); ok {
				out = append(out, v)
				i += n
				continue
			}
		}
		nl := strings.IndexByte(s[i:], '\n')
		if nl < 0 {
			break
		}
		i += nl + 1
	}
	return out
}

// resultHas reports whether r is an object with one of keys at top level.
func resultHas(r any, keys []string) bool {
	m, ok := r.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func serverMatches(server string, wanted []string) bool {
	if server == "" {
		return false
	}
	for _, w := range wanted {
		w = strings.ToLower(w)
		// mcp__supabase__x, mcp__claude_ai_Supabase__x, mcp__plugin_p_supabase__x
		if server == w || strings.HasSuffix(server, "_"+w) {
			return true
		}
	}
	return false
}

func (w *whenSpec) toolMatches(d *action) bool {
	if len(w.MCPTools) > 0 {
		if !serverMatches(d.mcpServer, w.MCPServer) {
			return false
		}
		for _, t := range w.MCPTools {
			if t == "*" || strings.ToLower(t) == d.mcpTool {
				return w.inputMatches(d)
			}
		}
		return false
	}
	if w.Tool == nil || !w.Tool.MatchString(d.raw.Action.ToolName) {
		return false
	}
	for _, re := range w.Command {
		if !re.MatchString(d.command) {
			return false
		}
	}
	if !w.inputMatches(d) {
		return false
	}
	for _, re := range w.NotCommand {
		if re.MatchString(d.command) {
			return false
		}
	}
	return true
}

// inputMatches checks input_matches (tool input fields by regex); MCP tools
// too, e.g. stripe_api_write's method and path.
func (w *whenSpec) inputMatches(d *action) bool {
	for path, re := range w.InputMatch {
		v, _ := lookup(map[string]any{"input": d.raw.Input}, "input."+path)
		s, _ := scalarString(v)
		if !re.MatchString(s) {
			return false
		}
	}
	return true
}

func (w *whenSpec) matches(d *action) bool {
	if w.Status != "any" && d.raw.Action.Status != contract.AgentActionStatusOk {
		return false
	}
	if !w.toolMatches(d) {
		return false
	}
	if w.Output != nil && !w.Output.MatchString(d.output) {
		return false
	}
	// One Bash call may print several JSON values (chained commands). A
	// lacked key (e.g. error) in any of them rejects the call; a wanted key
	// must be in at least one, and only those feed `from: result` extracts.
	for _, r := range d.results {
		if resultHas(r, w.ResultLacks) {
			return false
		}
	}
	if len(w.ResultHas) > 0 && len(w.results(d)) == 0 {
		return false
	}
	return true
}

// results returns the JSON values of d's output that `from: result` extracts
// may read: those with a result_has key, or all of them.
func (w *whenSpec) results(d *action) []any {
	if len(w.ResultHas) == 0 {
		return d.results
	}
	var out []any
	for _, r := range d.results {
		if resultHas(r, w.ResultHas) {
			out = append(out, r)
		}
	}
	return out
}

func guess(a *adapterSpec, raw observe.RawAction) bool {
	d := digest(raw)
	if serverMatches(d.mcpServer, a.MCPServers) {
		return true
	}
	for _, s := range a.Signatures {
		// A tool-only signature (any Bash call whose output has an id) says
		// nothing about which provider a call was for.
		if len(s.When.MCPTools) == 0 && len(s.When.Command) == 0 && len(s.When.InputMatch) == 0 {
			continue
		}
		if s.When.toolMatches(d) {
			return true
		}
	}
	return false
}

func extract(a *adapterSpec, raw observe.RawAction) []contract.Identifier {
	d := digest(raw)
	var out []contract.Identifier
	for _, s := range a.Signatures {
		if !s.When.matches(d) {
			continue
		}
		for _, e := range s.Extract {
			out = append(out, runExtract(a, s, e, d)...)
		}
	}
	return out
}

// runExtract runs one extract. A `from: result` extract reads each JSON value
// of the output in turn (a chained Bash call prints one per command), so the
// value it needs is found wherever it is; any other extract sees the first.
func runExtract(a *adapterSpec, s *signatureSpec, e *extractSpec, d *action) []contract.Identifier {
	rs := s.When.results(d)
	if e.From == "result" {
		var out []contract.Identifier
		for _, r := range rs {
			out = append(out, extractWith(a, s, e, d, r)...)
		}
		return out
	}
	if len(rs) > 0 {
		return extractWith(a, s, e, d, rs[0])
	}
	return extractWith(a, s, e, d, nil)
}

// extractWith runs extract e with result (nil: none) as {result}.
func extractWith(a *adapterSpec, s *signatureSpec, e *extractSpec, d *action, result any) []contract.Identifier {
	base := map[string]any{"input": d.raw.Input}
	// mcp.<key>: identifier params of the MCP server the call went to (e.g.
	// mcp.project_ref for a project-pinned Supabase server).
	mcp := make(map[string]any, len(d.raw.MCP))
	for k, v := range d.raw.MCP {
		mcp[k] = v
	}
	base["mcp"] = mcp
	if result != nil {
		base["result"] = result
	}
	for _, c := range e.Capture {
		m := c.Pattern.FindStringSubmatch(textFrom(d, base, c.From))
		for i, n := range c.Pattern.SubexpNames() {
			if n != "" && i < len(m) {
				base[n] = m[i]
			}
		}
	}
	type hit struct {
		ctx        map[string]any
		excerpt    string
		referenced bool
	}
	var hits []hit

	switch e.From {
	case "result", "input":
		if e.From == "result" && result == nil {
			return nil
		}
		if e.Each == "" {
			hits = append(hits, hit{ctx: base})
			break
		}
		v, ok := lookup(base, e.Each)
		if !ok {
			return nil
		}
		var els []any
		switch t := v.(type) {
		case []any:
			els = t
		case map[string]any:
			els = []any{t}
		}
		for _, el := range els {
			ctx := copyCtx(base)
			ctx["item"] = el
			hits = append(hits, hit{ctx: ctx})
		}
	default:
		text := preprocess(e.Preprocess, textFrom(d, base, e.From))
		if text == "" {
			return nil
		}
		names := e.Pattern.SubexpNames()
		for _, m := range e.Pattern.FindAllStringSubmatch(text, -1) {
			groups := map[string]string{}
			for i, n := range names {
				if n != "" && i < len(m) {
					groups[n] = m[i]
				}
			}
			ref := e.ReferencedIf != "" && groups[e.ReferencedIf] != ""
			values := []string{""}
			if e.Split != nil {
				values = nil
				for _, part := range strings.Split(groups[e.Split.Field], e.Split.Sep) {
					if part = strings.TrimSpace(part); part != "" {
						values = append(values, part)
					}
				}
			}
			for _, v := range values {
				ctx := copyCtx(base)
				for n, g := range groups {
					ctx[n] = g
				}
				if e.Split != nil {
					ctx[e.Split.Field] = v
				}
				hits = append(hits, hit{ctx: ctx, excerpt: strings.ReplaceAll(m[0], segmentSep, " "), referenced: ref})
			}
		}
	}

	var out []contract.Identifier
	for _, h := range hits {
		val, ok := e.Value.render(h.ctx)
		if !ok {
			continue
		}
		id := contract.Identifier{
			Provider:    a.Provider,
			Kind:        fullKind(a, e.Kind),
			Value:       val,
			Role:        contract.IdentifierRole(s.Role),
			SignatureID: s.ID,
		}
		if h.referenced {
			id.Role = contract.IdentifierRoleReferenced
		}
		if c, ok := e.Container.render(h.ctx); ok {
			id.Container = &c
		}
		ex := h.excerpt
		if ex == "" {
			ex = d.raw.Action.ToolName + " -> " + val
		}
		ex = truncate(strings.Join(strings.Fields(ex), " "), 200)
		id.Excerpt = &ex
		out = append(out, id)
	}
	return out
}

func textFrom(d *action, base map[string]any, from string) string {
	switch {
	case from == "command":
		return d.command
	case strings.HasPrefix(from, "input."):
		v, _ := lookup(base, from)
		s, _ := v.(string)
		return s
	default:
		return d.output
	}
}

func copyCtx(m map[string]any) map[string]any {
	c := make(map[string]any, len(m)+4)
	for k, v := range m {
		c[k] = v
	}
	return c
}

// dedupeIdentifiers keeps one identifier per (provider, kind, value,
// container); a created identifier beats a referenced one.
func dedupeIdentifiers(ids []contract.Identifier) []contract.Identifier {
	idx := map[string]int{}
	var out []contract.Identifier
	for _, id := range ids {
		var b bytes.Buffer
		b.WriteString(id.Provider + "\x00" + id.Kind + "\x00" + id.Value + "\x00")
		if id.Container != nil {
			b.WriteString(*id.Container)
		}
		k := b.String()
		if i, ok := idx[k]; ok {
			if out[i].Role == contract.IdentifierRoleReferenced && id.Role == contract.IdentifierRoleCreated {
				out[i] = id
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, id)
	}
	return out
}

var defaultMatch = func() matchSpec {
	t, _ := parseTmpl("{provider_id}")
	return matchSpec{Value: t}
}()

// matchesFor returns the match rules that may apply to id: one per extract of
// its signature that produces id's kind (the extract's own rule, else the
// kind's), or the kind's rule when the signature is unknown. An identifier
// does not record which extract produced it, so any of them may match.
func matchesFor(a *adapterSpec, id contract.Identifier) []matchSpec {
	kindMatch := defaultMatch
	if k := kindByFull(a, id.Kind); k != nil && k.Match != nil {
		kindMatch = *k.Match
	}
	var out []matchSpec
	for _, s := range a.Signatures {
		if s.ID != id.SignatureID {
			continue
		}
		for _, e := range s.Extract {
			if fullKind(a, e.Kind) != id.Kind {
				continue
			}
			if e.Match != nil {
				out = append(out, *e.Match)
			} else {
				out = append(out, kindMatch)
			}
		}
	}
	if len(out) == 0 {
		out = append(out, kindMatch)
	}
	return out
}
