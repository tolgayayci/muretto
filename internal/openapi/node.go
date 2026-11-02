package openapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// maxDepth bounds nesting while parsing JSON and following YAML aliases.
const maxDepth = 512

// parseTree parses a JSON or YAML document into a yaml.Node tree. JSON is
// read with encoding/json (YAML parsers reject tab indentation, which some
// JSON files use) and converted to nodes so both formats keep key order.
func parseTree(data []byte) (*yaml.Node, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errors.New("empty document")
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		n, err := jsonNode(dec, 0)
		if err != nil {
			return nil, fmt.Errorf("JSON: %w", err)
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, errors.New("JSON: data after the top-level value")
		}
		return n, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("YAML: %w", err)
	}
	n := deref(&root)
	if n == nil {
		return nil, errors.New("empty document")
	}
	return n, nil
}

func jsonNode(dec *json.Decoder, depth int) (*yaml.Node, error) {
	if depth > maxDepth {
		return nil, errors.New("nested too deeply")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				v, err := jsonNode(dec, depth+1)
				if err != nil {
					return nil, err
				}
				m.Content = append(m.Content, scalar(key, "!!str"), v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			s := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				v, err := jsonNode(dec, depth+1)
				if err != nil {
					return nil, err
				}
				s.Content = append(s.Content, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case string:
		return scalar(t, "!!str"), nil
	case json.Number:
		if strings.ContainsAny(string(t), ".eE") {
			return scalar(string(t), "!!float"), nil
		}
		return scalar(string(t), "!!int"), nil
	case bool:
		if t {
			return scalar("true", "!!bool"), nil
		}
		return scalar("false", "!!bool"), nil
	case nil:
		return scalar("null", "!!null"), nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

func scalar(v, tag string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v}
}

// deref unwraps document nodes and follows aliases.
func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && i < 64; i++ {
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) == 0 {
				return nil
			}
			n = n.Content[0]
		case yaml.AliasNode:
			n = n.Alias
		default:
			return n
		}
	}
	return n
}

type entry struct {
	key string
	val *yaml.Node
}

// entries lists a mapping's pairs in order. A repeated key keeps its last
// value (at the first key's position); YAML merge keys (<<) add pairs the
// mapping does not define itself.
func entries(n *yaml.Node) []entry {
	return entriesDepth(n, 0)
}

func entriesDepth(n *yaml.Node, depth int) []entry {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode || depth > 8 {
		return nil
	}
	var out []entry
	idx := map[string]int{}
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := deref(n.Content[i])
		if k == nil || k.Kind != yaml.ScalarNode {
			continue
		}
		if k.Value == "<<" && k.Tag != "!!str" {
			merges = append(merges, n.Content[i+1])
			continue
		}
		if j, ok := idx[k.Value]; ok {
			out[j].val = n.Content[i+1]
			continue
		}
		idx[k.Value] = len(out)
		out = append(out, entry{k.Value, n.Content[i+1]})
	}
	for _, m := range merges {
		m = deref(m)
		var srcs []*yaml.Node
		if m != nil && m.Kind == yaml.SequenceNode {
			srcs = m.Content
		} else {
			srcs = []*yaml.Node{m}
		}
		for _, src := range srcs {
			for _, e := range entriesDepth(src, depth+1) {
				if _, ok := idx[e.key]; !ok {
					idx[e.key] = len(out)
					out = append(out, e)
				}
			}
		}
	}
	return out
}

// get returns the value of key in mapping n, or nil. A repeated key gives
// its last value; merge keys (<<) are consulted only when n lacks the key.
func get(n *yaml.Node, key string) *yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out *yaml.Node
	merge := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind == yaml.AliasNode {
			k = deref(k)
		}
		if k == nil || k.Kind != yaml.ScalarNode {
			continue
		}
		if k.Value == "<<" && k.Tag != "!!str" {
			merge = true
			continue
		}
		if k.Value == key {
			out = n.Content[i+1]
		}
	}
	if out == nil && merge {
		for _, e := range entries(n) {
			if e.key == key {
				return deref(e.val)
			}
		}
	}
	return deref(out)
}

func has(n *yaml.Node, key string) bool { return get(n, key) != nil }

// str returns a scalar's text, or "" for anything else (including null).
func str(n *yaml.Node) string {
	n = deref(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

func isTrue(n *yaml.Node) bool {
	switch str(n) {
	case "true", "True", "TRUE":
		return true
	}
	return false
}

func items(n *yaml.Node) []*yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, 0, len(n.Content))
	for _, c := range n.Content {
		if c = deref(c); c != nil {
			out = append(out, c)
		}
	}
	return out
}

func strs(n *yaml.Node) []string {
	var out []string
	for _, c := range items(n) {
		if s := str(c); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isMap(n *yaml.Node) bool {
	n = deref(n)
	return n != nil && n.Kind == yaml.MappingNode
}

// value decodes a node into plain Go values (maps, slices, scalars).
func value(n *yaml.Node) any {
	n = deref(n)
	if n == nil {
		return nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil
	}
	return normalizeValue(v, 0)
}

// normalizeValue turns map[string]interface{} trees from yaml.v3 into
// map[string]any with string keys only.
func normalizeValue(v any, depth int) any {
	if depth > 64 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeValue(x, depth+1)
		}
		return t
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[fmt.Sprint(k)] = normalizeValue(x, depth+1)
		}
		return m
	case []any:
		for i, x := range t {
			t[i] = normalizeValue(x, depth+1)
		}
		return t
	}
	return v
}
