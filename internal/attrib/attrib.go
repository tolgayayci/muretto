// Package attrib joins agent evidence (actions with identifiers) to provider
// resources. It is pure: no I/O, no clock other than the now argument.
//
// The rules (docs/decisions/0401-attribution-rules.md):
//
//   - id_in_result: an identifier with role created whose value equals a
//     resource's provider id gives exact.
//   - host_in_result: the same, when the value equals the resource's own
//     generated hostname (attributes.host, e.g. a Vercel deployment URL that
//     `vercel deploy` prints in human mode). Hosts are unique per resource.
//   - name_in_container: an identifier with role created that names its
//     container (a project ref, a team, team/project) and a name that resolves
//     to exactly one resource in that container gives exact, if the creating
//     action succeeded and no other session created something with that name.
//   - Provider created_at may veto a match (the resource is older than every
//     creating action); so may Muretto's own history: a scan that listed it
//     (first_seen_at) before the earliest creating action means the action
//     did not create it (docs/decisions/0406-proof-of-creation.md); so may a
//     recorded disappearance after the last creating action (the resource
//     was recreated). Timing never creates one.
//   - Two sessions claiming the same resource give unknown, with the
//     conflicting evidence listed.
//   - Referenced identifiers never attribute.
//   - Everything else is unknown.
package attrib

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/redact"
)

// Rule ids stored in Attribution.RuleID.
const (
	RuleIDInResult      = "id_in_result"
	RuleHostInResult    = "host_in_result"
	RuleNameInContainer = "name_in_container"
)

// VetoTolerance absorbs tool run time and clock skew: an action's timestamp
// may be taken when the tool finished, after the provider stamped created_at.
// A resource is only vetoed when it is older than the earliest creating action
// by more than this. A larger value can only miss a veto, never invent a match.
const VetoTolerance = 10 * time.Minute

// Compute returns one attribution per resource (listed resources first, then
// evidence-only ones) and the evidence-only resources: things a session's own
// output says it created that no listed resource matches.
func Compute(sessions []contract.Session, actions []contract.AgentAction, resources []contract.Resource, now time.Time) ([]contract.Attribution, []contract.Resource) {
	now = now.UTC()
	acts := dedupe(sessions, actions)
	idx := newIndex(resources)

	perRes := map[string]*resClaims{}
	get := func(id string) *resClaims {
		rc := perRes[id]
		if rc == nil {
			rc = &resClaims{}
			perRes[id] = rc
		}
		return rc
	}
	evOnly := map[string]*evidenceOnly{}
	var unscoped []claim

	for i := range acts {
		a := &acts[i]
		for _, ident := range a.Identifiers {
			c := claim{action: a, ident: ident}
			if ident.Role != contract.IdentifierRoleCreated {
				for _, r := range idx.matchAny(ident) {
					get(r.ID).refs = append(get(r.ID).refs, c)
				}
				continue
			}
			if ident.Container == nil || *ident.Container == "" {
				unscoped = append(unscoped, c)
			}
			matched, rule := idx.match(ident)
			switch {
			case len(matched) == 1:
				c.rule = rule
				rc := get(matched[0].ID)
				rc.claims = append(rc.claims, c)
			case len(matched) > 1:
				for _, r := range matched {
					rc := get(r.ID)
					rc.ambiguous = append(rc.ambiguous, c)
				}
			default:
				key := evidenceKey(ident)
				eo := evOnly[key]
				if eo == nil {
					eo = &evidenceOnly{id: key, ident: ident}
					evOnly[key] = eo
				}
				eo.claims = append(eo.claims, c)
			}
		}
	}

	var out []contract.Attribution
	for _, r := range resources {
		out = append(out, decide(r, perRes[r.ID], unscoped, now))
	}

	keys := make([]string, 0, len(evOnly))
	for k := range evOnly {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var extra []contract.Resource
	for _, k := range keys {
		eo := evOnly[k]
		res := eo.resource(idx)
		extra = append(extra, res)
		out = append(out, decideEvidenceOnly(res, eo.claims, now))
	}
	return out, extra
}

type claim struct {
	action *contract.AgentAction
	ident  contract.Identifier
	rule   string
}

type resClaims struct {
	claims    []claim // created identifiers matching exactly this resource
	ambiguous []claim // created identifiers whose name matched several resources
	refs      []claim // referenced identifiers
}

type evidenceOnly struct {
	id     string
	ident  contract.Identifier
	claims []claim
}

// dedupe keeps one copy of each action id (the Claude Code tool_use_id). A
// fork copies history into a new session, so the copy in the session that
// started earliest wins.
func dedupe(sessions []contract.Session, actions []contract.AgentAction) []contract.AgentAction {
	started := map[string]time.Time{}
	for _, s := range sessions {
		started[s.ID] = s.StartedAt
	}
	earlier := func(a, b contract.AgentAction) bool {
		sa, oka := started[a.SessionID]
		sb, okb := started[b.SessionID]
		if oka != okb {
			return oka
		}
		if !sa.Equal(sb) {
			return sa.Before(sb)
		}
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.Before(b.OccurredAt)
		}
		return a.SessionID < b.SessionID
	}
	best := map[string]int{}
	var out []contract.AgentAction
	for _, a := range actions {
		if i, ok := best[a.ID]; ok {
			if earlier(a, out[i]) {
				out[i] = a
			}
			continue
		}
		best[a.ID] = len(out)
		out = append(out, a)
	}
	return out
}

