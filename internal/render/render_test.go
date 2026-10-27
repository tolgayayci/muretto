package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/answer"
	"github.com/tolgayayci/muretto/internal/contract"
	tf "github.com/tolgayayci/muretto/internal/testfixture"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fixtureAnswer() contract.Answer {
	return answer.Compute(answer.Input{
		Developer: tf.Developer(), DeveloperNames: tf.Names(), Sessions: tf.Sessions(), Actions: tf.Actions(),
		Resources: tf.Resources(), Findings: tf.Findings(), Costs: tf.Costs(), Checks: tf.Checks(),
		Reviews: tf.Reviews(), Hooks: &answer.HookState{Installed: true}, Accounts: 2,
	}, tf.Now)
}

func opts() Options { return Options{Now: tf.Now, Loc: time.UTC} }

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/render -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestGoldenViews(t *testing.T) {
	a := fixtureAnswer()
	table := "supabase.table:" + tf.DevRef + "/public.customers"
	views := map[string]func(*bytes.Buffer) error{
		"summary":     func(b *bytes.Buffer) error { return Summary(b, a, opts()) },
		"sessions":    func(b *bytes.Buffer) error { return Sessions(b, a, opts()) },
		"session":     func(b *bytes.Buffer) error { return Session(b, a, tf.AliceSession, opts()) },
		"session_bob": func(b *bytes.Buffer) error { return Session(b, a, tf.BobSession, opts()) },
		"resources":   func(b *bytes.Buffer) error { return Resources(b, a, opts()) },
		"show":        func(b *bytes.Buffer) error { return Show(b, a, table, opts()) },
		"show_unknown": func(b *bytes.Buffer) error {
			return Show(b, a, "supabase.project:"+tf.ProdRef, opts())
		},
		"why": func(b *bytes.Buffer) error { return Why(b, a, table, opts()) },
		"why_unknown": func(b *bytes.Buffer) error {
			return Why(b, a, "supabase.project:"+tf.ProdRef, opts())
		},
		"exposed": func(b *bytes.Buffer) error { return Exposed(b, a, opts()) },
		"fix": func(b *bytes.Buffer) error {
			return Fix(b, a, table, &contract.Fix{Kind: contract.FixKindSql, Text: `DROP TABLE "public"."customers";`}, opts())
		},
		"review": func(b *bytes.Buffer) error { return Review(b, a, opts()) },
	}
	for name, f := range views {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := f(&b); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(b.String(), "\x1b[") {
				t.Fatal("color without Options.Color")
			}
			golden(t, name, b.Bytes())
		})
	}
}

func TestSessionTargetLook(t *testing.T) {
	var b bytes.Buffer
	if err := Session(&b, fixtureAnswer(), tf.AliceSession, opts()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"Session 8f2a · alice · Claude Code · Oct 19 14:02–14:31",
		`Task: "add customer import with file uploads"`,
		"Repo: acme/web (feat/import)",
		"  SUPABASE  project acme-dev",
		"  ✓ table      customers        exact   ⚠ RLS off: publicly readable",
		"  ✓ bucket     customer-files   exact   ⚠ public",
		"  ✓ function   process-import   exact",
		"  ✓ branch     import-test      exact   7 days old · review due",
		"  ✓ deployment preview-a81f     exact   ⚠ public preview",
		"3 exposed · 1 needs review · checked 3 min ago",
		"→ muretto why customers   → muretto fix customers",
	} {
		if !strings.Contains(out, want+"\n") && !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestColorOnlyRedForExposed(t *testing.T) {
	o := opts()
	o.Color = true
	var b bytes.Buffer
	if err := Session(&b, fixtureAnswer(), tf.AliceSession, o); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, ansiRed) && !strings.Contains(line, "⚠") && !strings.Contains(line, "exposed") {
			t.Errorf("red on a non-exposure line: %q", line)
		}
	}
	if !strings.Contains(out, ansiRed+"⚠ RLS off") {
		t.Error("exposed finding should be red")
	}
}

func TestColorEnabled(t *testing.T) {
	env := map[string]string{"TERM": "xterm"}
	get := func(k string) string { return env[k] }
	if !ColorEnabled(true, get, false, false) {
		t.Fatal("tty should color")
	}
	for _, c := range []struct {
		tty, json, noColor bool
		env                map[string]string
	}{
		{false, false, false, map[string]string{}},
		{true, true, false, map[string]string{}},
		{true, false, true, map[string]string{}},
		{true, false, false, map[string]string{"NO_COLOR": "1"}},
		{true, false, false, map[string]string{"TERM": "dumb"}},
	} {
		e := c.env
		if ColorEnabled(c.tty, func(k string) string { return e[k] }, c.json, c.noColor) {
			t.Errorf("should not color: %+v", c)
		}
	}
	// NO_COLOR set but empty does not disable color (no-color.org).
	env["NO_COLOR"] = ""
	if !ColorEnabled(true, get, false, false) {
		t.Fatal("empty NO_COLOR must not disable color")
	}
}

