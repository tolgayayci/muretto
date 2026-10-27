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
	result    any    // Output parsed as JSON, or nil
	hasResult bool
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
	d.result, d.hasResult = parseJSONLoose(d.output)
	return d
}

// parseJSONLoose parses s as JSON, or failing that the first JSON object or
// array that starts a line (CLI tools print JSON on stdout next to stderr
// noise in the same Bash result).
func parseJSONLoose(s string) (any, bool) {
	try := func(t string) (any, bool) {
		dec := json.NewDecoder(strings.NewReader(t))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		switch v.(type) {
		case map[string]any, []any:
			return v, true
		}
		return nil, false
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return nil, false
	}
	if v, ok := try(t); ok {
		return v, true
	}
	for i := 0; i < len(s); i++ {
		if (s[i] == '{' || s[i] == '[') && (i == 0 || s[i-1] == '\n') {
			if v, ok := try(s[i:]); ok {
				return v, true
			}
		}
	}
	return nil, false
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
				return true
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
	for path, re := range w.InputMatch {
		v, _ := lookup(map[string]any{"input": d.raw.Input}, "input."+path)
		s, _ := v.(string)
		if !re.MatchString(s) {
			return false
		}
	}
	for _, re := range w.NotCommand {
		if re.MatchString(d.command) {
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
	if len(w.ResultLacks) > 0 {
		if m, ok := d.result.(map[string]any); ok {
			for _, k := range w.ResultLacks {
				if _, has := m[k]; has {
					return false
				}
			}
		}
	}
	if len(w.ResultHas) > 0 {
		m, ok := d.result.(map[string]any)
		if !ok {
			return false
		}
		has := false
		for _, k := range w.ResultHas {
			if _, ok := m[k]; ok {
				has = true
			}
		}
		if !has {
			return false
		}
	}
	return true
}

func guess(a *adapterSpec, raw observe.RawAction) bool {
	d := digest(raw)
	if serverMatches(d.mcpServer, a.MCPServers) {
		return true
	}
	for _, s := range a.Signatures {
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

func runExtract(a *adapterSpec, s *signatureSpec, e *extractSpec, d *action) []contract.Identifier {
	base := map[string]any{"input": d.raw.Input}
	// mcp.<key>: identifier params of the MCP server the call went to (e.g.
	// mcp.project_ref for a project-pinned Supabase server).
	mcp := make(map[string]any, len(d.raw.MCP))
	for k, v := range d.raw.MCP {
		mcp[k] = v
	}
	base["mcp"] = mcp
	if d.hasResult {
		base["result"] = d.result
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

	switch {
	case e.From == "result" || e.From == "input":
		if e.From == "result" && !d.hasResult {
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
