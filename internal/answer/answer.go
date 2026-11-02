// Package answer computes contract.Answer, the one model that the text
// output, --json output and sync payloads are all rendered from.
package answer

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/attrib"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/redact"
	"github.com/tolgayayci/muretto/internal/store"
)

// SchemaVersion of the Answer.
const SchemaVersion = 1

// HookState is what claudecfg.Inspect reported.
type HookState struct {
	Installed     bool
	HooksDisabled bool
	ManagedOnly   bool
}

// HookWindow is how recent a hook-recorded session must be to show that
// Muretto's hooks run, even when they are not in Claude Code's settings.json.
const HookWindow = 7 * 24 * time.Hour

// HookEventsSeen reports whether a session with hook-recorded events was
// active within HookWindow before now.
func HookEventsSeen(sessions []contract.Session, now time.Time) bool {
	for _, s := range sessions {
		if !slices.Contains(s.Sources, contract.SessionSourcesElemHook) {
			continue
		}
		last := s.StartedAt
		if s.LastActivityAt != nil && s.LastActivityAt.After(last) {
			last = *s.LastActivityAt
		}
		if now.Sub(last) <= HookWindow {
			return true
		}
	}
	return false
}

// Input is everything Compute needs. It does no I/O.
type Input struct {
	Developer      contract.Developer
	DeveloperNames map[string]string // developer id -> display name
	Sessions       []contract.Session
	Actions        []contract.AgentAction
	Resources      []contract.Resource
	Findings       []contract.ExposureFinding
	Costs          []contract.CostRecord
	Checks         []contract.ProviderCheck
	Reviews        []contract.ReviewItem // recorded human decisions
	ReviewAfter    time.Duration         // default 7 days
	StaleAfter     time.Duration         // default 15 minutes
	Hooks          *HookState            // nil when unknown
	Accounts       int                   // connected provider accounts
	Warnings       []contract.Warning    // from this run's ingest and scan
	// AmbiguousActions are tool calls held by two sessions with identical
	// timing (observe.Merge warning action_owner_ambiguous). Resources they
	// created are labelled unknown.
	AmbiguousActions map[string]bool
}

// Compute builds the Answer.
func Compute(in Input, now time.Time) contract.Answer {
	now = now.UTC()
	if in.ReviewAfter <= 0 {
		in.ReviewAfter = 7 * 24 * time.Hour
	}
	if in.StaleAfter <= 0 {
		in.StaleAfter = 15 * time.Minute
	}
	atts, evOnly := attrib.Compute(in.Sessions, in.Actions, in.Resources, now)
	for i := range atts {
		atts[i] = downgradeAmbiguous(atts[i], in.AmbiguousActions)
	}
	checks := append([]contract.ProviderCheck{}, in.Checks...)

	resources := append([]contract.Resource{}, in.Resources...)
	for _, r := range evOnly {
		if c, ok := coveringCheck(checks, r, nil); ok && c.Status == contract.ProviderCheckStatusOk && r.AccountID != "" {
			// A successful list of this kind did not include it.
			g := *c.LastSuccessAt
			r.Existence, r.GoneAt = contract.ResourceExistenceGone, &g
		}
		resources = append(resources, r)
	}
	for i := range resources {
		if resources[i].Attributes == nil {
			resources[i].Attributes = contract.ResourceAttributes{}
		}
	}
	sort.SliceStable(resources, func(i, j int) bool { return resourceLess(resources[i], resources[j]) })
	sort.SliceStable(atts, func(i, j int) bool { return atts[i].ResourceID < atts[j].ResourceID })

	attBy := map[string]contract.Attribution{}
	for _, a := range atts {
		attBy[a.ResourceID] = a
	}
	sessBy := map[string]contract.Session{}
	for _, s := range in.Sessions {
		sessBy[s.ID] = s
	}

	a := contract.Answer{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now,
		Developer:     in.Developer,
		Resources:     resources,
		Attributions:  atts,
		Checks:        checks,
		Findings:      deriveFindings(resources, in.Findings, checks),
		Costs:         deriveCosts(resources, attBy, in.Costs, now),
		Reviews:       deriveReviews(resources, attBy, sessBy, in.Reviews, in.ReviewAfter),
	}
	a.Sessions = sessionViews(in, a, now)
	a.Warnings = warnings(in, a, now)
	a.Summary = Summarize(a, now)
	return Redact(a)
}

