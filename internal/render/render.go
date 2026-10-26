// Package render prints an Answer as calm, short text or as JSON. Every
// printed string goes through redact.String. Red is used only for exposed
// findings; color is used only when Options.Color is set (see ColorEnabled).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/answer"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/redact"
)

// Options control text output.
type Options struct {
	Color bool
	Now   time.Time
	Loc   *time.Location
}

// ColorEnabled applies the no-color rules: a TTY, NO_COLOR unset or empty,
// TERM not dumb, no --json and no --no-color.
func ColorEnabled(isTTY bool, getenv func(string) string, jsonOut, noColor bool) bool {
	return isTTY && getenv("NO_COLOR") == "" && getenv("TERM") != "dumb" && !jsonOut && !noColor
}

// JSON writes v as indented JSON. Answers are already redacted in their
// free-text fields by answer.Compute.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
)

type printer struct {
	w   io.Writer
	o   Options
	err error
}

func newPrinter(w io.Writer, o Options) *printer {
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	return &printer{w: w, o: o}
}

// line prints one redacted line.
func (p *printer) line(format string, a ...any) {
	if p.err != nil {
		return
	}
	s := format
	if len(a) > 0 {
		s = fmt.Sprintf(format, a...)
	}
	_, p.err = io.WriteString(p.w, redact.String(s)+"\n")
}

