package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/testserver"
)

// findingBatch is alice's fixture batch reduced to one finding.
func findingBatch(id string, f contract.ExposureFinding) contract.SyncBatch {
	b := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")
	sum := sha256.Sum256(fixture.JSON(f))
	b.BatchID = strings.Repeat(id, 32)[:32]
	b.Sessions, b.Resources, b.Attributions = []contract.SyncSession{}, []contract.SyncResource{}, []contract.SyncAttribution{}
	b.Costs, b.Reviews, b.Checks = []contract.SyncCost{}, []contract.SyncReview{}, []contract.SyncCheck{}
	b.Findings = []contract.SyncFinding{{Rev: contract.Rev(hex.EncodeToString(sum[:])), Entity: f}}
	return b
}

// The review's reproduction: bob, another member, syncs not_exposed for
// alice's exposed finding with checked_at five years ahead. The team view
// must still show it exposed; alice's own later fix clears it.
func TestTeamAnswerMemberCannotHideExposure(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	alice, bob := s.Login(t, "alice"), s.Login(t, "bob")
	var org orgResp
	alice.JSON("POST", "/v1/orgs", map[string]string{"name": "Acme"}, 201, &org)
	var inv struct{ URL string }
	alice.JSON("POST", "/v1/orgs/"+org.ID+"/invites", nil, 201, &inv)
	bob.JSON("POST", "/v1/invites/"+inv.URL[strings.LastIndex(inv.URL, "/")+1:]+"/accept", nil, 200, nil)

	now := s.Clock.Now().UTC().Truncate(time.Second)
	exposed := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Findings[0].Entity
	exposed.CheckedAt = &now
	ingest(t, s, s.Token(t, "alice"), findingBatch("1", exposed))

	s.Clock.Advance(time.Minute)
	future := now.AddDate(5, 0, 0)
	hidden := exposed
	hidden.State, hidden.Severity, hidden.Title, hidden.CheckedAt = contract.ExposureFindingStateNotExposed, contract.ExposureFindingSeverityInfo, "fine", &future
	ingest(t, s, s.Token(t, "bob"), findingBatch("2", hidden))

	team := func() contract.Answer {
		var a contract.Answer
		alice.JSON("GET", "/v1/orgs/"+org.ID+"/answer", nil, 200, &a)
		return a
	}
	if a := team(); len(a.Findings) != 1 || a.Findings[0].State != contract.ExposureFindingStateExposed || a.Summary.Exposed != 1 {
		t.Fatalf("bob hid alice's exposure: %+v %+v", a.Findings, a.Summary)
	}

	// alice fixes the table and re-checks: her own not_exposed replaces her
	// exposure. bob's not_exposed agrees, so the team view clears.
	s.Clock.Advance(time.Minute)
	fixed := hidden
	later := s.Clock.Now().UTC().Truncate(time.Second)
	fixed.CheckedAt = &later
	ingest(t, s, s.Token(t, "alice"), findingBatch("3", fixed))
	if a := team(); a.Findings[0].State != contract.ExposureFindingStateNotExposed || a.Summary.Exposed != 0 {
		t.Fatalf("alice's own fix did not clear: %+v %+v", a.Findings, a.Summary)
	}
}
