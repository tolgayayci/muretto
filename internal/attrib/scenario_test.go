package attrib

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// The scenario files in testdata/scenarios use a small line format, so a
// reviewer can read each case without knowing Go:
//
//	=== name of the scenario
//	# comment
//	session <id> start=<time>
//	action <id> session=<id> at=<time> tool=<name> [status=ok|error] [agent=<subagent>]
//	  created|referenced <kind> value=<v> [container=<c>] [sig=<id>]
//	resource <kind>:<provider_id> [name=] [account=] [account_name=] [account_slug=] [host=] [parent=<id>]
//	         [created=<time>] [existence=confirmed|gone|unconfirmed] [gone=<time>] [seen=list|advisor]
//	expect <resource id> exact <session> <rule>
//	expect <resource id> unknown
//	expect-evidence-only <resource id> [session=<id>]
//
// Times are HH:MM on 2026-10-20 UTC, or dN/HH:MM for N days later.
// Every resource (listed and evidence-only) must have an expect line.

var base = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

type scenario struct {
	name      string
	file      string
	line      int
	sessions  []contract.Session
	actions   []contract.AgentAction
	resources []contract.Resource
	expect    map[string]expectation
	evOnly    map[string]string // id -> session of its evidence ("" = any)
}

type expectation struct {
	label   contract.AttributionLabel
	session string
	rule    string
}

func parseTime(s string) (time.Time, error) {
	day := 0
	if strings.HasPrefix(s, "d") {
		if _, err := fmt.Sscanf(s, "d%d/", &day); err != nil {
			return time.Time{}, err
		}
		s = s[strings.Index(s, "/")+1:]
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return time.Time{}, fmt.Errorf("bad time %q", s)
	}
	return base.AddDate(0, 0, day).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute), nil
}

func kv(fields []string) (map[string]string, []string) {
	m := map[string]string{}
	var pos []string
	for _, f := range fields {
		if k, v, ok := strings.Cut(f, "="); ok {
			m[k] = v
		} else {
			pos = append(pos, f)
		}
	}
	return m, pos
}

func ptr[T any](v T) *T { return &v }

func loadScenarios(t *testing.T) []*scenario {
	t.Helper()
	files, err := filepath.Glob("testdata/scenarios/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenario files: %v", err)
	}
	var all []*scenario
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		var cur *scenario
		var lastAction *contract.AgentAction
		sc := bufio.NewScanner(f)
		n := 0
		fail := func(format string, a ...any) {
			t.Fatalf("%s:%d: %s", file, n, fmt.Sprintf(format, a...))
		}
		mustTime := func(s string) time.Time {
			tm, err := parseTime(s)
			if err != nil {
				fail("%v", err)
			}
			return tm
		}
		for sc.Scan() {
			n++
			raw := sc.Text()
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "=== ") {
				cur = &scenario{name: strings.TrimPrefix(line, "=== "), file: file, line: n, expect: map[string]expectation{}, evOnly: map[string]string{}}
				all = append(all, cur)
				lastAction = nil
				continue
			}
			if cur == nil {
				fail("content before the first === line")
			}
			fields := strings.Fields(line)
			opts, pos := kv(fields[1:])
			switch fields[0] {
			case "session":
				cur.sessions = append(cur.sessions, contract.Session{
					ID: pos[0], DeveloperID: "dev", Agent: contract.SessionAgentClaudeCode,
					StartedAt: mustTime(opts["start"]), Sources: []contract.SessionSourcesElem{contract.SessionSourcesElemHook},
					TitleSource: contract.SessionTitleSourceNone,
				})
			case "action":
				status := contract.AgentActionStatusOk
				if opts["status"] == "error" {
					status = contract.AgentActionStatusError
				}
				a := contract.AgentAction{
					ID: pos[0], SessionID: opts["session"], OccurredAt: mustTime(opts["at"]), ToolName: opts["tool"],
					Status: status, Source: contract.AgentActionSourceHook, Identifiers: []contract.Identifier{},
				}
				if ag, ok := opts["agent"]; ok {
					a.AgentID = ptr(ag)
				}
				cur.actions = append(cur.actions, a)
				lastAction = &cur.actions[len(cur.actions)-1]
			case "created", "referenced":
				if lastAction == nil || !strings.HasPrefix(raw, " ") {
					fail("identifier must be indented under an action")
				}
				role := contract.IdentifierRoleCreated
				if fields[0] == "referenced" {
					role = contract.IdentifierRoleReferenced
				}
				kind := pos[0]
				prov, _, _ := strings.Cut(kind, ".")
				id := contract.Identifier{Kind: kind, Provider: prov, Value: opts["value"], Role: role, SignatureID: opts["sig"]}
				if id.SignatureID == "" {
					id.SignatureID = "test." + fields[0]
				}
				if c, ok := opts["container"]; ok {
					id.Container = ptr(c)
				}
				lastAction.Identifiers = append(lastAction.Identifiers, id)
			case "resource":
				id := pos[0]
				kind, pid, ok := strings.Cut(id, ":")
				if !ok {
					fail("resource id must be kind:provider_id")
				}
				prov, _, _ := strings.Cut(kind, ".")
				r := contract.Resource{
					ID: id, Kind: kind, Provider: prov, ProviderID: pid, Name: opts["name"], AccountID: opts["account"],
					Attributes: contract.ResourceAttributes{}, Existence: contract.ResourceExistenceConfirmed,
					SeenVia: contract.ResourceSeenViaList, FirstSeenAt: base, LastSeenAt: base,
				}
				if v, ok := opts["account_name"]; ok {
					r.AccountName = ptr(v)
				}
				if v, ok := opts["account_slug"]; ok {
					r.Attributes["account_slug"] = v
				}
				if v, ok := opts["host"]; ok {
					r.Attributes["host"] = v
				}
				if v, ok := opts["parent"]; ok {
					r.ParentID = ptr(v)
				}
				if v, ok := opts["created"]; ok {
					r.CreatedAt = ptr(mustTime(v))
				}
				if v, ok := opts["existence"]; ok {
					r.Existence = contract.ResourceExistence(v)
				}
				if v, ok := opts["gone"]; ok {
					r.GoneAt = ptr(mustTime(v))
				}
				if v, ok := opts["seen"]; ok {
					r.SeenVia = contract.ResourceSeenVia(v)
				}
				cur.resources = append(cur.resources, r)
			case "expect":
				e := expectation{label: contract.AttributionLabel(pos[1])}
				if e.label == contract.AttributionLabelExact {
					if len(pos) != 4 {
						fail("expect exact needs <session> <rule>")
					}
					e.session, e.rule = pos[2], pos[3]
				}
				cur.expect[pos[0]] = e
			case "expect-evidence-only":
				cur.evOnly[pos[0]] = opts["session"]
			default:
				fail("unknown directive %q", fields[0])
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	return all
}