func TestNothingFoundOnlyAfterSuccessfulCheck(t *testing.T) {
	a := fixtureAnswer()
	empty := answer.Subset(a, func(contract.Resource) bool { return false }, func(contract.SessionView) bool { return false })
	var b bytes.Buffer
	_ = Resources(&b, empty, opts())
	if !strings.HasPrefix(b.String(), "No successful check yet") {
		t.Fatalf("no checks in subset: %q", b.String())
	}
	empty.Checks = a.Checks
	b.Reset()
	_ = Resources(&b, empty, opts())
	if !strings.HasPrefix(b.String(), "Nothing found") {
		t.Fatalf("with ok checks: %q", b.String())
	}
	none := a
	none.Findings, none.Checks = nil, nil
	b.Reset()
	_ = Exposed(&b, none, opts())
	if strings.Contains(b.String(), "Nothing exposed") {
		t.Fatal("must not say nothing exposed without a successful rule check")
	}
}

// Adapter titles name the resource ("RLS off: public.customers is publicly
// readable"); the session row already does, so it shows the short label.
func TestSessionShortExposureLabels(t *testing.T) {
	in := answer.Input{
		Developer: tf.Developer(), DeveloperNames: tf.Names(), Sessions: tf.Sessions(), Actions: tf.Actions(),
		Resources: tf.Resources(), Findings: tf.Findings(), Checks: tf.Checks(), Accounts: 2,
	}
	adapterTitles := map[string]string{
		"rls_disabled": "RLS off: public.customers is publicly readable", "public_bucket": "Public bucket: customer-files",
		"public_preview": "Public preview: preview-a81f.vercel.app",
	}
	for i, f := range in.Findings {
		in.Findings[i].Title = adapterTitles[f.RuleID]
	}
	var b bytes.Buffer
	if err := Session(&b, answer.Compute(in, tf.Now), tf.AliceSession, opts()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exact   ⚠ RLS off: publicly readable ·", "exact   ⚠ public ·", "exact   ⚠ public preview ·"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
	for _, long := range adapterTitles {
		if strings.Contains(b.String(), long) {
			t.Errorf("session row repeats the adapter title %q", long)
		}
	}
	if got := ShortFinding(contract.ExposureFinding{RuleID: "new_rule", Title: "Something"}); got != "Something" {
		t.Errorf("unknown rule keeps its title, got %q", got)
	}
}

// A failed rule check: the not-confirmed row says exposure is unknown and
// the footer says a check failed, instead of looking clean.
func TestSessionShowsFailedChecks(t *testing.T) {
	checks := tf.Checks()
	for i, c := range checks {
		if c.RuleID != nil && *c.RuleID == "rls_disabled" {
			code, msg := contract.ProviderCheckErrorCodeProvider, "HTTP 500"
			checks[i].Status, checks[i].ErrorCode, checks[i].Error, checks[i].LastSuccessAt = contract.ProviderCheckStatusError, &code, &msg, nil
		}
	}
	a := answer.Compute(answer.Input{
		Developer: tf.Developer(), DeveloperNames: tf.Names(), Sessions: tf.Sessions(), Actions: tf.Actions(),
		Resources: tf.Resources(), Checks: checks, Accounts: 2,
	}, tf.Now)
	var b bytes.Buffer
	if err := Session(&b, a, tf.BobSession, opts()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"? table      orders           unknown in acme-dev · exposure unknown\n", "  1 check failed · checked"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
}

func TestUnknownCostIsNeverZero(t *testing.T) {
	var b bytes.Buffer
	_ = Show(&b, fixtureAnswer(), "supabase.function:"+tf.DevRef+"/process-import", opts())
	if strings.Contains(b.String(), "$0") || !strings.Contains(b.String(), "unknown · no cost data available") {
		t.Fatalf("cost line:\n%s", b.String())
	}
}

// TestShowCreatorUnknownOnce: an unknown attribution's reason starts with
// "creator unknown: ", and Show must not print "unknown · creator unknown".
func TestShowCreatorUnknownOnce(t *testing.T) {
	a := fixtureAnswer()
	sawNoRecord := false
	for _, at := range a.Attributions {
		var b bytes.Buffer
		if err := Show(&b, a, at.ResourceID, opts()); err != nil {
			t.Fatal(err)
		}
		out := b.String()
		if strings.Contains(out, "unknown · creator unknown") {
			t.Errorf("%s: creator unknown said twice:\n%s", at.ResourceID, out)
		}
		if strings.Contains(out, "Creator: unknown · no recorded agent session created it") {
			sawNoRecord = true
		}
	}
	if !sawNoRecord {
		t.Error("no fixture resource showed the no-record reason")
	}
}
