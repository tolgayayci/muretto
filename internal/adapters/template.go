package adapters

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// tmpl is a string template from an adapter file: literal text with {expr}
// placeholders. An expr is a dotted path into the current context, followed by
// optional filters: {path|filter|filter:arg}.
//
// Filters (documented in docs/WRITING_AN_ADAPTER.md):
//
//	or:<path>     use <path> when the value is missing, null or empty
//	default:<lit> use the literal when the value is missing, null or empty
//	lower, upper, trim
//	join          sort an array of scalars and join it with ","; strings pass through
//	unquote       strip one pair of SQL double quotes and undouble ""
//	sql_ident     escape for use inside "..." (doubles ")
//	sql_literal   escape for use inside '...' (doubles ')
//	pg_ident      Postgres identifier as written in SQL: "Quoted" keeps case and
//	              is unquoted, bare identifiers fold to lower case
type tmpl struct {
	raw   string
	parts []tpart
}

type tpart struct {
	lit     string
	path    string
	filters []tfilter
}

type tfilter struct{ name, arg string }

var knownFilters = map[string]bool{
	"or": true, "default": true, "lower": true, "upper": true, "trim": true,
	"join": true, "unquote": true, "sql_ident": true, "sql_literal": true, "pg_ident": true,
}

func parseTmpl(s string) (tmpl, error) {
	t := tmpl{raw: s}
	for len(s) > 0 {
		i := strings.IndexByte(s, '{')
		if i < 0 {
			t.parts = append(t.parts, tpart{lit: s})
			break
		}
		if i > 0 {
			t.parts = append(t.parts, tpart{lit: s[:i]})
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return tmpl{}, fmt.Errorf("template %q: unclosed {", t.raw)
		}
		expr := strings.TrimSpace(s[i+1 : i+j])
		if expr == "" {
			return tmpl{}, fmt.Errorf("template %q: empty {}", t.raw)
		}
		segs := strings.Split(expr, "|")
		p := tpart{path: strings.TrimSpace(segs[0])}
		for _, f := range segs[1:] {
			name, arg, _ := strings.Cut(strings.TrimSpace(f), ":")
			if !knownFilters[name] {
				return tmpl{}, fmt.Errorf("template %q: unknown filter %q", t.raw, name)
			}
			if (name == "or" || name == "default") && arg == "" {
				return tmpl{}, fmt.Errorf("template %q: %s needs an argument", t.raw, name)
			}
			p.filters = append(p.filters, tfilter{name, arg})
		}
		t.parts = append(t.parts, p)
		s = s[i+j+1:]
	}
	return t, nil
}

func (t *tmpl) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: template must be a string", n.Line)
	}
	p, err := parseTmpl(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*t = p
	return nil
}

func (t tmpl) empty() bool { return t.raw == "" }

// render expands t. ok is false when a placeholder resolves to nothing (missing,
// null or empty after filters).
func (t tmpl) render(ctx map[string]any) (string, bool) {
	var b strings.Builder
	for _, p := range t.parts {
		if p.path == "" {
			b.WriteString(p.lit)
			continue
		}
		v, ok := p.eval(ctx)
		if !ok {
			return "", false
		}
		b.WriteString(v)
	}
	return b.String(), true
}

// renderLoose expands t, writing <path> for placeholders that resolve to
// nothing. Used for human text (titles, fixes) where a hole beats no text.
func (t tmpl) renderLoose(ctx map[string]any) string {
	var b strings.Builder
	for _, p := range t.parts {
		if p.path == "" {
			b.WriteString(p.lit)
			continue
		}
		if v, ok := p.eval(ctx); ok {
			b.WriteString(v)
		} else {
			b.WriteString("<" + p.path + ">")
		}
	}
	return b.String()
}

func (p tpart) eval(ctx map[string]any) (string, bool) {
	v, found := lookup(ctx, p.path)
	s, ok := scalarString(v)
	ok = ok && found
	for _, f := range p.filters {
		switch f.name {
		case "or":
			if !ok || s == "" {
				v2, found2 := lookup(ctx, f.arg)
				s, ok = scalarString(v2)
				ok = ok && found2
				v = v2
			}
		case "default":
			if !ok || s == "" {
				s, ok = f.arg, true
			}
		case "join":
			if arr, isArr := v.([]any); isArr {
				var parts []string
				for _, el := range arr {
					if es, eok := scalarString(el); eok {
						parts = append(parts, es)
					}
				}
				sort.Strings(parts)
				s, ok = strings.Join(parts, ","), true
			}
		case "lower":
			s = strings.ToLower(s)
		case "upper":
			s = strings.ToUpper(s)
		case "trim":
			s = strings.TrimSpace(s)
		case "unquote":
			s = sqlUnquote(s)
		case "pg_ident":
			if strings.HasPrefix(s, `"`) {
				s = sqlUnquote(s)
			} else {
				s = strings.ToLower(s)
			}
		case "sql_ident":
			s = strings.ReplaceAll(s, `"`, `""`)
		case "sql_literal":
			s = strings.ReplaceAll(s, `'`, `''`)
		}
	}
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func sqlUnquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

// lookup resolves a dotted path. found is true when every key exists; an
// explicit JSON null on the way yields (nil, true). "$" is the context itself.
func lookup(ctx any, path string) (any, bool) {
	if path == "$" || path == "" {
		return ctx, true
	}
	cur := ctx
	for _, seg := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case nil:
			return nil, true
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// scalarString renders a scalar. Arrays of scalars render joined with ","
// in their original order. Objects and nil are not scalars.
func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case []any:
		var parts []string
		for _, el := range t {
			s, ok := scalarString(el)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), true
	default:
		return "", false
	}
}

// attrValue converts a decoded JSON value into a Resource attribute value
// (string, number, bool or nil). Arrays of scalars become a joined string;
// objects are not allowed and report false.
func attrValue(v any) (any, bool) {
	switch t := v.(type) {
	case nil, string, bool:
		return t, true
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i, true
		}
		if f, err := t.Float64(); err == nil {
			return f, true
		}
		return t.String(), true
	case int, int64, float64:
		return t, true
	case []any:
		s, ok := scalarString(t)
		return s, ok
	default:
		return nil, false
	}
}
