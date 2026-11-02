package openapi

import (
	"io/fs"
	"net/url"
	"path"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// resolve follows a chain of $refs (for parameters, responses, request
// bodies, path items, security schemes) and returns the final node and the
// file it lives in. A broken ref returns a nil node and a warning.
func (l *loader) resolve(file string, n *yaml.Node) (string, *yaml.Node) {
	for i := 0; i < 32; i++ {
		n = deref(n)
		ref := str(get(n, "$ref"))
		if n == nil || ref == "" {
			return file, n
		}
		f, t, ok := l.lookupRef(file, ref)
		if !ok {
			return file, nil
		}
		file, n = f, t
	}
	l.warn("$ref chain longer than 32 links; stopped")
	return file, nil
}

// lookupRef finds the node a $ref points to.
func (l *loader) lookupRef(file, ref string) (string, *yaml.Node, bool) {
	target, frag, _ := strings.Cut(ref, "#")
	if target != "" {
		low := strings.ToLower(target)
		if strings.Contains(low, "://") || strings.HasPrefix(low, "urn:") || strings.HasPrefix(low, "file:") {
			l.warn("$ref %s points to a URL; it was not followed (Muretto never fetches refs)", ref)
			return "", nil, false
		}
		target = path.Clean(path.Join(path.Dir(file), target))
		if file == "" {
			target = path.Clean(strings.TrimPrefix(target, "/"))
		}
		root, ok := l.files[target]
		if !ok {
			if l.fsys == nil {
				l.warn("$ref %s needs the other files of the spec; give the whole directory", ref)
				return "", nil, false
			}
			if !fs.ValidPath(target) {
				l.warn("$ref %s points outside the spec's directory; it was not followed", ref)
				return "", nil, false
			}
			data, err := readCapped(l.fsys, target)
			if err != nil {
				l.warn("$ref %s: %v", ref, err)
				return "", nil, false
			}
			root, err = parseTree(data)
			if err != nil {
				l.warn("$ref %s: %v", ref, err)
				return "", nil, false
			}
			l.files[target] = root
		}
		file = target
		if frag == "" {
			return file, root, true
		}
		n := pointer(root, frag)
		if n == nil {
			l.warn("$ref %s not found", ref)
			return "", nil, false
		}
		return file, n, true
	}
	if frag != "" && !strings.HasPrefix(frag, "/") {
		l.warn("$ref %s uses an anchor; only JSON Pointer refs are followed", ref)
		return "", nil, false
	}
	n := pointer(l.files[file], frag)
	if n == nil {
		l.warn("$ref %s not found", ref)
		return "", nil, false
	}
	return file, n, true
}

func readCapped(fsys fs.FS, name string) ([]byte, error) {
	if st, err := fs.Stat(fsys, name); err == nil && st.Size() > MaxFileSize {
		return nil, fs.ErrInvalid
	}
	return fs.ReadFile(fsys, name)
}

// pointer evaluates a JSON Pointer fragment ("/components/schemas/X").
func pointer(root *yaml.Node, frag string) *yaml.Node {
	if dec, err := url.PathUnescape(frag); err == nil {
		frag = dec
	}
	n := deref(root)
	if frag == "" {
		return n
	}
	for _, tok := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch {
		case n == nil:
			return nil
		case n.Kind == yaml.MappingNode:
			n = get(n, tok)
		case n.Kind == yaml.SequenceNode:
			i, err := strconv.Atoi(tok)
			its := items(n)
			if err != nil || i < 0 || i >= len(its) {
				return nil
			}
			n = its[i]
		default:
			return nil
		}
	}
	return n
}

func refName(ref string) string {
	_, frag, _ := strings.Cut(ref, "#")
	if frag == "" {
		base := path.Base(strings.SplitN(ref, "#", 2)[0])
		return strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml"), ".json")
	}
	i := strings.LastIndex(frag, "/")
	return strings.ReplaceAll(strings.ReplaceAll(frag[i+1:], "~1", "/"), "~0", "~")
}

// schemaKeys are keywords that change a schema's shape. A $ref with any of
// them beside it is merged with them (JSON Schema 2020-12 semantics; older
// versions ignore siblings, but real specs rely on them).
var schemaKeys = []string{
	"type", "properties", "items", "allOf", "oneOf", "anyOf", "nullable", "x-nullable",
	"readOnly", "writeOnly", "required", "enum", "const", "format", "additionalProperties", "deprecated",
}

const maxSchemaDepth = 256

func (l *loader) schema(file string, n *yaml.Node) *Schema {
	return l.schemaDepth(file, n, 0, map[*yaml.Node]bool{})
}