func (p *printer) wrap(code, s string) string {
	if !p.o.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (p *printer) red(s string) string  { return p.wrap(ansiRed, s) }
func (p *printer) dim(s string) string  { return p.wrap(ansiDim, s) }
func (p *printer) bold(s string) string { return p.wrap(ansiBold, s) }

// ---- shared helpers ----

type index struct {
	a        contract.Answer
	res      map[string]contract.Resource
	att      map[string]contract.Attribution
	findings map[string][]contract.ExposureFinding
	costs    map[string][]contract.CostRecord
	review   map[string]contract.ReviewItem
	sessions map[string]contract.SessionView
}

func newIndex(a contract.Answer) *index {
	ix := &index{a: a, res: map[string]contract.Resource{}, att: map[string]contract.Attribution{},
		findings: map[string][]contract.ExposureFinding{}, costs: map[string][]contract.CostRecord{},
		review: map[string]contract.ReviewItem{}, sessions: map[string]contract.SessionView{}}
	for _, r := range a.Resources {
		ix.res[r.ID] = r
	}
	for _, x := range a.Attributions {
		ix.att[x.ResourceID] = x
	}
	for _, f := range a.Findings {
		ix.findings[f.ResourceID] = append(ix.findings[f.ResourceID], f)
	}
	for _, c := range a.Costs {
		ix.costs[c.ResourceID] = append(ix.costs[c.ResourceID], c)
	}
	for _, r := range a.Reviews {
		ix.review[r.ResourceID] = r
	}
	for _, s := range a.Sessions {
		ix.sessions[s.Session.ID] = s
	}
	return ix
}

func (ix *index) exposed(id string) []contract.ExposureFinding {
	var out []contract.ExposureFinding
	for _, f := range ix.findings[id] {
		if f.State == contract.ExposureFindingStateExposed {
			out = append(out, f)
		}
	}
	return out
}

func (ix *index) unknownExposure(id string) bool {
	for _, f := range ix.findings[id] {
		if f.State == contract.ExposureFindingStateUnknown {
			return true
		}
	}
	return false
}

func (ix *index) reviewDue(id string, now time.Time) bool {
	r, ok := ix.review[id]
	return ok && r.State == contract.ReviewItemStatePending && !r.DueAt.After(now)
}

// claimant returns the single session in an unknown attribution's evidence,
// for evidence-only resources seen in one session's output.
func (ix *index) claimant(id string) string {
	at := ix.att[id]
	s := ""
	for _, e := range at.Evidence {
		if s != "" && e.SessionID != s {
			return ""
		}
		s = e.SessionID
	}
	return s
}

// ShortKind turns supabase.env_var into "env var".
func ShortKind(kind string) string {
	if _, k, ok := strings.Cut(kind, "."); ok {
		return strings.ReplaceAll(k, "_", " ")
	}
	return kind
}

// DisplayName is the name used in text output and accepted as an argument.
func DisplayName(r contract.Resource) string {
	if r.Name != "" {
		if r.Kind == "supabase.table" {
			return strings.TrimPrefix(r.Name, "public.")
		}
		return r.Name
	}
	return r.ProviderID
}

// ShortFinding is the brief label for an exposed finding on a row that
// already names the resource (the session view, brief §10). Adapter titles
// repeat the resource name, e.g. "RLS off: public.customers is publicly
// readable"; unknown rules keep their title.
func ShortFinding(f contract.ExposureFinding) string {
	switch f.RuleID {
	case "rls_disabled":
		return "RLS off: publicly readable"
	case "public_bucket":
		return "public"
	case "public_preview":
		return "public preview"
	case "client_exposed_env":
		return "exposed to the browser"
	}
	return f.Title
}

// ShortSession is the 4-character session label used in text output.
func ShortSession(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

func providerTitle(p string) string { return strings.ToUpper(p) }

func (p *printer) when(t time.Time) string {
	lt := t.In(p.o.Loc)
	now := p.o.Now.In(p.o.Loc)
	y1, m1, d1 := lt.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today " + lt.Format("15:04")
	case now.AddDate(0, 0, -1).Format("2006-01-02") == lt.Format("2006-01-02"):
		return "yesterday " + lt.Format("15:04")
	case y1 == y2:
		return lt.Format("Jan 2 15:04")
	default:
		return lt.Format("Jan 2 2006 15:04")
	}
}

func (p *printer) span(s contract.Session) string {
	start := p.when(s.StartedAt)
	end := s.EndedAt
	if end == nil {
		end = s.LastActivityAt
	}
	if end == nil {
		return start
	}
	if end.In(p.o.Loc).Format("2006-01-02") == s.StartedAt.In(p.o.Loc).Format("2006-01-02") {
		return start + "–" + end.In(p.o.Loc).Format("15:04")
	}
	return start + " – " + p.when(*end)
}

func (p *printer) ago(t time.Time) string {
	d := p.o.Now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	return answer.Age(d) + " ago"
}

func (p *printer) daysOld(t time.Time) string {
	d := int(p.o.Now.Sub(t).Hours() / 24)
	switch d {
	case 0:
		return "today"
	case 1:
		return "1 day old"
	}
	return fmt.Sprintf("%d days old", d)
}

// checkedLine says how old the data is, or that nothing was checked.
func (p *printer) checked(a contract.Answer) string {
	if a.Summary.OldestCheckAt == nil {
		return "not checked yet"
	}
	return "checked " + p.ago(*a.Summary.OldestCheckAt)
}

func anyCheckOK(a contract.Answer, rule bool) bool {
	for _, c := range a.Checks {
		if c.Status == contract.ProviderCheckStatusOk && (c.RuleID != nil) == rule {
			return true
		}
	}
	return false
}

func pad(s string, n int) string {
	if w := len([]rune(s)); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func (ix *index) createdAt(r contract.Resource) time.Time {
	if r.CreatedAt != nil {
		return *r.CreatedAt
	}
	if ev := ix.att[r.ID].Evidence; len(ev) > 0 {
		return ev[0].OccurredAt
	}
	return r.FirstSeenAt
}

func (ix *index) parentName(r contract.Resource) string {
	if r.ParentID == nil {
		return ""
	}
	if pr, ok := ix.res[*r.ParentID]; ok {
		return DisplayName(pr)
	}
	_, pid, _ := strings.Cut(*r.ParentID, ":")
	return pid
}

func (ix *index) sessionLabel(id string) string {
	s := ShortSession(id)
	if v, ok := ix.sessions[id]; ok && v.DeveloperName != "" {
		s += " (" + v.DeveloperName + ")"
	}
	return s
}

// group is a provider section with an optional container header.
type group struct {
	provider string
	header   string
	ids      []string
}

// groupResources groups by provider, then by top-level container (the
// parent, or the resource itself when it is a top-level container).
func (ix *index) groupResources(ids []string) []group {
	var out []group
	pos := map[string]int{}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ix.res[ids[i]], ix.res[ids[j]]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		ka, kb := kindOrder(a.Kind), kindOrder(b.Kind)
		if ka != kb {
			return ka < kb
		}
		return DisplayName(a) < DisplayName(b)
	})
	isParent := map[string]bool{}
	for _, id := range ids {
		if r := ix.res[id]; r.ParentID != nil {
			isParent[*r.ParentID] = true
		}
	}
	for _, id := range ids {
		r := ix.res[id]
		header := ""
		if r.ParentID == nil && isParent[r.ID] {
			header = ShortKind(r.Kind) + " " + DisplayName(r)
		} else if r.ParentID != nil {
			kind, _, _ := strings.Cut(*r.ParentID, ":")
			header = ShortKind(kind) + " " + ix.parentName(r)
		}
		key := r.Provider + "\x00" + header
		i, ok := pos[key]
		if !ok {
			i = len(out)
			pos[key] = i
			out = append(out, group{provider: r.Provider, header: header})
		}
		out[i].ids = append(out[i].ids, id)
	}
	return out
}

