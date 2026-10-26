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
// sessions. Records of the same entity from several users are merged: resource
// ids are deterministic, so two developers can sync the same resource.
func Compose(recs []store.Record, names map[string]string, viewer Viewer, now time.Time) (contract.Answer, error) {
	sessions := map[string]sessionRow{}
	resources := map[string]contract.Resource{}
	attributions := map[string]contract.Attribution{}
	findings := map[string]contract.ExposureFinding{}
	costs := map[string]contract.CostRecord{}
	reviews := map[string]contract.ReviewItem{}
	checks := map[string]contract.ProviderCheck{}

	for _, r := range recs {
		var err error
		switch r.Kind {
		case store.KindSession:
			var v contract.Session
			if err = json.Unmarshal(r.Data, &v); err == nil {
				if old, ok := sessions[v.ID]; !ok || activity(v).After(activity(old.s)) {
					sessions[v.ID] = sessionRow{s: v, owner: r.UserID}
				}
			}
		case store.KindResource:
			var v contract.Resource
			if err = json.Unmarshal(r.Data, &v); err == nil {
				if old, ok := resources[v.ID]; !ok || v.LastSeenAt.After(old.LastSeenAt) {
					resources[v.ID] = v
				}
			}
		case store.KindAttribution:
			var v contract.Attribution
			if err = json.Unmarshal(r.Data, &v); err == nil {
				if old, ok := attributions[v.ResourceID]; !ok || betterAttribution(v, old) {
					attributions[v.ResourceID] = v
				}
			}
		case store.KindFinding:
			var v contract.ExposureFinding
			if err = json.Unmarshal(r.Data, &v); err == nil {
				if old, ok := findings[v.ID]; !ok || timeOf(v.CheckedAt).After(timeOf(old.CheckedAt)) {
					findings[v.ID] = v
				}
			}
		case store.KindCost:
			var v contract.CostRecord
			if err = json.Unmarshal(r.Data, &v); err == nil {
				k := store.CostKey(v)
				if old, ok := costs[k]; !ok || v.AsOf.After(old.AsOf) {
					costs[k] = v
				}
			}
		case store.KindReview:
			var v contract.ReviewItem
			if err = json.Unmarshal(r.Data, &v); err == nil {
				if old, ok := reviews[v.ResourceID]; !ok || timeOf(v.DecidedAt).After(timeOf(old.DecidedAt)) {
					reviews[v.ResourceID] = v
				}
			}
		case store.KindCheck:
			var v contract.ProviderCheck
			if err = json.Unmarshal(r.Data, &v); err == nil {
				k := store.CheckKey(v)
				if old, ok := checks[k]; !ok || v.AttemptedAt.After(old.AttemptedAt) {
					checks[k] = v
				}
			}
		default:
			continue
		}
		if err != nil {
			return contract.Answer{}, fmt.Errorf("record %s %s: %w", r.Kind, r.Key, err)
		}
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
			at := c.AttemptedAt
			if c.LastSuccessAt != nil {
				at = *c.LastSuccessAt
			}
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

// betterAttribution prefers exact over unknown, then the newest.
func betterAttribution(a, b contract.Attribution) bool {
	ae, be := a.Label == contract.AttributionLabelExact, b.Label == contract.AttributionLabelExact
	if ae != be {
		return ae
	}
	return a.ComputedAt.After(b.ComputedAt)
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