func (l *loader) schemaDepth(file string, n *yaml.Node, depth int, refs map[*yaml.Node]bool) *Schema {
	n = deref(n)
	if n == nil || depth > maxSchemaDepth {
		return nil
	}
	if s, ok := l.schemas[n]; ok {
		return s
	}
	if n.Kind == yaml.ScalarNode {
		switch str(n) {
		case "true", "True", "TRUE":
			return &Schema{}
		}
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	if ref := str(get(n, "$ref")); ref != "" {
		if refs[n] {
			return nil // a ref cycle with no schema in between
		}
		refs[n] = true
		defer delete(refs, n)
		f, t, ok := l.lookupRef(file, ref)
		var target *Schema
		if ok {
			target = l.schemaDepth(f, t, depth+1, refs)
			if target != nil && target.Name == "" {
				target.Name = refName(ref)
			}
		}
		siblings := false
		for _, k := range schemaKeys {
			if has(n, k) {
				siblings = true
				break
			}
		}
		if !siblings {
			if target != nil {
				l.schemas[n] = target
			}
			return target
		}
		s := &Schema{Required: map[string]bool{}}
		l.schemas[n] = s
		absorb(s, target, true, 0)
		l.fill(file, s, n, depth, refs)
		return s
	}
	s := &Schema{Required: map[string]bool{}}
	l.schemas[n] = s
	l.fill(file, s, n, depth, refs)
	return s
}

func (l *loader) fill(file string, s *Schema, n *yaml.Node, depth int, refs map[*yaml.Node]bool) {
	addType := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		switch t {
		case "":
		case "null":
			s.Nullable = true
		default:
			if !s.Is(t) {
				s.Types = append(s.Types, t)
			}
		}
	}
	if tn := get(n, "type"); tn != nil {
		if tn.Kind == yaml.SequenceNode {
			for _, t := range strs(tn) {
				addType(t)
			}
		} else {
			addType(str(tn))
		}
	}
	if isTrue(get(n, "nullable")) || isTrue(get(n, "x-nullable")) {
		s.Nullable = true
	}
	if v := str(get(n, "format")); v != "" {
		s.Format = v
	}
	if v := strings.TrimSpace(str(get(n, "title"))); v != "" {
		s.Title = v
	}
	if v := strings.TrimSpace(str(get(n, "description"))); v != "" {
		s.Description = v
	}
	if v := str(get(n, "pattern")); v != "" {
		s.Pattern = v
	}
	if en := get(n, "enum"); en != nil {
		var vals []string
		for _, e := range items(en) {
			if e.Kind == yaml.ScalarNode && e.Tag == "!!null" {
				s.Nullable = true
				continue
			}
			if e.Kind == yaml.ScalarNode {
				vals = append(vals, e.Value)
			}
		}
		if len(vals) > 0 {
			s.Enum = vals
		}
	}
	if c := get(n, "const"); c != nil && c.Kind == yaml.ScalarNode {
		if c.Tag == "!!null" {
			s.Nullable = true
		} else if len(s.Enum) == 0 {
			s.Enum = []string{c.Value}
		}
	}
	if d := get(n, "default"); d != nil {
		s.Default = value(d)
	}
	if ex := get(n, "example"); ex != nil {
		s.Example = value(ex)
	} else if exs := get(n, "examples"); exs != nil && exs.Kind == yaml.SequenceNode {
		if its := items(exs); len(its) > 0 {
			s.Example = value(its[0])
		}
	}
	s.ReadOnly = s.ReadOnly || isTrue(get(n, "readOnly"))
	s.WriteOnly = s.WriteOnly || isTrue(get(n, "writeOnly"))
	s.Deprecated = s.Deprecated || isTrue(get(n, "deprecated"))
	if m := str(get(n, "maximum")); m != "" {
		if f, err := strconv.ParseFloat(m, 64); err == nil {
			s.Maximum = &f
		}
	}
	for _, e := range entries(get(n, "properties")) {
		ps := l.schemaDepth(file, e.val, depth+1, refs)
		if ps == nil {
			continue
		}
		if s.Properties == nil {
			s.Properties = map[string]*Schema{}
		}
		if _, ok := s.Properties[e.key]; !ok {
			s.Order = append(s.Order, e.key)
		}
		s.Properties[e.key] = ps
	}
	if rq := get(n, "required"); rq != nil && rq.Kind == yaml.SequenceNode {
		for _, k := range strs(rq) {
			s.Required[k] = true
		}
	}
	if it := get(n, "items"); it != nil {
		if it.Kind == yaml.SequenceNode {
			if its := items(it); len(its) > 0 {
				s.Items = l.schemaDepth(file, its[0], depth+1, refs)
			}
		} else {
			s.Items = l.schemaDepth(file, it, depth+1, refs)
		}
	}
	if ap := get(n, "additionalProperties"); isMap(ap) {
		s.Additional = l.schemaDepth(file, ap, depth+1, refs)
	}
	for _, it := range items(get(n, "allOf")) {
		absorb(s, l.schemaDepth(file, it, depth+1, refs), true, 0)
	}
	var variants []*Schema
	for _, key := range []string{"oneOf", "anyOf"} {
		for _, it := range items(get(n, key)) {
			sub := l.schemaDepth(file, it, depth+1, refs)
			if sub == nil {
				continue
			}
			if nullOnly(sub) {
				s.Nullable = true
				continue
			}
			variants = append(variants, sub)
		}
	}
	switch len(variants) {
	case 0:
	case 1:
		absorb(s, variants[0], true, 0)
	default:
		s.Variants = variants
		for _, v := range variants {
			absorb(s, v, false, 0)
		}
		for k := range variants[0].Required {
			all := true
			for _, v := range variants[1:] {
				if !v.Required[k] {
					all = false
					break
				}
			}
			if all {
				s.Required[k] = true
			}
		}
	}
	if len(s.Types) == 0 {
		switch {
		case len(s.Properties) > 0:
			s.Types = []string{"object"}
		case s.Items != nil:
			s.Types = []string{"array"}
		}
	}
	if ext := extensions(n); ext != nil {
		if s.Ext == nil {
			s.Ext = map[string]any{}
		}
		for k, v := range ext {
			s.Ext[k] = v
		}
	}
}

