// Package fixture builds valid, obviously fake sync batches for server tests.
package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// T0 is the fixed time fixtures use.
var T0 = time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func rev(v any) contract.Rev {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return contract.Rev(hex.EncodeToString(s[:]))
}

func hash16(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// Batch returns a batch from developer dev with one session that created one
// exposed Supabase project (resource id "supabase.project:<projectRef>").
func Batch(dev, projectRef string) contract.SyncBatch {
	sessionID := "00000000-0000-4000-8000-" + hash16(dev)[:12]
	resID := "supabase.project:" + projectRef
	session := contract.Session{
		ID: sessionID, Agent: contract.SessionAgentClaudeCode, DeveloperID: "dev-" + dev,
		StartedAt: T0, TitleSource: contract.SessionTitleSourceFirstPrompt,
		TaskTitle: ptr("Add a customers table"), Sources: []contract.SessionSourcesElem{contract.SessionSourcesElemHook},
		Repo: &contract.SessionRepo{PathHash: hash16("repo-" + dev), DisplayName: ptr("example/app")},
	}
	resource := contract.Resource{
		ID: resID, Provider: "supabase", AccountID: "org-fake", Kind: "supabase.project", ProviderID: projectRef,
		Name: "demo-" + dev, Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList,
		FirstSeenAt: T0, LastSeenAt: T0, Attributes: contract.ResourceAttributes{"region": "us-east-1"},
	}
	attribution := contract.Attribution{
		ResourceID: resID, Label: contract.AttributionLabelExact, SessionID: ptr(sessionID), Reason: "id in tool result",
		ComputedAt: T0, Evidence: []contract.Evidence{{
			ActionID: "toolu_fake_" + dev, SessionID: sessionID, OccurredAt: T0, ToolName: "Bash",
			SignatureID: "supabase.projects.create", MatchedValue: projectRef, Excerpt: "supabase projects create demo",
		}},
	}
	finding := contract.ExposureFinding{
		ID: "rls_disabled:" + resID, ResourceID: resID, RuleID: "rls_disabled", State: contract.ExposureFindingStateExposed,
		Severity: contract.ExposureFindingSeverityHigh, Title: "RLS disabled", Explanation: "Table readable by anon.",
		Signal: contract.ExposureFindingSignalOfficialAdvisor, CheckedAt: ptr(T0),
	}
	cost := contract.CostRecord{ResourceID: resID, Source: contract.CostRecordSourceEstimated, AmountMicros: ptr(10_000_000),
		Currency: ptr("USD"), Interval: ptr(contract.CostRecordIntervalMonthly), AsOf: T0, Basis: "add-on price"}
	review := contract.ReviewItem{ResourceID: resID, DueAt: T0.Add(-time.Hour), State: contract.ReviewItemStatePending}
	check := contract.ProviderCheck{Provider: "supabase", AccountID: "org-fake", Kind: "supabase.project",
		Status: contract.ProviderCheckStatusOk, AttemptedAt: T0, LastSuccessAt: ptr(T0), Items: ptr(1)}

	return contract.SyncBatch{
		SchemaVersion: 1, BatchID: hash16("batch-"+dev+projectRef) + hash16(projectRef), SentAt: T0, ClientVersion: "test",
		Developer:    contract.Developer{ID: "dev-" + dev, DisplayName: dev, MachineIDHash: hash16("machine-" + dev)},
		Sessions:     []contract.SyncSession{{Rev: rev(session), Entity: session}},
		Resources:    []contract.SyncResource{{Rev: rev(resource), Entity: resource}},
		Attributions: []contract.SyncAttribution{{Rev: rev(attribution), Entity: attribution}},
		Findings:     []contract.SyncFinding{{Rev: rev(finding), Entity: finding}},
		Costs:        []contract.SyncCost{{Rev: rev(cost), Entity: cost}},
		Reviews:      []contract.SyncReview{{Rev: rev(review), Entity: review}},
		Checks:       []contract.SyncCheck{{Rev: rev(check), Entity: check}},
	}
}

// JSON marshals v or panics.
func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