var kinds = []string{"project", "table", "bucket", "function", "branch", "deployment", "domain", "env_var"}

func kindOrder(kind string) int {
	_, k, _ := strings.Cut(kind, ".")
	for i, x := range kinds {
		if x == k {
			return i
		}
	}
	return len(kinds)
}

func (p *printer) warnings(a contract.Answer) {
	if len(a.Warnings) == 0 {
		return
	}
	p.line("")
	for _, w := range a.Warnings {
		s := "  ! " + w.Message
		if w.Fix != nil && *w.Fix != "" {
			s += p.dim("  → " + *w.Fix)
		}
		p.line("%s", s)
	}
}

// ---- views ----

// Summary is `muretto`.
func Summary(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	s := a.Summary
	p.line("%s", p.bold("Muretto")+" · "+a.Developer.DisplayName+" · "+p.dim(p.checked(a)))
	p.line("")
	exposed := fmt.Sprintf("%d exposed", s.Exposed)
	if s.Exposed > 0 {
		exposed = p.red(exposed)
	}
	p.line("  %d agent-created %s · %d still exist · %s · %d need%s review",
		s.AgentCreated, plural(s.AgentCreated, "resource", "resources"), s.StillExisting, exposed, s.NeedsReview, plural(s.NeedsReview, "s", ""))
	if s.UnknownCreator > 0 {
		p.line("  %d %s with creator unknown", s.UnknownCreator, plural(s.UnknownCreator, "resource", "resources"))
	}
	if s.ChecksFailed > 0 {
		p.line("  %d %s failed", s.ChecksFailed, plural(s.ChecksFailed, "check", "checks"))
	}
	p.line("")
	if len(a.Sessions) == 0 {
		p.line("  No agent sessions recorded yet.")
	} else {
		p.line("  %s", p.bold("Recent sessions"))
		n := len(a.Sessions)
		if n > 5 {
			n = 5
		}
		p.sessionRows(ix, a.Sessions[:n])
	}
	p.line("")
	var hints []string
	if s.Exposed > 0 {
		hints = append(hints, "→ muretto exposed")
	}
	for _, v := range a.Sessions {
		if len(v.ResourceIds) > 0 {
			hints = append(hints, "→ muretto session "+ShortSession(v.Session.ID))
			break
		}
	}
	hints = append(hints, "→ muretto resources")
	p.line("  %s", strings.Join(hints, "   "))
	p.warnings(a)
	return p.err
}

func (p *printer) sessionRows(ix *index, views []contract.SessionView) {
	for _, v := range views {
		title := "(no title)"
		if v.Session.TaskTitle != nil {
			title = *v.Session.TaskTitle
		}
		if len([]rune(title)) > 42 {
			title = string([]rune(title)[:41]) + "…"
		}
		counts := fmt.Sprintf("%d created", len(v.ResourceIds))
		if v.ExposedCount > 0 {
			counts += " · " + p.red(fmt.Sprintf("%d exposed", v.ExposedCount))
		}
		if v.ReviewDueCount > 0 {
			counts += fmt.Sprintf(" · %d review due", v.ReviewDueCount)
		}
		p.line("  %s  %s  %s  %s  %s", ShortSession(v.Session.ID), pad(p.when(v.Session.StartedAt), 18), pad(v.DeveloperName, 8), pad(title, 42), counts)
	}
}

// Sessions is `muretto sessions`.
func Sessions(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	if len(a.Sessions) == 0 {
		p.line("No agent sessions recorded yet. Run `muretto init`, then use Claude Code as usual.")
		return p.err
	}
	p.line("%s", p.bold(fmt.Sprintf("%d %s", len(a.Sessions), plural(len(a.Sessions), "session", "sessions")))+" · "+p.dim(p.checked(a)))
	p.line("")
	p.sessionRows(newIndex(a), a.Sessions)
	p.line("")
	p.line("  → muretto session <id>")
	return p.err
}

