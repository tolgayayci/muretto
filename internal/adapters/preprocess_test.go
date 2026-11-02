package adapters

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Supabase function created_at is an int64 whose unit is unverified (ms or s,
// adapters/supabase/research.md). format auto must read both as the same instant,
// so the created_at veto works for redeployed functions either way.
func TestParseTimeAutoUnits(t *testing.T) {
	want := time.Date(2025, 10, 24, 10, 0, 0, 0, time.UTC)
	for _, v := range []string{"1761300000000", "1761300000", "2025-10-24T10:00:00Z"} {
		got, ok := parseTime(v, "auto")
		if !ok || !got.Equal(want) {
			t.Errorf("parseTime(%s, auto) = %v, %v; want %v", v, got, ok, want)
		}
	}
}

func TestLexSQL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"create table users (id int)", "create table users (id int)"},
		{"select 1 -- create table users\n", "select 1  \n"},
		{"select 1 -- create table users", "select 1  "},
		{"select /* create table a */ 1", "select   1"},
		{"/* outer /* create table a */ still comment */ x", "  x"},
		{"/* unterminated create table a", " "},
		{"select 'create table users'", "select 'create%20table%20users'"},
		{"select 'it''s; create table x'", "select 'it%27s%3B%20create%20table%20x'"},
		{`select E'a\' create table x'`, "select 'a%27%20create%20table%20x'"},
		{`select e'\\' , 'b'`, "select '%5C' , 'b'"},
		{"select 'a\\' create table x", "select 'a%5C' create table x"}, // standard strings: backslash is literal
		{"select $$ create table x $$", "select '%20create%20table%20x%20'"},
		{"select $fn$ a $$ create table x $$ b $fn$ y", "select '%20a%20%24%24%20create%20table%20x%20%24%24%20b%20' y"},
		{"select $1, a$b$c", "select $1, a$b$c"},
		{"select $$ unterminated create table x", "select '%20unterminated%20create%20table%20x'"},
		{`create table "My ""Tbl"""`, `create table "My%20%22Tbl%22"`},
		{`create table "Orders" (x int)`, `create table "Orders" (x int)`},
		{`select 1 as "create table x"`, `select 1 as "create%20table%20x"`},
		{`select "a--b", '/*' , 1`, `select "a--b", '%2F%2A' , 1`},
		{"insert into storage.buckets (id) values ('customer-files')", "insert into storage.buckets (id) values ('customer-files')"},
		{"select 'unterminated create table x", "select 'unterminated%20create%20table%20x'"},
		{"select some'x'", "select some'x'"},
	}
	for _, c := range cases {
		if got := LexSQL(c.in); got != c.want {
			t.Errorf("LexSQL(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestShellArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		ok   bool
	}{
		{`supabase db query "CREATE TABLE t (id int)" --project-ref abc`, []string{"supabase", "db", "query", "CREATE TABLE t (id int)", "--project-ref", "abc"}, true},
		{`supabase db query 'select '"'"'x'"'"''`, []string{"supabase", "db", "query", "select 'x'"}, true},
		{`a "x \"y\" \$z \n" b\ c`, []string{"a", `x "y" $z \n`, "b c"}, true},
		{"a $'it\\'s' b", []string{"a", "it's", "b"}, true},
		{"cd x && supabase db query \"q\"; echo # create table x", []string{"cd", "x", "supabase", "db", "query", "q", "echo"}, true},
		{"a \\\n b", []string{"a", "b"}, true},
		{`supabase db query "unterminated`, nil, false},
		{`a 'unterminated`, nil, false},
	}
	for _, c := range cases {
		got, ok := ShellArgs(c.in)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ShellArgs(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPreprocessSegments(t *testing.T) {
	got := preprocess([]string{"shell_args", "sql"}, `supabase db query "select 1 -- create table users" --project-ref abc`)
	if strings.Contains(got, "create table") {
		t.Errorf("comment survived: %q", got)
	}
	if !strings.Contains(got, segmentSep+"abc") {
		t.Errorf("words not separated: %q", got)
	}
	if got := preprocess([]string{"shell_args", "sql"}, `supabase db query "create table x`); got != "" {
		t.Errorf("unsplittable command should yield nothing, got %q", got)
	}
	if err := checkPreprocess([]string{"sql", "nope"}); err == nil {
		t.Error("unknown step accepted")
	}
}
