package observe

import (
	"fmt"
	"sort"

	"github.com/tolgayayci/muretto/internal/contract"
)

var titleRank = map[contract.SessionTitleSource]int{
	contract.SessionTitleSourceCustomTitle: 3,
	contract.SessionTitleSourceFirstPrompt: 2,
	contract.SessionTitleSourceAiTitle:     1,
	contract.SessionTitleSourceNone:        0,
}

// Merge combines batches (typically IngestSpool + ScanTranscripts) into one:
//
//   - Sessions with the same id are merged. sources is the union; started_at is
//     the earliest, last_activity_at the latest; version, model and branch come
//     from whichever source has them (only transcripts do); the title is the
//     best by custom title > first prompt > AI title.
//   - Actions are deduplicated on tool_use_id. When the same id appears in
//     several sessions (--fork-session copies history), the session that really
//     ran it wins: a hook record beats a transcript copy (hooks fire once, in
//     the running session), then the earliest action time, then the earliest
//     session start. A tie that remains is resolved deterministically and
//     reported as a warning.
//   - Files and warnings are concatenated.
func Merge(batches ...Batch) Batch {
	var out Batch
	sessions := map[string]*contract.Session{}
	var order []string
	for _, b := range batches {
		for _, s := range b.Sessions {
			if cur, ok := sessions[s.ID]; ok {
				mergeSession(cur, s)
				continue
			}
			cp := s
			cp.Sources = append([]contract.SessionSourcesElem(nil), s.Sources...)
			sessions[s.ID] = &cp
			order = append(order, s.ID)
		}
		out.Files = append(out.Files, b.Files...)
		out.Warnings = append(out.Warnings, b.Warnings...)
		out.Ambiguous = append(out.Ambiguous, b.Ambiguous...)
	}

	// Group actions by tool_use_id, keeping first-seen order.
	groups := map[string][]RawAction{}
	var ids []string
	for _, b := range batches {
		for _, a := range b.Actions {
			if _, ok := groups[a.Action.ID]; !ok {
				ids = append(ids, a.Action.ID)
			}
			groups[a.Action.ID] = append(groups[a.Action.ID], a)
		}
	}
	for _, id := range ids {
		g := groups[id]
		win, ambiguous := pickAction(g, sessions)
		if ambiguous {
			out.Ambiguous = append(out.Ambiguous, id)
			out.Warnings = append(out.Warnings, warn("action_owner_ambiguous",
				fmt.Sprintf("tool call %s appears in %d sessions with identical timing; kept session %s", id, len(g), win.Action.SessionID)))
		}
		out.Actions = append(out.Actions, win)
	}
	out.Ambiguous = uniqueStrings(out.Ambiguous)
	for _, id := range order {
		out.Sessions = append(out.Sessions, *sessions[id])
	}
	return out
}

func mergeSession(cur *contract.Session, s contract.Session) {
	for _, src := range s.Sources {
		cur.Sources = addSource(cur.Sources, src)
	}
	if !s.StartedAt.IsZero() && (cur.StartedAt.IsZero() || s.StartedAt.Before(cur.StartedAt)) {
		cur.StartedAt = s.StartedAt
	}
	if s.LastActivityAt != nil && (cur.LastActivityAt == nil || s.LastActivityAt.After(*cur.LastActivityAt)) {
		cur.LastActivityAt = s.LastActivityAt
	}
	if s.EndedAt != nil && (cur.EndedAt == nil || s.EndedAt.After(*cur.EndedAt)) {
		cur.EndedAt = s.EndedAt
		if s.EndReason != nil {
			cur.EndReason = s.EndReason
		}
	}
	if cur.EndReason == nil {
		cur.EndReason = s.EndReason
	}
	if s.AgentVersion != nil {
		cur.AgentVersion = s.AgentVersion
	}
	if s.Model != nil {
		cur.Model = s.Model
	}
	if s.GitBranch != nil {
		cur.GitBranch = s.GitBranch
	}
	if cur.Repo == nil {
		cur.Repo = s.Repo
	} else if s.Repo != nil {
		if cur.Repo.RemoteUrl == nil {
			cur.Repo.RemoteUrl = s.Repo.RemoteUrl
		}
		if cur.Repo.DisplayName == nil {
			cur.Repo.DisplayName = s.Repo.DisplayName
		}
	}
	if titleRank[s.TitleSource] > titleRank[cur.TitleSource] {
		cur.TaskTitle, cur.TitleSource = s.TaskTitle, s.TitleSource
	}
}

// pickAction chooses the record of a tool call to keep.
func pickAction(g []RawAction, sessions map[string]*contract.Session) (RawAction, bool) {
	if len(g) == 1 {
		return g[0], false
	}
	start := func(a RawAction) int64 {
		if s, ok := sessions[a.Action.SessionID]; ok && !s.StartedAt.IsZero() {
			return s.StartedAt.UnixNano()
		}
		return 1<<63 - 1
	}
	hookFirst := func(a RawAction) int {
		if a.Action.Source == contract.AgentActionSourceHook {
			return 0
		}
		return 1
	}
	c := append([]RawAction(nil), g...)
	sort.SliceStable(c, func(i, j int) bool {
		a, b := c[i], c[j]
		if a.Action.SessionID != b.Action.SessionID {
			if ha, hb := hookFirst(a), hookFirst(b); ha != hb {
				return ha < hb
			}
			if !a.Action.OccurredAt.Equal(b.Action.OccurredAt) {
				return a.Action.OccurredAt.Before(b.Action.OccurredAt)
			}
			if sa, sb := start(a), start(b); sa != sb {
				return sa < sb
			}
			return a.Action.SessionID < b.Action.SessionID
		}
		return hookFirst(a) < hookFirst(b)
	})
	win := c[0]
	// Fill gaps in the winner from other records of the same session.
	for _, o := range c[1:] {
		if o.Action.SessionID != win.Action.SessionID {
			continue
		}
		if win.Output == "" && o.Output != "" {
			win.Output = o.Output
			win.Action.ResultExcerpt = o.Action.ResultExcerpt
		}
		if win.Input == nil && o.Input != nil {
			win.Input = o.Input
			win.Action.InputExcerpt = o.Action.InputExcerpt
		}
		if win.Action.AgentID == nil {
			win.Action.AgentID = o.Action.AgentID
		}
	}
	// Ambiguous: another session's copy is indistinguishable from the winner.
	ambiguous := false
	for _, o := range c[1:] {
		if o.Action.SessionID != win.Action.SessionID && hookFirst(o) == hookFirst(win) &&
			o.Action.OccurredAt.Equal(win.Action.OccurredAt) && start(o) == start(win) {
			ambiguous = true
		}
	}
	return win, ambiguous
}

// uniqueStrings returns ss without duplicates, keeping first-seen order.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