// Session is `muretto session <id>`: the git-log-for-infrastructure view.
func Session(w io.Writer, a contract.Answer, sessionID string, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	v, ok := ix.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s is not in this answer", sessionID)
	}
	s := v.Session
	p.line("%s", p.bold("Session "+ShortSession(s.ID))+" · "+v.DeveloperName+" · Claude Code · "+p.span(s))
	if s.TaskTitle != nil {
		p.line("Task: %q", *s.TaskTitle)
	}
	if s.Repo != nil && s.Repo.DisplayName != nil {
		repo := *s.Repo.DisplayName
		if s.GitBranch != nil && *s.GitBranch != "" {
			repo += " (" + *s.GitBranch + ")"
		}
		p.line("Repo: %s", repo)
	}
	if len(s.Sources) == 1 && s.Sources[0] == contract.SessionSourcesElemTranscript {
		p.line("%s", p.dim("Seen only in transcripts (hooks did not run)."))
	}

	var unconfirmed []string
	for _, r := range a.Resources {
		if r.SeenVia == contract.ResourceSeenViaEvidence && ix.claimant(r.ID) == s.ID {
			unconfirmed = append(unconfirmed, r.ID)
		}
	}
	if len(v.ResourceIds) == 0 && len(unconfirmed) == 0 {
		p.line("")
		p.line("  This session created nothing Muretto can see outside the repo.")
		p.line("  %s", p.dim(p.checked(a)))
		return p.err
	}

	nameW := 16
	for _, id := range append(append([]string{}, v.ResourceIds...), unconfirmed...) {
		if n := len([]rune(DisplayName(ix.res[id]))); n+1 > nameW {
			nameW = n + 1
		}
	}
	exposedN, reviewN := 0, 0
	var firstExposed, firstName string
	for _, g := range ix.groupResources(append([]string{}, v.ResourceIds...)) {
		p.line("")
		h := "  " + p.bold(providerTitle(g.provider))
		if g.header != "" {
			h += "  " + g.header
		}
		p.line("%s", h)
		for _, id := range g.ids {
			r := ix.res[id]
			if firstName == "" {
				firstName = DisplayName(r)
			}
			mark := "✓"
			var notes []string
			if r.Existence == contract.ResourceExistenceGone {
				mark = "✗"
				notes = append(notes, p.dim("gone"))
			}
			for _, f := range ix.exposed(id) {
				notes = append(notes, p.red("⚠ "+ShortFinding(f)))
				exposedN++
				if firstExposed == "" {
					firstExposed = DisplayName(r)
				}
			}
			if ix.reviewDue(id, p.o.Now) {
				notes = append(notes, p.daysOld(ix.createdAt(r))+" · review due")
				reviewN++
			}
			if len(ix.exposed(id)) == 0 && ix.unknownExposure(id) && r.Existence != contract.ResourceExistenceGone {
				notes = append(notes, p.dim("exposure unknown"))
			}
			p.line("%s", strings.TrimRight(fmt.Sprintf("  %s %s %s %s %s", mark, pad(ShortKind(r.Kind), 10), pad(DisplayName(r), nameW), pad(string(ix.att[id].Label), 7), strings.Join(notes, " · ")), " "))
		}
	}
	if len(unconfirmed) > 0 {
		p.line("")
		p.line("  %s", p.bold("NOT CONFIRMED")+p.dim("  seen only in this session's output"))
		for _, id := range unconfirmed {
			r := ix.res[id]
			where := ""
			if pn := ix.parentName(r); pn != "" {
				where = "in " + pn
			} else {
				where = "target unknown"
			}
			notes := []string{p.dim(where)}
			for _, f := range ix.exposed(id) {
				notes = append(notes, p.red("⚠ "+ShortFinding(f)))
				exposedN++
			}
			if len(ix.exposed(id)) == 0 && ix.unknownExposure(id) {
				notes = append(notes, p.dim("exposure unknown"))
			}
			p.line("%s", strings.TrimRight(fmt.Sprintf("  ? %s %s %s %s", pad(ShortKind(r.Kind), 10), pad(DisplayName(r), nameW), pad(string(ix.att[id].Label), 7), strings.Join(notes, " · ")), " "))
		}
	}
	p.line("")
	foot := []string{}
	if exposedN > 0 {
		foot = append(foot, p.red(fmt.Sprintf("%d exposed", exposedN)))
	}
	if reviewN > 0 {
		foot = append(foot, fmt.Sprintf("%d needs review", reviewN))
	}
	if n := a.Summary.ChecksFailed; n > 0 {
		foot = append(foot, fmt.Sprintf("%d %s failed", n, plural(n, "check", "checks")))
	}
	foot = append(foot, p.dim(p.checked(a)))
	p.line("  %s", strings.Join(foot, " · "))
	target := firstExposed
	if target == "" {
		target = firstName
	}
	if target != "" {
		hint := "→ muretto why " + target
		if firstExposed != "" {
			hint += "   → muretto fix " + target
		}
		p.line("  %s", hint)
	}
	return p.err
}

