package claudecfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// node is an order-preserving JSON value. Objects keep their keys in file
// order (duplicates included), and scalars keep their exact raw bytes, so a
// round trip never reorders keys, rewrites numbers or changes string escapes.
// Only whitespace may change, to the 2-space layout Claude Code itself writes.
type node struct {
	kind byte // 'o' object, 'a' array, 's' scalar (string, number, bool, null)
	keys []string
	raws [][]byte // raw key bytes (with quotes), parallel to keys
	vals []*node
	raw  []byte
}

func parseJSON(data []byte) (*node, error) {
	if !json.Valid(data) {
		return nil, errors.New("not valid JSON (settings files must be strict JSON)")
	}
	p := &parser{b: data}
	n, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.b) {
		return nil, errors.New("trailing data after JSON value")
	}
	return n, nil
}

type parser struct {
	b []byte
	i int
}

func (p *parser) ws() {
	for p.i < len(p.b) && (p.b[p.i] == ' ' || p.b[p.i] == '\t' || p.b[p.i] == '\n' || p.b[p.i] == '\r') {
		p.i++
	}
}

func (p *parser) value(depth int) (*node, error) {
	if depth > 512 {
		return nil, errors.New("JSON nested too deeply")
	}
	p.ws()
	if p.i >= len(p.b) {
		return nil, errors.New("unexpected end of JSON")
	}
	switch p.b[p.i] {
	case '{':
		p.i++
		n := &node{kind: 'o'}
		p.ws()
		if p.b[p.i] == '}' {
			p.i++
			return n, nil
		}
		for {
			p.ws()
			kraw := p.str()
			var k string
			if err := json.Unmarshal(kraw, &k); err != nil {
				return nil, fmt.Errorf("bad key: %w", err)
			}
			p.ws()
			p.i++ // ':'
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, k)
			n.raws = append(n.raws, kraw)
			n.vals = append(n.vals, v)
			p.ws()
			if p.b[p.i] == ',' {
				p.i++
				continue
			}
			p.i++ // '}'
			return n, nil
		}
	case '[':
		p.i++
		n := &node{kind: 'a'}
		p.ws()
		if p.b[p.i] == ']' {
			p.i++
			return n, nil
		}
		for {
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.vals = append(n.vals, v)
			p.ws()
			if p.b[p.i] == ',' {
				p.i++
				continue
			}
			p.i++ // ']'
			return n, nil
		}
	case '"':
		return &node{kind: 's', raw: p.str()}, nil
	default:
		start := p.i
		for p.i < len(p.b) && !strings.ContainsRune(",}] \t\r\n", rune(p.b[p.i])) {
			p.i++
		}
		return &node{kind: 's', raw: p.b[start:p.i]}, nil
	}
}

// str consumes a string literal (the input is already known to be valid).
func (p *parser) str() []byte {
	start := p.i
	p.i++
	for p.b[p.i] != '"' {
		if p.b[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	p.i++
	return p.b[start:p.i]
}

// encode writes n in JSON.stringify(v, null, indent) style.
func (n *node) encode(indent string) []byte {
	var b bytes.Buffer
	n.write(&b, indent, 0)
	return b.Bytes()
}

func (n *node) write(b *bytes.Buffer, indent string, depth int) {
	pad := func(d int) {
		b.WriteByte('\n')
		for i := 0; i < d; i++ {
			b.WriteString(indent)
		}
	}
	switch n.kind {
	case 'o':
		if len(n.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i := range n.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			pad(depth + 1)
			b.Write(n.raws[i])
			b.WriteString(": ")
			n.vals[i].write(b, indent, depth+1)
		}
		pad(depth)
		b.WriteByte('}')
	case 'a':
		if len(n.vals) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, v := range n.vals {
			if i > 0 {
				b.WriteByte(',')
			}
			pad(depth + 1)
			v.write(b, indent, depth+1)
		}
		pad(depth)
		b.WriteByte(']')
	default:
		b.Write(n.raw)
	}
}

// get returns the last value for key (JSON.parse semantics), or nil.
func (n *node) get(key string) *node {
	if n == nil || n.kind != 'o' {
		return nil
	}
	for i := len(n.keys) - 1; i >= 0; i-- {
		if n.keys[i] == key {
			return n.vals[i]
		}
	}
	return nil
}

// set appends key at the end of the object (or replaces its last value).
func (n *node) set(key string, v *node) {
	for i := len(n.keys) - 1; i >= 0; i-- {
		if n.keys[i] == key {
			n.vals[i] = v
			return
		}
	}
	kraw, _ := json.Marshal(key)
	n.keys = append(n.keys, key)
	n.raws = append(n.raws, kraw)
	n.vals = append(n.vals, v)
}

// del removes every occurrence of key.
func (n *node) del(key string) {
	for i := len(n.keys) - 1; i >= 0; i-- {
		if n.keys[i] == key {
			n.keys = append(n.keys[:i], n.keys[i+1:]...)
			n.raws = append(n.raws[:i], n.raws[i+1:]...)
			n.vals = append(n.vals[:i], n.vals[i+1:]...)
		}
	}
}

// str returns a scalar's string value, or "".
func (n *node) strVal() string {
	if n == nil || n.kind != 's' || len(n.raw) == 0 || n.raw[0] != '"' {
		return ""
	}
	var s string
	_ = json.Unmarshal(n.raw, &s)
	return s
}

func (n *node) isTrue() bool { return n != nil && n.kind == 's' && string(n.raw) == "true" }

// fromValue builds a node from a Go value (used for Muretto's own entries).
func fromValue(v any) *node {
	switch t := v.(type) {
	case orderedObj:
		n := &node{kind: 'o'}
		for _, kv := range t {
			n.set(kv.k, fromValue(kv.v))
		}
		return n
	case []string:
		n := &node{kind: 'a'}
		for _, s := range t {
			n.vals = append(n.vals, fromValue(s))
		}
		return n
	case []any:
		n := &node{kind: 'a'}
		for _, e := range t {
			n.vals = append(n.vals, fromValue(e))
		}
		return n
	default:
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		return &node{kind: 's', raw: bytes.TrimRight(b.Bytes(), "\n")}
	}
}

type kv struct {
	k string
	v any
}

type orderedObj []kv

// detectIndent returns the indentation unit used by a JSON file (2 spaces
// when it can't tell).
func detectIndent(data []byte) string {
	for _, line := range bytes.Split(data, []byte("\n"))[1:] {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) == 0 || len(trimmed) == len(line) {
			continue
		}
		ws := line[:len(line)-len(trimmed)]
		if ws[0] == '\t' {
			return "\t"
		}
		return string(ws)
	}
	return "  "
}