type index struct {
	byKey  map[string]map[string]*contract.Resource // kind -> provider id
	byName map[string]map[string][]*contract.Resource
	byHost map[string]map[string][]*contract.Resource // kind -> attributes.host
	byID   map[string]*contract.Resource
	byPID  map[string]*contract.Resource // provider id of any kind (for containers)
}

func newIndex(resources []contract.Resource) *index {
	ix := &index{
		byKey:  map[string]map[string]*contract.Resource{},
		byName: map[string]map[string][]*contract.Resource{},
		byHost: map[string]map[string][]*contract.Resource{},
		byID:   map[string]*contract.Resource{},
		byPID:  map[string]*contract.Resource{},
	}
	for i := range resources {
		r := &resources[i]
		if ix.byKey[r.Kind] == nil {
			ix.byKey[r.Kind] = map[string]*contract.Resource{}
			ix.byName[r.Kind] = map[string][]*contract.Resource{}
		}
		ix.byKey[r.Kind][r.ProviderID] = r
		if r.Name != "" {
			ix.byName[r.Kind][r.Name] = append(ix.byName[r.Kind][r.Name], r)
		}
		if h, ok := r.Attributes["host"].(string); ok && h != "" {
			if ix.byHost[r.Kind] == nil {
				ix.byHost[r.Kind] = map[string][]*contract.Resource{}
			}
			ix.byHost[r.Kind][h] = append(ix.byHost[r.Kind][h], r)
		}
		ix.byID[r.ID] = r
		if _, dup := ix.byPID[r.ProviderID]; !dup {
			ix.byPID[r.ProviderID] = r
		}
	}
	return ix
}

// match resolves a created identifier to resources and names the rule.
func (ix *index) match(id contract.Identifier) ([]*contract.Resource, string) {
	if r := ix.byKey[id.Kind][id.Value]; r != nil {
		return []*contract.Resource{r}, RuleIDInResult
	}
	if rs := ix.byHost[id.Kind][id.Value]; len(rs) > 0 {
		return rs, RuleHostInResult // more than one is ambiguous, never exact
	}
	if id.Container == nil || *id.Container == "" {
		return nil, ""
	}
	cont := *id.Container
	if r := ix.byKey[id.Kind][cont+"/"+id.Value]; r != nil {
		return []*contract.Resource{r}, RuleNameInContainer
	}
	var out []*contract.Resource
	for _, r := range ix.byName[id.Kind][id.Value] {
		if ix.inContainer(r, cont) {
			out = append(out, r)
		}
	}
	return out, RuleNameInContainer
}

