package adapters

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// tri is a three-valued truth value (Kleene logic). Exposure rules must be able
// to say "I don't know" when an input is missing, so predicates never collapse
// a missing field to false.
type tri int8

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

func triOf(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

// predicate is the small, reusable set of checks adapters use for item filters
// (where, unavailable_if) and exposure rules (applies, exposed, not_exposed).
// Exactly one form is set:
//
//	all: [p...]            true if every p is true (false if any is false)
//	any: [p...]            true if some p is true (false if every p is false)
//	not: p
//	field: <path> plus exactly one test:
//	  present: bool        the field exists (null counts as present); never unknown
//	  is_null: bool
//	  equals: <scalar>
//	  in: [<scalar>...]
//	  prefix_any: [str...] string value starts with one of these
//	  matches: <regex>     RE2, unanchored unless you anchor it
//	  contains_any: [str]  array value (or comma-separated string) contains one
//	  in_field: <path>     value is an element of another field's comma list
//	  older_than: <age>    a timestamp (RFC 3339 or epoch) more than <age> before
//	                       the scan (30m, 12h, 30d, 2w); newer_than is the opposite
//	  due_within: <age>    a timestamp at most <age> after the scan (or already past)
//
// A test on a missing field is unknown. A null value is false for every test
// except is_null and present.
type predicate struct {
	All   []*predicate
	Any   []*predicate
	Not   *predicate
	Field string

	op          string
	values      []string // equals (one), in, prefix_any, contains_any
	boolArg     bool     // present, is_null
	re          *regexp.Regexp
	inFieldPath string
	age         time.Duration // older_than, newer_than
}

var predicateTests = []string{"present", "is_null", "equals", "in", "prefix_any", "matches", "contains_any", "in_field", "older_than", "newer_than", "due_within"}

// nowKey holds the scan time in predicate contexts. It cannot collide with a
// field name from an adapter file.
const nowKey = "\x00now"

var reAge = regexp.MustCompile(`^([1-9][0-9]{0,5})(m|h|d|w)$`)

func parseAge(s string) (time.Duration, bool) {
	m := reAge.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, true
}

func (p *predicate) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: a predicate must be a mapping", n.Line)
	}
	keys := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys[n.Content[i].Value] = n.Content[i+1]
	}
	forms := 0
	if v, ok := keys["all"]; ok {
		forms++
		if err := v.Decode(&p.All); err != nil {
			return err
		}
		if len(p.All) == 0 {
			return fmt.Errorf("line %d: all needs at least one predicate", v.Line)
		}
	}
	if v, ok := keys["any"]; ok {
		forms++
		if err := v.Decode(&p.Any); err != nil {
			return err
		}
		if len(p.Any) == 0 {
			return fmt.Errorf("line %d: any needs at least one predicate", v.Line)
		}
	}
	if v, ok := keys["not"]; ok {
		forms++
		p.Not = &predicate{}
		if err := v.Decode(p.Not); err != nil {
			return err
		}
	}
	if v, ok := keys["field"]; ok {
		forms++
		p.Field = v.Value
		tests := 0
		for _, t := range predicateTests {
			arg, ok := keys[t]
			if !ok {
				continue
			}
			tests++
			p.op = t
			if err := p.decodeTest(t, arg); err != nil {
				return err
			}
		}
		if tests != 1 {
			return fmt.Errorf("line %d: field %q needs exactly one test (%s)", n.Line, p.Field, strings.Join(predicateTests, ", "))
		}
	}
	if forms != 1 {
		return fmt.Errorf("line %d: a predicate needs exactly one of all, any, not, field", n.Line)
	}
	for k := range keys {
		if k == "all" || k == "any" || k == "not" || k == "field" {
			continue
		}
		known := false
		for _, t := range predicateTests {
			known = known || k == t
		}
		if !known || p.Field == "" {
			return fmt.Errorf("line %d: unknown predicate key %q", n.Line, k)
		}
	}
	return nil
}

