// Package view composes an Answer-shaped document from synced records, for
// one user ("me") or the union of an org's members ("team").
package view

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/store"
)

// Viewer is who the Answer is composed for; it fills Answer.developer.
type Viewer struct {
	UserID      string
	Login       string
	DisplayName string
}

// Developer returns the Answer.developer for a server-composed view. The id is
// the server user id; machine_id_hash is derived from it (no machine is
// involved server-side).
func (v Viewer) Developer() contract.Developer {
	sum := sha256.Sum256([]byte("muretto-server-view:" + v.UserID))
	name := v.DisplayName
	if name == "" {
		name = v.Login
	}
	if name == "" {
		name = v.UserID
	}
	d := contract.Developer{ID: v.UserID, DisplayName: name, MachineIDHash: hex.EncodeToString(sum[:16])}
	if v.Login != "" {
		login := v.Login
		d.AccountLogin = &login
	}
	return d
}

type sessionRow struct {
	s     contract.Session
	owner string
}

// Compose builds the Answer. names maps user id to the developer name shown on
// sessions. Copies of one entity synced by several users (resource ids are
// deterministic) are merged conservatively; see merge.go.
func Compose(recs []store.Record, names map[string]string, viewer Viewer, now time.Time) (contract.Answer, error) {
	sessionsIn := map[string][]owned[contract.Session]{}
	resourcesIn := map[string][]owned[contract.Resource]{}
	attributionsIn := map[string][]owned[contract.Attribution]{}
	findingsIn := map[string][]owned[contract.ExposureFinding]{}
	costsIn := map[string][]owned[contract.CostRecord]{}
	reviewsIn := map[string][]owned[contract.ReviewItem]{}
	checksIn := map[string][]owned[contract.ProviderCheck]{}

	for _, r := range recs {
		recv := r.UpdatedAt
		if recv.IsZero() || recv.After(now) {
			recv = now
		}
		var err error
		switch r.Kind {
		case store.KindSession:
			err = collect(r, recv, sessionsIn, func(v contract.Session) string { return v.ID })
		case store.KindResource:
			err = collect(r, recv, resourcesIn, func(v contract.Resource) string { return v.ID })
		case store.KindAttribution:
			err = collect(r, recv, attributionsIn, func(v contract.Attribution) string { return v.ResourceID })
		case store.KindFinding:
			err = collect(r, recv, findingsIn, func(v contract.ExposureFinding) string { return v.ID })
		case store.KindCost:
			err = collect(r, recv, costsIn, store.CostKey)
		case store.KindReview:
			err = collect(r, recv, reviewsIn, func(v contract.ReviewItem) string { return v.ResourceID })
		case store.KindCheck:
			err = collect(r, recv, checksIn, store.CheckKey)
		default:
			continue
		}
		if err != nil {
			return contract.Answer{}, fmt.Errorf("record %s %s: %w", r.Kind, r.Key, err)
		}
	}

	sessions := map[string]sessionRow{}
	for id, x := range mergeAll(sessionsIn, sessionKey) {
		sessions[id] = sessionRow{s: x.v, owner: x.owner}
	}
	resources := values(mergeAll(resourcesIn, resourceKey))
	findings := values(mergeAll(findingsIn, findingKey))
	costs := values(mergeAll(costsIn, costKey))
	reviews := values(mergeAll(reviewsIn, reviewKey))
	checksOwned := mergeAll(checksIn, checkKey)
	checks := values(checksOwned)
	attributions := map[string]contract.Attribution{}
	for id, in := range attributionsIn {
		attributions[id] = mergeAttribution(in)
	}

	a := contract.Answer{
		SchemaVersion: 1,
		GeneratedAt:   now.UTC().Truncate(time.Second),
		Developer:     viewer.Developer(),
		Sessions:      []contract.SessionView{},
		Resources:     sortedValues(resources),
		Attributions:  sortedValues(attributions),
		Findings:      sortedValues(findings),
		Costs:         sortedValues(costs),
		Reviews:       sortedValues(reviews),
		Checks:        sortedValues(checks),
		Warnings:      []contract.Warning{},
	}

	exposedByResource := map[string]int{}
	for _, f := range a.Findings {
		if f.State == contract.ExposureFindingStateExposed {
			exposedByResource[f.ResourceID]++
			a.Summary.Exposed++
		}
	}
	dueByResource := map[string]int{}
	for _, r := range a.Reviews {
		if r.State == contract.ReviewItemStatePending && !r.DueAt.After(now) {
			dueByResource[r.ResourceID]++
			a.Summary.NeedsReview++
		}
	}
	bySession := map[string][]string{}
	for _, at := range a.Attributions {
		if at.Label != contract.AttributionLabelExact {
			continue
		}
		a.Summary.AgentCreated++
		if res, ok := resources[at.ResourceID]; ok && res.Existence == contract.ResourceExistenceConfirmed {
			a.Summary.StillExisting++
		}
		if at.SessionID != nil {
			bySession[*at.SessionID] = append(bySession[*at.SessionID], at.ResourceID)
		}
	}
	for _, r := range a.Resources {
		if at, ok := attributions[r.ID]; !ok || at.Label != contract.AttributionLabelExact {
			a.Summary.UnknownCreator++
		}
	}
	for _, c := range a.Checks {
		switch c.Status {
		case contract.ProviderCheckStatusOk:
			a.Summary.ChecksOk++
			at := checkFreshness(checksOwned[store.CheckKey(c)])
			if a.Summary.OldestCheckAt == nil || at.Before(*a.Summary.OldestCheckAt) {
				a.Summary.OldestCheckAt = &at
			}
		case contract.ProviderCheckStatusError:
			a.Summary.ChecksFailed++
		case contract.ProviderCheckStatusSkipped:
		}
	}

	rows := make([]sessionRow, 0, len(sessions))
	for _, s := range sessions {
		rows = append(rows, s)
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].s.StartedAt.Equal(rows[j].s.StartedAt) {
			return rows[i].s.StartedAt.After(rows[j].s.StartedAt)
		}
		return rows[i].s.ID < rows[j].s.ID
	})
	for _, row := range rows {
		ids := bySession[row.s.ID]
		sort.Strings(ids)
		if ids == nil {
			ids = []string{}
		}
		sv := contract.SessionView{Session: row.s, DeveloperName: names[row.owner], ResourceIds: ids}
		for _, id := range ids {
			sv.ExposedCount += exposedByResource[id]
			sv.ReviewDueCount += dueByResource[id]
		}
		a.Sessions = append(a.Sessions, sv)
	}
	a.Summary.Sessions = len(a.Sessions)
	return a, nil
}

func activity(s contract.Session) time.Time {
	if s.LastActivityAt != nil {
		return *s.LastActivityAt
	}
	return s.StartedAt
}

func timeOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// collect decodes one record into in, keyed by key(entity).
func collect[T any](r store.Record, recv time.Time, in map[string][]owned[T], key func(T) string) error {
	var v T
	if err := json.Unmarshal(r.Data, &v); err != nil {
		return err
	}
	k := key(v)
	in[k] = append(in[k], owned[T]{v: v, owner: r.UserID, recv: recv})
	return nil
}

func sortedValues[T any](m map[string]T) []T {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]T, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