// downgradeAmbiguous turns an exact label into unknown when any creating
// action behind it is held by two sessions that cannot be told apart.
func downgradeAmbiguous(at contract.Attribution, ambiguous map[string]bool) contract.Attribution {
	if at.Label != contract.AttributionLabelExact {
		return at
	}
	for _, e := range at.Evidence {
		if ambiguous[e.ActionID] {
			at.Label, at.SessionID, at.RuleID = contract.AttributionLabelUnknown, nil, nil
			at.Reason = "the tool call that created it (" + e.ActionID + ") appears in two sessions with identical timing, so the creating session is unknown"
			return at
		}
	}
	return at
}

func resourceLess(a, b contract.Resource) bool {
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// coveringCheck returns the check for r's kind and scope (and rule, when
// set), preferring the most specific one.
func coveringCheck(checks []contract.ProviderCheck, r contract.Resource, rule *string) (contract.ProviderCheck, bool) {
	scope := store.ParentProviderID(r)
	var best contract.ProviderCheck
	found, bestSpecific := false, false
	for _, c := range checks {
		if c.Provider != r.Provider || c.AccountID != r.AccountID {
			continue
		}
		if (rule == nil) != (c.RuleID == nil) || (rule != nil && *rule != *c.RuleID) {
			continue
		}
		if !store.CheckCovers(c, r.Kind, scope, r.AccountID) {
			continue
		}
		specific := c.Scope != nil && *c.Scope != ""
		if !found || (specific && !bestSpecific) || (specific == bestSpecific && c.AttemptedAt.After(best.AttemptedAt)) {
			best, found, bestSpecific = c, true, specific
		}
	}
	return best, found
}

func findingKey(resourceID, rule string) string { return rule + ":" + resourceID }

// deriveFindings keeps the adapters' findings and fills in a tri-state
// finding for every rule that applies to each existing resource: not_exposed
// only after a successful check of that kind, scope and rule with no exposed
// finding; unknown otherwise. Never not_exposed by default.
func deriveFindings(resources []contract.Resource, stored []contract.ExposureFinding, checks []contract.ProviderCheck) []contract.ExposureFinding {
	live := map[string]contract.Resource{}
	for _, r := range resources {
		if r.Existence != contract.ResourceExistenceGone {
			live[r.ID] = r
		}
	}
	have := map[string]bool{}
	template := map[string]contract.ExposureFinding{}
	out := []contract.ExposureFinding{}
	for _, f := range stored {
		if _, ok := live[f.ResourceID]; !ok {
			continue
		}
		if f.State != contract.ExposureFindingStateExposed && f.CheckedAt == nil {
			f.State = contract.ExposureFindingStateUnknown
		}
		have[findingKey(f.ResourceID, f.RuleID)] = true
		if _, ok := template[f.RuleID]; !ok || f.State == contract.ExposureFindingStateExposed {
			template[f.RuleID] = f
		}
		out = append(out, f)
	}
	rulesByKind := map[string][]string{}
	seenRule := map[string]bool{}
	for _, c := range checks {
		if c.RuleID == nil {
			continue
		}
		k := c.Kind + "\x00" + *c.RuleID
		if !seenRule[k] {
			seenRule[k] = true
			rulesByKind[c.Kind] = append(rulesByKind[c.Kind], *c.RuleID)
		}
	}
	for _, r := range resources {
		if r.Existence == contract.ResourceExistenceGone {
			continue
		}
		for _, rule := range rulesByKind[r.Kind] {
			if have[findingKey(r.ID, rule)] {
				continue
			}
			f := contract.ExposureFinding{
				ID: findingKey(r.ID, rule), ResourceID: r.ID, RuleID: rule,
				State: contract.ExposureFindingStateUnknown, Severity: contract.ExposureFindingSeverityInfo,
				Signal: contract.ExposureFindingSignalApiField, Title: adapters.HumanRule(rule),
			}
			if t, ok := template[rule]; ok {
				f.Signal, f.Caveat = t.Signal, t.Caveat
			}
			c, ok := coveringCheck(checks, r, &rule)
			if ok {
				f.CheckedAt = c.LastSuccessAt
			}
			switch {
			case ok && c.Status == contract.ProviderCheckStatusOk:
				f.State = contract.ExposureFindingStateNotExposed
				f.Title = adapters.NotExposedTitle(rule)
				f.Explanation = adapters.NotExposedExplanation
			case ok:
				f.Title = adapters.HumanRule(rule) + ": not checked"
				f.Explanation = "The last check of this rule did not succeed, so exposure is unknown."
				if c.Error != nil {
					f.Explanation += " Error: " + *c.Error
				}
			default:
				f.Title = adapters.HumanRule(rule) + ": not checked"
				f.Explanation = "This rule has not been checked for this resource yet, so exposure is unknown."
			}
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if si, sj := stateRank(out[i].State), stateRank(out[j].State); si != sj {
			return si < sj
		}
		if si, sj := SeverityRank(out[i].Severity), SeverityRank(out[j].Severity); si != sj {
			return si < sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func stateRank(s contract.ExposureFindingState) int {
	switch s {
	case contract.ExposureFindingStateExposed:
		return 0
	case contract.ExposureFindingStateUnknown:
		return 1
	case contract.ExposureFindingStateNotExposed:
		return 2
	}
	return 3
}

// SeverityRank orders severities worst first.
func SeverityRank(s contract.ExposureFindingSeverity) int {
	switch s {
	case contract.ExposureFindingSeverityCritical:
		return 0
	case contract.ExposureFindingSeverityHigh:
		return 1
	case contract.ExposureFindingSeverityMedium:
		return 2
	case contract.ExposureFindingSeverityLow:
		return 3
	case contract.ExposureFindingSeverityInfo:
		return 4
	}
	return 5
}

// deriveCosts keeps provider costs and adds an explicit unknown record for
// each agent-created resource without one. Unknown never carries an amount.
func deriveCosts(resources []contract.Resource, attBy map[string]contract.Attribution, stored []contract.CostRecord, now time.Time) []contract.CostRecord {
	out := []contract.CostRecord{}
	has := map[string]bool{}
	for _, c := range stored {
		if c.Source == contract.CostRecordSourceUnknown {
			c.AmountMicros, c.Currency = nil, nil
		}
		has[c.ResourceID] = true
		out = append(out, c)
	}
	for _, r := range resources {
		if has[r.ID] || r.Existence == contract.ResourceExistenceGone || attBy[r.ID].Label != contract.AttributionLabelExact {
			continue
		}
		out = append(out, contract.CostRecord{ResourceID: r.ID, Source: contract.CostRecordSourceUnknown, Basis: "no cost data available", AsOf: now})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ResourceID < out[j].ResourceID })
	return out
}

// deriveReviews gives every existing agent-created resource a review item:
// the recorded decision, or pending, due ReviewAfter after creation, owned by
// the developer of the creating session.
func deriveReviews(resources []contract.Resource, attBy map[string]contract.Attribution, sessBy map[string]contract.Session, recorded []contract.ReviewItem, after time.Duration) []contract.ReviewItem {
	rec := map[string]contract.ReviewItem{}
	for _, r := range recorded {
		rec[r.ResourceID] = r
	}
	out := []contract.ReviewItem{}
	for _, r := range resources {
		att := attBy[r.ID]
		if r.Existence == contract.ResourceExistenceGone || att.Label != contract.AttributionLabelExact || att.SessionID == nil {
			continue
		}
		base := r.FirstSeenAt
		if len(att.Evidence) > 0 {
			base = att.Evidence[0].OccurredAt
		}
		if r.CreatedAt != nil {
			base = *r.CreatedAt
		}
		item := contract.ReviewItem{ResourceID: r.ID, State: contract.ReviewItemStatePending, DueAt: base.Add(after).UTC()}
		if s, ok := sessBy[*att.SessionID]; ok && s.DeveloperID != "" {
			owner := s.DeveloperID
			item.OwnerDeveloperID = &owner
		}
		if d, ok := rec[r.ID]; ok {
			item.State, item.DecidedAt, item.DecidedBy, item.Note = d.State, d.DecidedAt, d.DecidedBy, d.Note
		}
		out = append(out, item)
	}
	return out
}

// DeveloperName returns the display name for a developer id.
func DeveloperName(in Input, id string) string {
	if n := in.DeveloperNames[id]; n != "" {
		return n
	}
	if id == in.Developer.ID && in.Developer.DisplayName != "" {
		return in.Developer.DisplayName
	}
	if len(id) > 12 {
		return id[:12]
	}
	if id == "" {
		return "unknown"
	}
	return id
}

func sessionViews(in Input, a contract.Answer, now time.Time) []contract.SessionView {
	exposed := exposedSet(a.Findings)
	due := reviewDueSet(a.Reviews, now)
	bySession := map[string][]string{}
	for _, at := range a.Attributions {
		if at.Label == contract.AttributionLabelExact && at.SessionID != nil {
			bySession[*at.SessionID] = append(bySession[*at.SessionID], at.ResourceID)
		}
	}
	sessions := append([]contract.Session{}, in.Sessions...)
	sort.SliceStable(sessions, func(i, j int) bool {
		if !sessions[i].StartedAt.Equal(sessions[j].StartedAt) {
			return sessions[i].StartedAt.After(sessions[j].StartedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	out := []contract.SessionView{}
	for _, s := range sessions {
		if s.Sources == nil {
			s.Sources = []contract.SessionSourcesElem{}
		}
		ids := bySession[s.ID]
		if ids == nil {
			ids = []string{}
		}
		sort.Strings(ids)
		v := contract.SessionView{Session: s, DeveloperName: clip(DeveloperName(in, s.DeveloperID), 100), ResourceIds: ids}
		for _, id := range ids {
			if exposed[id] {
				v.ExposedCount++
			}
			if due[id] {
				v.ReviewDueCount++
			}
		}
		out = append(out, v)
	}
	return out
}

func exposedSet(fs []contract.ExposureFinding) map[string]bool {
	m := map[string]bool{}
	for _, f := range fs {
		if f.State == contract.ExposureFindingStateExposed {
			m[f.ResourceID] = true
		}
	}
	return m
}

func reviewDueSet(rs []contract.ReviewItem, now time.Time) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		if r.State == contract.ReviewItemStatePending && !r.DueAt.After(now) {
			m[r.ResourceID] = true
		}
	}
	return m
}

// Summarize computes the summary counts of an answer (also used for subsets).
func Summarize(a contract.Answer, now time.Time) contract.Summary {
	s := contract.Summary{Sessions: len(a.Sessions)}
	res := map[string]contract.Resource{}
	for _, r := range a.Resources {
		res[r.ID] = r
	}
	for _, at := range a.Attributions {
		r := res[at.ResourceID]
		if at.Label == contract.AttributionLabelExact {
			s.AgentCreated++
			if r.Existence == contract.ResourceExistenceConfirmed {
				s.StillExisting++
			}
		} else if r.SeenVia != contract.ResourceSeenViaEvidence && r.Existence != contract.ResourceExistenceGone {
			s.UnknownCreator++
		}
	}
	for _, f := range a.Findings {
		if f.State == contract.ExposureFindingStateExposed {
			s.Exposed++
		}
	}
	s.NeedsReview = len(reviewDueSet(a.Reviews, now))
	for _, c := range a.Checks {
		switch c.Status {
		case contract.ProviderCheckStatusOk:
			s.ChecksOk++
		case contract.ProviderCheckStatusError:
			s.ChecksFailed++
		case contract.ProviderCheckStatusSkipped:
		}
		if c.LastSuccessAt != nil && (s.OldestCheckAt == nil || c.LastSuccessAt.Before(*s.OldestCheckAt)) {
			t := *c.LastSuccessAt
			s.OldestCheckAt = &t
		}
	}
	return s
}

func warnings(in Input, a contract.Answer, now time.Time) []contract.Warning {
	out := []contract.Warning{}
	add := func(code, msg, hint string) {
		w := contract.Warning{Code: code, Message: clip(msg, 500)}
		if hint != "" {
			h := clip(hint, 500)
			w.Hint = &h
		}
		out = append(out, w)
	}
	out = append(out, in.Warnings...)
	if in.Accounts == 0 {
		add("no_providers", "No provider accounts are connected, so Muretto only sees what sessions said they created.", "muretto init")
	}
	if in.Hooks != nil {
		switch {
		case in.Hooks.HooksDisabled:
			add("hooks_disabled", "Claude Code hooks are disabled (disableAllHooks), so new sessions are only seen through transcripts.", "Remove disableAllHooks from your Claude Code settings")
		case in.Hooks.ManagedOnly:
			add("hooks_managed_only", "Your Claude Code settings only allow managed hooks, so Muretto's hooks do not run.", "Ask your admin to add Muretto's hooks to managed settings")
		case !in.Hooks.Installed && HookEventsSeen(in.Sessions, now):
			// Registered elsewhere (e.g. `claude --settings`, a plugin or
			// managed settings): hook events are arriving, so say nothing.
		case !in.Hooks.Installed:
			add("hooks_not_installed", "Muretto's Claude Code hooks are not installed; sessions are read from transcripts only.", "muretto init")
		}
	}
	tOnly := 0
	for _, s := range in.Sessions {
		if len(s.Sources) == 1 && s.Sources[0] == contract.SessionSourcesElemTranscript {
			tOnly++
		}
	}
	if tOnly > 0 {
		add("transcript_only_sessions", fmt.Sprintf("%d %s seen only in transcripts (hooks did not run); their evidence may be incomplete.", tOnly, plural(tOnly, "session was", "sessions were")), "muretto doctor")
	}
	type key struct{ p, acct string }
	failed := map[key][]contract.ProviderCheck{}
	var keys []key
	for _, c := range a.Checks {
		if c.Status != contract.ProviderCheckStatusError {
			continue
		}
		k := key{c.Provider, c.AccountID}
		if _, ok := failed[k]; !ok {
			keys = append(keys, k)
		}
		failed[k] = append(failed[k], c)
	}
	for _, k := range keys {
		cs := failed[k]
		msg := fmt.Sprintf("%d %s check%s failed for account %s", len(cs), k.p, plural(len(cs), "", "s"), k.acct)
		if cs[0].Error != nil {
			msg += ": " + *cs[0].Error
		}
		add("check_failed", msg+". Data from those checks is shown with its age.", "muretto doctor")
	}
	sum := Summarize(a, now)
	if sum.OldestCheckAt != nil && now.Sub(*sum.OldestCheckAt) > in.StaleAfter {
		add("stale_data", fmt.Sprintf("Some provider data was last checked %s ago.", Age(now.Sub(*sum.OldestCheckAt))), "muretto --refresh")
	}
	return out
}

// Age formats a duration like "3 min" or "2 h".
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}

// Redact runs the redactor over every free-text field. Ids are left alone so
// joins keep working; render redacts every printed string again.
func Redact(a contract.Answer) contract.Answer {
	rp := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := redact.String(*p)
		return &v
	}
	for i := range a.Sessions {
		s := &a.Sessions[i].Session
		s.TaskTitle = rp(s.TaskTitle)
		if s.Repo != nil {
			r := *s.Repo
			r.RemoteUrl, r.DisplayName = rp(r.RemoteUrl), rp(r.DisplayName)
			s.Repo = &r
		}
	}
	for i := range a.Attributions {
		at := &a.Attributions[i]
		at.Reason = redact.String(at.Reason)
		ev := make([]contract.Evidence, len(at.Evidence))
		for j, e := range at.Evidence {
			e.Excerpt = redact.String(e.Excerpt)
			ev[j] = e
		}
		at.Evidence = ev
	}
	for i := range a.Findings {
		f := &a.Findings[i]
		f.Title, f.Explanation = redact.String(f.Title), redact.String(f.Explanation)
		f.Caveat = rp(f.Caveat)
	}
	for i := range a.Checks {
		a.Checks[i].Error = rp(a.Checks[i].Error)
	}
	for i := range a.Reviews {
		a.Reviews[i].Note = rp(a.Reviews[i].Note)
	}
	for i := range a.Warnings {
		a.Warnings[i].Message = redact.String(a.Warnings[i].Message)
		a.Warnings[i].Hint = rp(a.Warnings[i].Hint)
	}
	return a
}

// Subset keeps the resources and sessions the predicates accept, with their
// attributions, findings, costs and reviews, and recomputes the summary. Text
// views and --json for a command are both rendered from the same subset.
func Subset(a contract.Answer, keepRes func(contract.Resource) bool, keepSess func(contract.SessionView) bool) contract.Answer {
	out := a
	ids := map[string]bool{}
	accounts := map[string]bool{}
	out.Resources = []contract.Resource{}
	for _, r := range a.Resources {
		if keepRes(r) {
			ids[r.ID] = true
			accounts[r.Provider+"\x00"+r.AccountID] = true
			out.Resources = append(out.Resources, r)
		}
	}
	out.Sessions = []contract.SessionView{}
	for _, s := range a.Sessions {
		if keepSess(s) {
			out.Sessions = append(out.Sessions, s)
		}
	}
	out.Attributions = filter(a.Attributions, func(x contract.Attribution) bool { return ids[x.ResourceID] })
	out.Findings = filter(a.Findings, func(x contract.ExposureFinding) bool { return ids[x.ResourceID] })
	out.Costs = filter(a.Costs, func(x contract.CostRecord) bool { return ids[x.ResourceID] })
	out.Reviews = filter(a.Reviews, func(x contract.ReviewItem) bool { return ids[x.ResourceID] })
	out.Checks = filter(a.Checks, func(x contract.ProviderCheck) bool { return accounts[x.Provider+"\x00"+x.AccountID] })
	out.Summary = Summarize(out, a.GeneratedAt)
	return out
}

func filter[T any](xs []T, keep func(T) bool) []T {
	out := []T{}
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}