// Resources is `muretto resources`.
func Resources(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	if len(a.Resources) == 0 {
		if anyCheckOK(a, false) {
			p.line("Nothing found · %s", p.checked(a))
		} else {
			p.line("No successful check yet, so Muretto cannot say what exists. → muretto doctor")
		}
		p.warnings(a)
		return p.err
	}
	p.line("%s", p.bold(fmt.Sprintf("%d %s", len(a.Resources), plural(len(a.Resources), "resource", "resources")))+" · "+p.dim(p.checked(a)))
	ids := make([]string, 0, len(a.Resources))
	nameW := 16
	for _, r := range a.Resources {
		ids = append(ids, r.ID)
		if n := len([]rune(DisplayName(r))); n+1 > nameW && n < 40 {
			nameW = n + 1
		}
	}
	for _, g := range ix.groupResources(ids) {
		p.line("")
		h := "  " + p.bold(providerTitle(g.provider))
		if g.header != "" {
			h += "  " + g.header
		}
		p.line("%s", h)
		for _, id := range g.ids {
			r := ix.res[id]
			at := ix.att[id]
			mark := "·"
			creator := p.dim("creator unknown")
			if at.Label == contract.AttributionLabelExact && at.SessionID != nil {
				mark = "✓"
				creator = "session " + ix.sessionLabel(*at.SessionID)
			} else if c := ix.claimant(id); c != "" && r.SeenVia == contract.ResourceSeenViaEvidence {
				creator = p.dim("seen in session " + ix.sessionLabel(c) + " only")
			}
			var notes []string
			switch r.Existence {
			case contract.ResourceExistenceGone:
				mark = "✗"
				notes = append(notes, p.dim("gone"))
			case contract.ResourceExistenceUnconfirmed:
				mark = "?"
				notes = append(notes, p.dim("not confirmed"))
			case contract.ResourceExistenceConfirmed:
			}
			for _, f := range ix.exposed(id) {
				notes = append(notes, p.red("⚠ "+f.Title))
			}
			line := fmt.Sprintf("  %s %s %s %s %s", mark, pad(ShortKind(r.Kind), 10), pad(DisplayName(r), nameW), pad(string(at.Label), 8), creator)
			if len(notes) > 0 {
				line += " · " + strings.Join(notes, " · ")
			}
			p.line("%s", line)
		}
	}
	p.warnings(a)
	return p.err
}