func TestScenarios(t *testing.T) {
	scenarios := loadScenarios(t)
	if len(scenarios) < 30 {
		t.Fatalf("want at least 30 scenarios, have %d", len(scenarios))
	}
	now := base.AddDate(0, 0, 3)
	wrongExact, exactTotal, unknownTotal := 0, 0, 0
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			atts, extra := Compute(sc.sessions, sc.actions, sc.resources, now)
			got := map[string]contract.Attribution{}
			for _, a := range atts {
				if _, dup := got[a.ResourceID]; dup {
					t.Errorf("two attributions for %s", a.ResourceID)
				}
				got[a.ResourceID] = a
			}
			if len(atts) != len(sc.resources)+len(extra) {
				t.Errorf("want one attribution per resource: %d attributions, %d resources", len(atts), len(sc.resources)+len(extra))
			}
			gotEv := map[string]contract.Resource{}
			for _, r := range extra {
				gotEv[r.ID] = r
				if r.Existence != contract.ResourceExistenceUnconfirmed || r.SeenVia != contract.ResourceSeenViaEvidence {
					t.Errorf("evidence-only %s: existence=%s seen_via=%s", r.ID, r.Existence, r.SeenVia)
				}
			}
			for id, wantSess := range sc.evOnly {
				if _, ok := gotEv[id]; !ok {
					t.Errorf("missing evidence-only resource %s (got %v)", id, keys(gotEv))
					continue
				}
				if wantSess != "" {
					a := got[id]
					if len(a.Evidence) == 0 || a.Evidence[0].SessionID != wantSess {
						t.Errorf("evidence-only %s: want evidence from %s, got %+v", id, wantSess, a.Evidence)
					}
				}
			}
			for id := range gotEv {
				if _, ok := sc.evOnly[id]; !ok {
					t.Errorf("unexpected evidence-only resource %s", id)
				}
			}
			for id, a := range got {
				want, ok := sc.expect[id]
				if !ok {
					if _, ev := gotEv[id]; ev && a.Label == contract.AttributionLabelUnknown {
						continue // evidence-only resources are unknown by definition
					}
					t.Errorf("%s: no expect line (got %s: %s)", id, a.Label, a.Reason)
					continue
				}
				if a.Reason == "" {
					t.Errorf("%s: empty reason", id)
				}
				if a.Label == contract.AttributionLabelExact {
					exactTotal++
					if len(a.Evidence) == 0 || a.SessionID == nil || a.RuleID == nil {
						t.Errorf("%s: exact without evidence, session or rule", id)
					}
					if want.label != contract.AttributionLabelExact || a.SessionID == nil || *a.SessionID != want.session {
						wrongExact++
						t.Errorf("CONFIDENTLY WRONG %s: got exact %v, want %s %s (%s)", id, deref(a.SessionID), want.label, want.session, a.Reason)
						continue
					}
					if *a.RuleID != want.rule {
						t.Errorf("%s: rule %s, want %s", id, *a.RuleID, want.rule)
					}
				} else {
					unknownTotal++
					if a.SessionID != nil {
						t.Errorf("%s: unknown label with a session id", id)
					}
					if want.label != contract.AttributionLabelUnknown {
						t.Errorf("%s: got unknown (%s), want exact %s", id, a.Reason, want.session)
					}
				}
			}
			for id := range sc.expect {
				if _, ok := got[id]; !ok {
					t.Errorf("expected attribution for %s, none computed", id)
				}
			}
		})
	}
	t.Logf("%d scenarios, %d exact, %d unknown, %d confidently wrong", len(scenarios), exactTotal, unknownTotal, wrongExact)
	if wrongExact != 0 {
		t.Fatalf("%d confidently wrong labels", wrongExact)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
