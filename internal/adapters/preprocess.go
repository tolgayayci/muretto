package adapters

import (
	"fmt"
	"strings"
)

// Signature text preprocessing (docs/decisions/0406-proof-of-creation.md).
//
// An extract may list `preprocess` steps that rewrite the text its pattern
// runs on, so a regex only sees the parts of the text that act:
//
//	preprocess: [shell_args, sql]   # from: command; SQL passed as a CLI argument
//	preprocess: sql                 # from: input.query; the text is SQL
//
// The text is a list of segments (one at the start). Each step maps every
// segment to zero or more segments; the result is joined with NUL, which no
// adapter pattern matches, so a match never spans two segments.
//
//   - shell_args splits a POSIX shell command into its words (quotes and
//     escapes removed), one segment per word. Operators (; & | < > ( )) and
//     comments end words and are dropped. A command it cannot split (an
//     unterminated quote) yields no segments: nothing matches.
//   - sql rewrites a segment with the SQL lexer below: comments become a
//     space; string constants and quoted identifiers are re-quoted with
//     their content encoded so no keyword can match inside them.
//
// Steps can only remove or hide text, so a preprocessed pattern can miss a
// creation but never find one the raw text does not contain.
var preprocessors = map[string]func(string) []string{
	"shell_args": func(s string) []string { w, _ := ShellArgs(s); return w },
	"sql":        func(s string) []string { return []string{LexSQL(s)} },
}

const segmentSep = "\x00"

func preprocess(steps []string, text string) string {
	if len(steps) == 0 {
		return text
	}
	segs := []string{text}
	for _, st := range steps {
		f := preprocessors[st]
		var next []string
		for _, s := range segs {
			next = append(next, f(s)...)
		}
		segs = next
	}
	return strings.Join(segs, segmentSep)
}

func checkPreprocess(steps []string) error {
	for _, s := range steps {
		if preprocessors[s] == nil {
			return fmt.Errorf("unknown preprocess step %q (want shell_args or sql)", s)
		}
	}
	return nil
}

// ShellArgs splits a POSIX shell command line into words, the way sh would
// pass them as arguments: whitespace separates words; '…' is literal; "…"
// honours \\ \" \$ \` and line continuations; $'…' honours backslash
// escapes; a backslash outside quotes escapes the next byte. The control
// operators ; & | < > ( ) end a word and are dropped, and # at the start of a
// word starts a comment. Expansions ($VAR, $(…), `…`) are kept as text. ok is
// false (and words nil) when a quote is not closed.
func ShellArgs(s string) (words []string, ok bool) {
	var cur strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, cur.String())
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == ';' || c == '&' || c == '|' || c == '<' || c == '>' || c == '(' || c == ')':
			flush()
		case c == '#' && !inWord:
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '\\':
			if i+1 < len(s) {
				i++
				if s[i] != '\n' { // a line continuation joins, it adds nothing
					inWord = true
					cur.WriteByte(s[i])
				}
			}
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			inWord = true
			i += 2
			closed := false
			for ; i < len(s); i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					cur.WriteByte(s[i])
					continue
				}
				if s[i] == '\'' {
					closed = true
					break
				}
				cur.WriteByte(s[i])
			}
			if !closed {
				return nil, false
			}
		case c == '"':
			inWord = true
			i++
			closed := false
			for ; i < len(s); i++ {
				if s[i] == '\\' && i+1 < len(s) {
					switch s[i+1] {
					case '\\', '"', '$', '`':
						i++
						cur.WriteByte(s[i])
						continue
					case '\n':
						i++
						continue
					}
				}
				if s[i] == '"' {
					closed = true
					break
				}
				cur.WriteByte(s[i])
			}
			if !closed {
				return nil, false
			}
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	flush()
	return words, true
}

// LexSQL rewrites PostgreSQL text so that keyword patterns match only real
// SQL tokens:
//
//   - -- line comments and /* block comments */ (which nest in Postgres)
//     become one space;
//   - string constants ('…' where a doubled quote escapes, E'…' with
//     backslash escapes, and $$…$$ / $tag$…$tag$ dollar quotes) become
//     '<content>';
//   - quoted identifiers ("…", doubled quote escapes) become "<content>";
//
// where <content> is the constant's value with every byte outside
// [A-Za-z0-9_.-] percent-encoded (a space is %20, a quote %27 or %22). An
// encoded constant holds no whitespace, quotes, parentheses or semicolons, so
// `select 'create table users'` cannot match a CREATE TABLE pattern, while a
// plain bucket id or "Table" name reads the same as before. An unterminated
// constant or comment runs to the end of the text.
func LexSQL(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	identByte := func(c byte) bool {
		return c == '_' || c == '$' || c >= 0x80 || (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'z')
	}
	prevIdent := func(i int) bool { return i > 0 && identByte(s[i-1]) }
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				i = len(s)
			} else {
				i += j
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			depth := 0
			for i < len(s) {
				if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
			b.WriteByte(' ')
		case (c == 'e' || c == 'E') && i+1 < len(s) && s[i+1] == '\'' && !prevIdent(i):
			val, n := quoted(s[i+1:], '\'', true)
			writeConst(&b, '\'', val)
			i += 1 + n
		case c == '\'':
			val, n := quoted(s[i:], '\'', false)
			writeConst(&b, '\'', val)
			i += n
		case c == '"':
			val, n := quoted(s[i:], '"', false)
			writeConst(&b, '"', val)
			i += n
		case c == '$' && !prevIdent(i):
			if tag, ok := dollarTag(s[i:]); ok {
				body := s[i+len(tag):]
				end := strings.Index(body, tag)
				if end < 0 {
					writeConst(&b, '\'', body)
					i = len(s)
				} else {
					writeConst(&b, '\'', body[:end])
					i += len(tag) + end + len(tag)
				}
				continue
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// quoted reads a constant starting at s[0] == q, doubling q as its escape
// (and backslash escapes when backslash is set). It returns the value and
// the number of bytes consumed (all of s when the constant is unterminated).
func quoted(s string, q byte, backslash bool) (string, int) {
	var v strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if backslash && c == '\\' && i+1 < len(s) {
			i++
			v.WriteByte(s[i])
			continue
		}
		if c == q {
			if i+1 < len(s) && s[i+1] == q {
				v.WriteByte(q)
				i++
				continue
			}
			return v.String(), i + 1
		}
		v.WriteByte(c)
	}
	return v.String(), len(s)
}

// dollarTag returns the opening "$tag$" (or "$$") at the start of s.
func dollarTag(s string) (string, bool) {
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '$' {
			return s[:i+1], true
		}
		letter := c == '_' || c >= 0x80 || (c|0x20 >= 'a' && c|0x20 <= 'z')
		if !letter && !(i > 1 && c >= '0' && c <= '9') {
			return "", false
		}
	}
	return "", false
}

func writeConst(b *strings.Builder, q byte, val string) {
	const hex = "0123456789ABCDEF"
	b.WriteByte(q)
	for i := 0; i < len(val); i++ {
		c := val[i]
		if c == '_' || c == '.' || c == '-' || (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'z') {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	b.WriteByte(q)
}