func (p *predicate) decodeTest(t string, arg *yaml.Node) error {
	switch t {
	case "present", "is_null":
		return arg.Decode(&p.boolArg)
	case "equals":
		if arg.Kind != yaml.ScalarNode {
			return fmt.Errorf("line %d: equals takes a scalar", arg.Line)
		}
		p.values = []string{arg.Value}
	case "in", "prefix_any", "contains_any":
		var vs []string
		if err := arg.Decode(&vs); err != nil {
			return fmt.Errorf("line %d: %s takes a list of scalars", arg.Line, t)
		}
		p.values = vs
	case "matches":
		re, err := regexp.Compile(arg.Value)
		if err != nil {
			return fmt.Errorf("line %d: %w", arg.Line, err)
		}
		p.re = re
	case "in_field":
		p.inFieldPath = arg.Value
	case "older_than", "newer_than", "due_within":
		d, ok := parseAge(arg.Value)
		if !ok {
			return fmt.Errorf("line %d: %s takes an age like 30m, 12h, 30d or 2w", arg.Line, t)
		}
		p.age = d
	}
	return nil
}

func (p *predicate) eval(ctx map[string]any) tri {
	if p == nil {
		return triTrue
	}
	switch {
	case p.All != nil:
		res := triTrue
		for _, c := range p.All {
			switch c.eval(ctx) {
			case triFalse:
				return triFalse
			case triUnknown:
				res = triUnknown
			case triTrue:
			}
		}
		return res
	case p.Any != nil:
		res := triFalse
		for _, c := range p.Any {
			switch c.eval(ctx) {
			case triTrue:
				return triTrue
			case triUnknown:
				res = triUnknown
			case triFalse:
			}
		}
		return res
	case p.Not != nil:
		switch p.Not.eval(ctx) {
		case triTrue:
			return triFalse
		case triFalse:
			return triTrue
		case triUnknown:
			fallthrough
		default:
			return triUnknown
		}
	}
	v, found := lookup(ctx, p.Field)
	if p.op == "present" {
		return triOf(found == p.boolArg)
	}
	if !found {
		return triUnknown
	}
	if p.op == "is_null" {
		return triOf((v == nil) == p.boolArg)
	}
	if v == nil {
		return triFalse
	}
	switch p.op {
	case "equals", "in":
		s, ok := scalarString(v)
		if !ok {
			return triFalse
		}
		for _, want := range p.values {
			if s == want {
				return triTrue
			}
		}
		return triFalse
	case "prefix_any":
		s, _ := scalarString(v)
		for _, pre := range p.values {
			if strings.HasPrefix(s, pre) {
				return triTrue
			}
		}
		return triFalse
	case "matches":
		s, _ := scalarString(v)
		return triOf(p.re.MatchString(s))
	case "contains_any":
		for _, el := range listOf(v) {
			for _, want := range p.values {
				if el == want {
					return triTrue
				}
			}
		}
		return triFalse
	case "older_than", "newer_than", "due_within":
		now, ok := ctx[nowKey].(time.Time)
		if !ok || now.IsZero() {
			now = time.Now().UTC()
		}
		s, _ := scalarString(v)
		ts, ok := parseTime(s, "auto")
		if !ok {
			return triUnknown // not a timestamp: never guess
		}
		if p.op == "due_within" {
			return triOf(ts.Sub(now) <= p.age) // already past counts as due
		}
		older := now.Sub(ts) > p.age
		if p.op == "older_than" {
			return triOf(older)
		}
		return triOf(!older)
	case "in_field":
		other, ok := lookup(ctx, p.inFieldPath)
		if !ok {
			return triUnknown
		}
		if other == nil {
			return triFalse
		}
		s, _ := scalarString(v)
		for _, el := range listOf(other) {
			if el == s {
				return triTrue
			}
		}
		return triFalse
	}
	return triUnknown
}

// listOf turns an array or a comma-separated string into trimmed elements.
func listOf(v any) []string {
	var out []string
	if arr, ok := v.([]any); ok {
		for _, el := range arr {
			if s, ok := scalarString(el); ok {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	s, ok := scalarString(v)
	if !ok || s == "" {
		return nil
	}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