// matchAny is match without uniqueness: used for referenced identifiers,
// which only feed the human-readable reason.
func (ix *index) matchAny(id contract.Identifier) []*contract.Resource {
	rs, _ := ix.match(id)
	return rs
}

// inContainer reports whether container names r's account, its parent, or
// account/parent (by id, name or slug).
func (ix *index) inContainer(r *contract.Resource, container string) bool {
	for _, c := range ix.containers(r) {
		if c == container {
			return true
		}
	}
	return false
}

func (ix *index) containers(r *contract.Resource) []string {
	accounts := []string{}
	if r.AccountID != "" {
		accounts = append(accounts, r.AccountID)
	}
	if r.AccountName != nil && *r.AccountName != "" {
		accounts = append(accounts, *r.AccountName)
	}
	if s, ok := r.Attributes["account_slug"].(string); ok && s != "" {
		accounts = append(accounts, s)
	}
	parents := []string{}
	if r.ParentID != nil {
		if _, pid, ok := strings.Cut(*r.ParentID, ":"); ok && pid != "" {
			parents = append(parents, pid)
		}
		if p := ix.byID[*r.ParentID]; p != nil && p.Name != "" {
			parents = append(parents, p.Name)
		}
	}
	out := append([]string{}, accounts...)
	out = append(out, parents...)
	for _, a := range accounts {
		for _, p := range parents {
			out = append(out, a+"/"+p)
		}
	}
	return out
}

func evidenceKey(id contract.Identifier) string {
	key := id.Value
	if id.Container != nil && *id.Container != "" && !strings.HasPrefix(id.Value, *id.Container+"/") {
		key = *id.Container + "/" + id.Value
	}
	return id.Kind + ":" + key
}

func (eo *evidenceOnly) resource(ix *index) contract.Resource {
	kind, key, _ := strings.Cut(eo.id, ":")
	first, last := eo.claims[0].action.OccurredAt, eo.claims[0].action.OccurredAt
	for _, c := range eo.claims {
		if c.action.OccurredAt.Before(first) {
			first = c.action.OccurredAt
		}
		if c.action.OccurredAt.After(last) {
			last = c.action.OccurredAt
		}
	}
	r := contract.Resource{
		ID:          eo.id,
		Kind:        kind,
		Provider:    eo.ident.Provider,
		ProviderID:  key,
		Name:        eo.ident.Value,
		Attributes:  contract.ResourceAttributes{},
		Existence:   contract.ResourceExistenceUnconfirmed,
		SeenVia:     contract.ResourceSeenViaEvidence,
		FirstSeenAt: first.UTC(),
		LastSeenAt:  last.UTC(),
	}
	if eo.ident.Container != nil {
		if p := ix.byPID[*eo.ident.Container]; p != nil {
			pid := p.ID
			r.ParentID = &pid
			r.AccountID = p.AccountID
			r.AccountName = p.AccountName
		}
	}
	return r
}