// Show is `muretto show <resource>`.
func Show(w io.Writer, a contract.Answer, resourceID string, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	r, ok := ix.res[resourceID]
	if !ok {
		return fmt.Errorf("resource %s is not in this answer", resourceID)
	}
	at := ix.att[r.ID]
	name := DisplayName(r)
	p.line("%s  %s", p.bold(ShortKind(r.Kind)+" "+name), p.dim(r.ID))
	acct := r.AccountID
	if r.AccountName != nil && *r.AccountName != "" {
		acct = *r.AccountName
	}
	where := r.Provider
	if acct != "" {
		where += " · account " + acct
	}
	if pn := ix.parentName(r); pn != "" {
		where += " · in " + pn
	}
	p.line("Provider: %s", where)
	if r.Url != nil {
		p.line("URL: %s", *r.Url)
	}
	switch r.Existence {
	case contract.ResourceExistenceConfirmed:
		p.line("Status: exists (seen via %s, last seen %s)", r.SeenVia, p.ago(r.LastSeenAt))
	case contract.ResourceExistenceGone:
		g := "unknown time"
		if r.GoneAt != nil {
			g = p.when(*r.GoneAt)
		}
		p.line("Status: gone (missing from a successful check at %s)", g)
	case contract.ResourceExistenceUnconfirmed:
		p.line("Status: not confirmed (known from %s; no check lists this kind)", r.SeenVia)
	}
	if at.Label == contract.AttributionLabelExact && at.SessionID != nil {
		p.line("Creator: session %s · exact", ix.sessionLabel(*at.SessionID))
	} else {
		p.line("Creator: unknown · %s", at.Reason)
	}

	p.line("")
	p.line("%s", p.bold("Timeline"))
	type ev struct {
		t    time.Time
		text string
	}
	var evs []ev
	if at.SessionID != nil {
		if v, ok := ix.sessions[*at.SessionID]; ok {
			t := "session " + ShortSession(v.Session.ID) + " started"
			if v.Session.TaskTitle != nil {
				t += ": " + fmt.Sprintf("%q", *v.Session.TaskTitle)
			}
			evs = append(evs, ev{v.Session.StartedAt, t})
		}
	}
	for _, e := range at.Evidence {
		evs = append(evs, ev{e.OccurredAt, e.ToolName + "  " + e.MatchedValue})
	}
	if r.CreatedAt != nil {
		evs = append(evs, ev{*r.CreatedAt, "created (provider)"})
	}
	if r.SeenVia != contract.ResourceSeenViaEvidence {
		evs = append(evs, ev{r.FirstSeenAt, "first seen by Muretto"})
	}
	if r.GoneAt != nil {
		t := "missing from a successful check"
		if r.Existence != contract.ResourceExistenceGone {
			t += " (seen again later)"
		}
		evs = append(evs, ev{*r.GoneAt, t})
	}
	if rv, ok := ix.review[r.ID]; ok && rv.DecidedAt != nil {
		evs = append(evs, ev{*rv.DecidedAt, "review: " + strings.ReplaceAll(string(rv.State), "_", "-")})
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].t.Before(evs[j].t) })
	for _, e := range evs {
		p.line("  %s  %s", pad(p.when(e.t), 18), e.text)
	}

	p.line("")
	p.line("%s", p.bold("Exposure"))
	fs := ix.findings[r.ID]
	if len(fs) == 0 {
		p.line("  %s", p.dim("no exposure rule applies to this kind"))
	}
	for _, f := range fs {
		chk := "never checked"
		if f.CheckedAt != nil {
			chk = "checked " + p.ago(*f.CheckedAt)
		}
		switch f.State {
		case contract.ExposureFindingStateExposed:
			p.line("  %s  %s", p.red("⚠ "+f.Title), p.dim(chk))
		case contract.ExposureFindingStateNotExposed:
			p.line("  ✓ %s  %s", f.Title, p.dim(chk))
		case contract.ExposureFindingStateUnknown:
			p.line("  ? %s  %s", f.Title, p.dim(chk))
		}
	}

	p.line("")
	p.line("%s", p.bold("Cost"))
	cs := ix.costs[r.ID]
	if len(cs) == 0 {
		p.line("  unknown")
	}
	for _, c := range cs {
		p.line("  %s", costText(c))
	}

	if rv, ok := ix.review[r.ID]; ok {
		p.line("")
		p.line("%s", p.bold("Review"))
		owner := ""
		if rv.OwnerDeveloperID != nil {
			owner = " · owner " + ownerName(ix, *rv.OwnerDeveloperID)
		}
		switch rv.State {
		case contract.ReviewItemStatePending:
			due := "due " + p.when(rv.DueAt)
			if !rv.DueAt.After(p.o.Now) {
				due = "review due"
			}
			p.line("  pending · %s%s", due, owner)
		case contract.ReviewItemStateKeep, contract.ReviewItemStateNotNeeded:
			p.line("  %s%s", strings.ReplaceAll(string(rv.State), "_", "-"), owner)
		}
	}
	p.line("")
	hint := "→ muretto why " + name
	if len(ix.exposed(r.ID)) > 0 {
		hint += "   → muretto fix " + name
	}
	p.line("  %s", hint)
	return p.err
}

func ownerName(ix *index, devID string) string {
	for _, s := range ix.a.Sessions {
		if s.Session.DeveloperID == devID && s.DeveloperName != "" {
			return s.DeveloperName
		}
	}
	if devID == ix.a.Developer.ID {
		return ix.a.Developer.DisplayName
	}
	return ShortSession(devID)
}

func costText(c contract.CostRecord) string {
	if c.Source == contract.CostRecordSourceUnknown || c.AmountMicros == nil {
		return "unknown · " + c.Basis
	}
	cur := "USD"
	if c.Currency != nil {
		cur = *c.Currency
	}
	amt := fmt.Sprintf("%.2f %s", float64(*c.AmountMicros)/1e6, cur)
	if cur == "USD" {
		amt = fmt.Sprintf("$%.2f", float64(*c.AmountMicros)/1e6)
	}
	if c.Interval != nil {
		switch *c.Interval {
		case contract.CostRecordIntervalMonthly:
			amt += "/month"
		case contract.CostRecordIntervalHourly:
			amt += "/hour"
		case contract.CostRecordIntervalPeriod:
		}
	}
	return amt + " · " + string(c.Source) + " · " + c.Basis
}