func nullOnly(s *Schema) bool {
	return s.Nullable && len(s.Types) == 0 && len(s.Properties) == 0 && s.Items == nil && len(s.Variants) == 0
}

func clone(s *Schema) *Schema {
	c := *s
	c.Types = append([]string(nil), s.Types...)
	c.Order = append([]string(nil), s.Order...)
	c.Properties = make(map[string]*Schema, len(s.Properties))
	for k, v := range s.Properties {
		c.Properties[k] = v
	}
	c.Required = make(map[string]bool, len(s.Required))
	for k, v := range s.Required {
		c.Required[k] = v
	}
	if s.Ext != nil {
		c.Ext = make(map[string]any, len(s.Ext))
		for k, v := range s.Ext {
			c.Ext[k] = v
		}
	}
	return &c
}

// absorb merges src into dst (allOf, or one oneOf/anyOf alternative). It
// never mutates src: a property both define is replaced by a merged copy.
func absorb(dst, src *Schema, required bool, depth int) {
	if src == nil || src == dst || depth > 8 {
		return
	}
	for _, t := range src.Types {
		if !dst.Is(t) {
			dst.Types = append(dst.Types, t)
		}
	}
	if dst.Format == "" {
		dst.Format = src.Format
	}
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if dst.Pattern == "" {
		dst.Pattern = src.Pattern
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	for _, k := range src.Order {
		sp := src.Properties[k]
		if dst.Properties == nil {
			dst.Properties = map[string]*Schema{}
		}
		if dp, ok := dst.Properties[k]; ok {
			if dp != sp && sp != nil {
				m := clone(dp)
				absorb(m, sp, true, depth+1)
				dst.Properties[k] = m
			}
			continue
		}
		dst.Properties[k] = sp
		dst.Order = append(dst.Order, k)
	}
	if dst.Required == nil {
		dst.Required = map[string]bool{}
	}
	if required {
		for k, v := range src.Required {
			if v {
				dst.Required[k] = true
			}
		}
	}
	if dst.Items == nil {
		dst.Items = src.Items
	}
	if dst.Additional == nil {
		dst.Additional = src.Additional
	}
	if len(dst.Enum) == 0 {
		dst.Enum = src.Enum
	}
	if dst.Default == nil {
		dst.Default = src.Default
	}
	if dst.Example == nil {
		dst.Example = src.Example
	}
	if dst.Maximum == nil {
		dst.Maximum = src.Maximum
	}
	dst.Nullable = dst.Nullable || src.Nullable
	dst.ReadOnly = dst.ReadOnly || src.ReadOnly
	dst.WriteOnly = dst.WriteOnly || src.WriteOnly
	dst.Deprecated = dst.Deprecated || src.Deprecated
	for k, v := range src.Ext {
		if dst.Ext == nil {
			dst.Ext = map[string]any{}
		}
		if _, ok := dst.Ext[k]; !ok {
			dst.Ext[k] = v
		}
	}
}