func decide(r contract.Resource, rc *resClaims, unscoped []claim, now time.Time) contract.Attribution {
	att := contract.Attribution{ResourceID: r.ID, ComputedAt: now, Label: contract.AttributionLabelUnknown, Evidence: []contract.Evidence{}}
	if rc == nil {
		rc = &resClaims{}
	}
	if len(rc.claims) == 0 {
		switch {
		case len(rc.ambiguous) > 0:
			att.Reason = fmt.Sprintf("a session created a %s named %q, but more than one resource matches that name", short(r.Kind), rc.ambiguous[0].ident.Value)
			att.Evidence = evidence(rc.ambiguous)
		case len(rc.refs) > 0:
			att.Reason = fmt.Sprintf("creator unknown: session %s only read or referenced it", shortID(rc.refs[0].action.SessionID))
		default:
			att.Reason = "creator unknown: no recorded agent session created it"
		}
		return att
	}

	bySession := groupBySession(rc.claims)
	if len(bySession) > 1 {
		att.Reason = fmt.Sprintf("%d sessions claim to have created this", len(bySession))
		att.Evidence = evidence(append(rc.claims, rc.ambiguous...))
		return att
	}
	sid := rc.claims[0].action.SessionID
	claims := rc.claims
	att.Evidence = evidence(claims)

	rule := RuleNameInContainer
	for _, c := range claims {
		if c.rule == RuleIDInResult || (c.rule == RuleHostInResult && rule != RuleIDInResult) {
			rule = c.rule
		}
	}
	if rule == RuleNameInContainer {
		ok := false
		for _, c := range claims {
			if c.action.Status == contract.AgentActionStatusOk {
				ok = true
			}
		}
		if !ok {
			att.Reason = "the only creating action for this name failed, so the name alone is not enough"
			return att
		}
		if len(rc.ambiguous) > 0 {
			att.Reason = "another created identifier with this name matched several resources"
			att.Evidence = evidence(append(claims, rc.ambiguous...))
			return att
		}
		var others []claim
		for _, u := range unscoped {
			if u.action.SessionID == sid || u.ident.Kind != r.Kind {
				continue
			}
			if u.ident.Value == r.Name || strings.HasSuffix(r.ProviderID, "/"+u.ident.Value) {
				others = append(others, u)
			}
		}
		if len(others) > 0 {
			att.Reason = fmt.Sprintf("session %s also created a %s named %q in an unknown container", shortID(others[0].action.SessionID), short(r.Kind), others[0].ident.Value)
			att.Evidence = evidence(append(claims, others...))
			return att
		}
	}

	earliest, latest := claims[0].action.OccurredAt, claims[0].action.OccurredAt
	for _, c := range claims {
		if c.action.OccurredAt.Before(earliest) {
			earliest = c.action.OccurredAt
		}
		if c.action.OccurredAt.After(latest) {
			latest = c.action.OccurredAt
		}
	}
	if r.CreatedAt != nil && r.CreatedAt.Before(earliest.Add(-VetoTolerance)) {
		att.Reason = fmt.Sprintf("the provider says it was created at %s, before session %s's action at %s, so that session cannot have created it", r.CreatedAt.UTC().Format(time.RFC3339), shortID(sid), earliest.UTC().Format(time.RFC3339))
		return att
	}
	if seenBefore(r, earliest) {
		if r.GoneAt != nil && !r.GoneAt.After(earliest) {
			att.Reason = fmt.Sprintf("Muretto listed it at %s and saw it gone at %s, before session %s's action at %s; it cannot tell whether it came back before that action, which may then only have updated it", r.FirstSeenAt.UTC().Format(time.RFC3339), r.GoneAt.UTC().Format(time.RFC3339), shortID(sid), earliest.UTC().Format(time.RFC3339))
		} else {
			att.Reason = fmt.Sprintf("Muretto already listed it at %s, before session %s's action at %s, so that session cannot have created it", r.FirstSeenAt.UTC().Format(time.RFC3339), shortID(sid), earliest.UTC().Format(time.RFC3339))
		}
		return att
	}
	if r.Existence != contract.ResourceExistenceGone && r.GoneAt != nil && r.GoneAt.After(latest) {
		att.Reason = fmt.Sprintf("it disappeared at %s, after session %s created it, and was recreated outside that session", r.GoneAt.UTC().Format(time.RFC3339), shortID(sid))
		return att
	}

	att.Label = contract.AttributionLabelExact
	att.SessionID = &sid
	att.RuleID = &rule
	switch rule {
	case RuleIDInResult:
		att.Reason = fmt.Sprintf("session %s created it: its id appeared in the result of a creating call", shortID(sid))
	case RuleHostInResult:
		att.Reason = fmt.Sprintf("session %s created it: its URL appeared in the result of a creating call", shortID(sid))
	default:
		att.Reason = fmt.Sprintf("session %s created it: a creating call named this %s in its container, and no other session did", shortID(sid), short(r.Kind))
	}
	return att
}