// Why is `muretto why <resource>`.
func Why(w io.Writer, a contract.Answer, resourceID string, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	r, ok := ix.res[resourceID]
	if !ok {
		return fmt.Errorf("resource %s is not in this answer", resourceID)
	}
	at := ix.att[r.ID]
	p.line("%s: %s", p.bold(ShortKind(r.Kind)+" "+DisplayName(r)), at.Label)
	if at.Label == contract.AttributionLabelExact && at.SessionID != nil {
		rule := ""
		if at.RuleID != nil {
			rule = " (rule " + *at.RuleID + ")"
		}
		p.line("Created by session %s%s", ix.sessionLabel(*at.SessionID), rule)
	}
	p.line("Reason: %s", at.Reason)
	if len(at.Evidence) == 0 {
		p.line("")
		p.line("No agent evidence mentions this resource as created. Muretto never guesses from timing.")
		return p.err
	}
	p.line("")
	title := "Evidence"
	if at.Label != contract.AttributionLabelExact {
		title = "Conflicting or insufficient evidence"
	}
	p.line("%s", p.bold(title))
	for i, e := range at.Evidence {
		p.line("  %d. %s · session %s · %s", i+1, p.when(e.OccurredAt), ix.sessionLabel(e.SessionID), e.ToolName)
		p.line("     matched %s %s", e.MatchedValue, p.dim("("+e.SignatureID+", "+e.ActionID+")"))
		for _, l := range excerptLines(e.Excerpt, 6) {
			p.line("     %s", p.dim("│ "+l))
		}
	}
	return p.err
}

func excerptLines(s string, max int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > max {
		lines = append(lines[:max], fmt.Sprintf("… %d more lines", len(lines)-max))
	}
	for i, l := range lines {
		if r := []rune(l); len(r) > 110 {
			lines[i] = string(r[:109]) + "…"
		}
	}
	return lines
}

// Exposed is `muretto exposed`: exposed findings, worst first.
func Exposed(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	var ex []contract.ExposureFinding
	unknown := map[string]bool{}
	for _, f := range a.Findings {
		switch f.State {
		case contract.ExposureFindingStateExposed:
			ex = append(ex, f)
		case contract.ExposureFindingStateUnknown:
			unknown[f.ResourceID] = true
		case contract.ExposureFindingStateNotExposed:
		}
	}
	sort.SliceStable(ex, func(i, j int) bool {
		return answer.SeverityRank(ex[i].Severity) < answer.SeverityRank(ex[j].Severity)
	})
	switch {
	case len(ex) > 0:
		p.line("%s · %s", p.red(fmt.Sprintf("%d exposed", len(ex))), p.dim(p.checked(a)))
	case anyCheckOK(a, true):
		p.line("Nothing exposed · %s", p.checked(a))
	default:
		p.line("Exposure has not been checked successfully yet, so Muretto cannot say nothing is exposed. → muretto doctor")
	}
	for _, f := range ex {
		r := ix.res[f.ResourceID]
		by := p.dim("creator unknown")
		if at := ix.att[r.ID]; at.Label == contract.AttributionLabelExact && at.SessionID != nil {
			by = "session " + ix.sessionLabel(*at.SessionID)
		}
		p.line("")
		p.line("  %s %s %s %s", p.red(pad(string(f.Severity), 8)), pad(ShortKind(r.Kind), 10), pad(DisplayName(r), 16), p.red("⚠ "+f.Title))
		p.line("    %s · %s", f.Explanation, by)
		if f.Caveat != nil {
			p.line("    %s", p.dim("Caveat: "+*f.Caveat))
		}
		p.line("    → muretto fix %s", DisplayName(r))
	}
	if len(unknown) > 0 {
		p.line("")
		p.line("  %s", p.dim(fmt.Sprintf("Exposure unknown for %d %s (check did not run or failed).", len(unknown), plural(len(unknown), "resource", "resources"))))
	}
	p.warnings(a)
	return p.err
}

// Fix is `muretto fix <resource>`. It prints; Muretto never runs anything.
func Fix(w io.Writer, a contract.Answer, resourceID string, remove *contract.Fix, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	r, ok := ix.res[resourceID]
	if !ok {
		return fmt.Errorf("resource %s is not in this answer", resourceID)
	}
	p.line("%s", p.bold("Fix for "+ShortKind(r.Kind)+" "+DisplayName(r)))
	p.line("%s", p.dim("Muretto never runs these. Review them and run them yourself."))
	ex := ix.exposed(r.ID)
	if len(ex) == 0 {
		p.line("")
		if ix.unknownExposure(r.ID) {
			p.line("  Exposure is unknown (the check did not run), so there is no fix to show yet.")
		} else if len(ix.findings[r.ID]) > 0 {
			p.line("  Nothing exposed (%s).", p.checked(a))
		} else {
			p.line("  No exposure rule applies to this kind.")
		}
	}
	for _, f := range ex {
		p.line("")
		p.line("  %s", p.red("⚠ "+f.Title))
		p.fix(f.Fix)
	}
	if remove != nil {
		p.line("")
		p.line("  %s", p.bold("If you no longer need it, remove it with:"))
		p.fix(*remove)
	}
	return p.err
}