// seenBefore reports whether a successful Muretto scan listed r (first_seen_at)
// more than VetoTolerance before the earliest creating action: the resource
// existed already, so the action did not create it (it may have updated it,
// e.g. a function redeploy). Evidence-only resources were never listed.
func seenBefore(r contract.Resource, earliest time.Time) bool {
	if r.SeenVia == contract.ResourceSeenViaEvidence || r.FirstSeenAt.IsZero() {
		return false
	}
	return r.FirstSeenAt.Before(earliest.Add(-VetoTolerance))
}

func decideEvidenceOnly(r contract.Resource, claims []claim, now time.Time) contract.Attribution {
	att := contract.Attribution{ResourceID: r.ID, ComputedAt: now, Label: contract.AttributionLabelUnknown, Evidence: evidence(claims)}
	bySession := groupBySession(claims)
	if len(bySession) > 1 {
		att.Reason = fmt.Sprintf("%d sessions claim to have created this; not confirmed in a connected account", len(bySession))
		return att
	}
	att.Reason = fmt.Sprintf("only seen in session %s's output; not confirmed in a connected account", shortID(claims[0].action.SessionID))
	return att
}

func groupBySession(cs []claim) map[string][]claim {
	m := map[string][]claim{}
	for _, c := range cs {
		m[c.action.SessionID] = append(m[c.action.SessionID], c)
	}
	return m
}

func evidence(cs []claim) []contract.Evidence {
	out := make([]contract.Evidence, 0, len(cs))
	seen := map[string]bool{}
	for _, c := range cs {
		k := c.action.ID + "\x00" + c.ident.SignatureID + "\x00" + c.ident.Value
		if seen[k] {
			continue
		}
		seen[k] = true
		mv := c.ident.Value
		if c.ident.Container != nil && *c.ident.Container != "" && !strings.HasPrefix(mv, *c.ident.Container+"/") {
			mv = *c.ident.Container + "/" + mv
		}
		ex := ""
		if c.ident.Excerpt != nil {
			ex = *c.ident.Excerpt
		} else {
			// No excerpt of its own: show only the text around the matched
			// value, never the whole tool input.
			in := excerptWindow(c.action.InputExcerpt, c.ident.Value)
			res := excerptWindow(c.action.ResultExcerpt, c.ident.Value)
			switch {
			case in != "" && res != "":
				ex = in + "\n→ " + res
			case res != "":
				ex = res
			default:
				ex = in
			}
		}
		out = append(out, contract.Evidence{
			ActionID:     c.action.ID,
			SessionID:    c.action.SessionID,
			ToolName:     c.action.ToolName,
			OccurredAt:   c.action.OccurredAt.UTC(),
			SignatureID:  c.ident.SignatureID,
			MatchedValue: clip(mv, 300),
			Excerpt:      clip(ex, 4000),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.Before(out[j].OccurredAt)
		}
		return out[i].ActionID < out[j].ActionID
	})
	return out
}

// windowRadius is how many bytes of context an evidence excerpt keeps on each
// side of the matched value when it falls back to an action's excerpts.
const windowRadius = 200

// excerptWindow returns the text within windowRadius bytes of value in *s (or
// its start when value isn't there), redacted again as defence in depth: the
// excerpts were redacted when recorded, but evidence is shown and synced.
func excerptWindow(s *string, value string) string {
	if s == nil || *s == "" {
		return ""
	}
	t := *s
	from, to := 0, min(len(t), 2*windowRadius)
	if i := strings.Index(t, value); value != "" && i >= 0 {
		from, to = max(0, i-windowRadius), min(len(t), i+len(value)+windowRadius)
	}
	for from > 0 && (t[from]&0xC0) == 0x80 { // keep whole UTF-8 runes
		from--
	}
	for to < len(t) && (t[to]&0xC0) == 0x80 {
		to++
	}
	return redact.String(t[from:to])
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}

func short(kind string) string {
	if _, k, ok := strings.Cut(kind, "."); ok {
		return strings.ReplaceAll(k, "_", " ")
	}
	return kind
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