func (p *printer) fix(f contract.Fix) {
	for _, l := range strings.Split(strings.TrimSpace(f.Text), "\n") {
		p.line("    %s", l)
	}
	if f.Note != nil {
		p.line("    %s", p.dim("Note: "+*f.Note))
	}
	if f.DocsUrl != nil {
		p.line("    %s", p.dim("Docs: "+*f.DocsUrl))
	}
}

// Review is `muretto review`.
func Review(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	ix := newIndex(a)
	var pending, decided []contract.ReviewItem
	for _, rv := range a.Reviews {
		if rv.State == contract.ReviewItemStatePending {
			pending = append(pending, rv)
		} else {
			decided = append(decided, rv)
		}
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].DueAt.Before(pending[j].DueAt) })
	if len(pending) == 0 {
		p.line("Nothing to review.")
	} else {
		p.line("%s", p.bold("Still need these?")+p.dim("  (Muretto only records your answer; it never deletes anything)"))
		for _, rv := range pending {
			r := ix.res[rv.ResourceID]
			due := "due " + p.when(rv.DueAt)
			if !rv.DueAt.After(p.o.Now) {
				due = "review due"
			}
			owner := ""
			if rv.OwnerDeveloperID != nil {
				owner = " · owner " + ownerName(ix, *rv.OwnerDeveloperID)
			}
			p.line("")
			p.line("  %s %s %s · %s%s", pad(ShortKind(r.Kind), 10), DisplayName(r), p.dim(p.daysOld(ix.createdAt(r))), due, owner)
			p.line("    → muretto review %s keep   → muretto review %s not-needed", DisplayName(r), DisplayName(r))
		}
	}
	if len(decided) > 0 {
		keep, nn := 0, 0
		for _, d := range decided {
			if d.State == contract.ReviewItemStateKeep {
				keep++
			} else {
				nn++
			}
		}
		p.line("")
		p.line("  %s", p.dim(fmt.Sprintf("Decided: %d keep, %d not needed.", keep, nn)))
	}
	return p.err
}

// ProviderRow describes a provider for `muretto providers`.
type ProviderRow struct {
	Name, DisplayName string
	Accounts          []string // "name (id) · token from keychain"
	Coverage          [][3]string
}

// Providers is `muretto providers [--coverage]`.
func Providers(w io.Writer, a contract.Answer, rows []ProviderRow, coverage bool, o Options) error {
	p := newPrinter(w, o)
	if len(rows) == 0 {
		p.line("No provider adapters are loaded.")
		return p.err
	}
	for i, row := range rows {
		if i > 0 {
			p.line("")
		}
		ok, failed := 0, 0
		var last *time.Time
		for _, c := range a.Checks {
			if c.Provider != row.Name {
				continue
			}
			if c.Status == contract.ProviderCheckStatusOk {
				ok++
			} else if c.Status == contract.ProviderCheckStatusError {
				failed++
			}
			if c.LastSuccessAt != nil && (last == nil || c.LastSuccessAt.Before(*last)) {
				t := *c.LastSuccessAt
				last = &t
			}
		}
		health := "not checked yet"
		if last != nil {
			health = fmt.Sprintf("%d checks ok", ok)
			if failed > 0 {
				health += fmt.Sprintf(", %d failed", failed)
			}
			health += " · checked " + p.ago(*last)
		}
		p.line("%s  %s", p.bold(pad(row.DisplayName, 10)), p.dim(health))
		if len(row.Accounts) == 0 {
			p.line("  not connected  → muretto init")
		}
		for _, acct := range row.Accounts {
			p.line("  %s", acct)
		}
		if coverage {
			p.line("")
			for _, c := range row.Coverage {
				p.line("  %s %s %s", pad(c[0], 22), pad(c[1], 8), p.dim(c[2]))
			}
		}
	}
	return p.err
}

// Warnings prints only the answer's warnings (used by doctor).
func Warnings(w io.Writer, a contract.Answer, o Options) error {
	p := newPrinter(w, o)
	p.warnings(a)
	return p.err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
