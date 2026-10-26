package attrib_test

import (
	"testing"
	"time"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/attrib"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

// TestProofOfCreation runs real tool calls through the shipped adapters and
// attribution together: a session gets exact only when its action proves it
// created the resource (docs/decisions/0406-proof-of-creation.md).
func TestProofOfCreation(t *testing.T) {
	reg, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	const ref = "abcdefghijklmnopqrst"
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	actAt := now.Add(-time.Hour)
	pid := "supabase.project:" + ref
	project := contract.Resource{ID: pid, Kind: "supabase.project", Provider: "supabase", ProviderID: ref, Name: "app",
		Attributes: contract.ResourceAttributes{}, Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList, FirstSeenAt: now.Add(-48 * time.Hour), LastSeenAt: now}
	table := func(name string, firstSeen time.Time) contract.Resource {
		return contract.Resource{ID: "supabase.table:" + ref + "/public." + name, Kind: "supabase.table", Provider: "supabase", ProviderID: ref + "/public." + name,
			Name: "public." + name, ParentID: &pid, Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaAdvisor,
			FirstSeenAt: firstSeen, LastSeenAt: now,
			Attributes: contract.ResourceAttributes{"schema": "public", "table": name, "project_ref": ref}}
	}
	function := func(slug string, firstSeen time.Time) contract.Resource {
		return contract.Resource{ID: "supabase.function:" + ref + "/" + slug, Kind: "supabase.function", Provider: "supabase", ProviderID: ref + "/" + slug,
			Name: slug, ParentID: &pid, Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList,
			FirstSeenAt: firstSeen, LastSeenAt: now,
			Attributes: contract.ResourceAttributes{"slug": slug, "project_ref": ref}}
	}
	bash := func(cmd, out string) observe.RawAction {
		return observe.RawAction{
			Action: contract.AgentAction{ID: "toolu_FAKE1", SessionID: "sessA", ToolName: "Bash", Status: contract.AgentActionStatusOk, OccurredAt: actAt, Source: contract.AgentActionSourceHook},
			Input:  map[string]any{"command": cmd},
			Output: out,
		}
	}
	mcp := func(tool string, input map[string]any, out string) observe.RawAction {
		return observe.RawAction{
			Action: contract.AgentAction{ID: "toolu_FAKE2", SessionID: "sessA", ToolName: "mcp__supabase__" + tool, Status: contract.AgentActionStatusOk, OccurredAt: actAt, Source: contract.AgentActionSourceHook},
			Input:  input,
			Output: out,
		}
	}
	after := now.Add(-30 * time.Minute) // first listed by a scan after the action
	cases := []struct {
		name string
		act  observe.RawAction
		res  contract.Resource
		want contract.AttributionLabel
	}{
		{"IF NOT EXISTS on an existing table", bash(`supabase db query "CREATE TABLE IF NOT EXISTS public.users (id int)" --project-ref `+ref, `{"rows":[]}`), table("users", after), contract.AttributionLabelUnknown},
		{"create table in a line comment", bash(`supabase db query "select count(*) from users -- create table users later" --project-ref `+ref, "count\n42\n"), table("users", after), contract.AttributionLabelUnknown},
		{"create table in a string constant", bash(`supabase db query "select 'create table users'" --project-ref `+ref, "ok"), table("users", after), contract.AttributionLabelUnknown},
		{"plain CREATE TABLE, first listed after", bash(`supabase db query "CREATE TABLE public.users (id int)" --project-ref `+ref, `{"rows":[]}`), table("users", after), contract.AttributionLabelExact},
		{"plain CREATE TABLE on a table Muretto listed earlier", bash(`supabase db query "CREATE TABLE public.users (id int)" --project-ref `+ref, `{"rows":[]}`), table("users", now.Add(-24*time.Hour)), contract.AttributionLabelUnknown},
		{"MCP execute_sql error result", mcp("execute_sql", map[string]any{"project_id": ref, "query": "create table public.users (id int)"}, `{"error":{"message":"relation \"users\" already exists"}}`), table("users", after), contract.AttributionLabelUnknown},
		{"MCP execute_sql create", mcp("execute_sql", map[string]any{"project_id": ref, "query": "create table public.users (id int)"}, `{"result":"[]"}`), table("users", after), contract.AttributionLabelExact},
		{"functions deploy that updated a listed function", bash("supabase functions deploy hello --project-ref "+ref, "Deployed Functions on project "+ref+": hello\n"), function("hello", now.Add(-24*time.Hour)), contract.AttributionLabelUnknown},
		{"functions deploy of a new function", bash("supabase functions deploy hello --project-ref "+ref, "Deployed Functions on project "+ref+": hello\n"), function("hello", after), contract.AttributionLabelExact},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := c.act.Action
			a.Identifiers = reg.Extract(c.act)
			atts, _ := attrib.Compute([]contract.Session{{ID: "sessA", StartedAt: actAt.Add(-time.Minute)}}, []contract.AgentAction{a}, []contract.Resource{project, c.res}, now)
			for _, at := range atts {
				if at.ResourceID != c.res.ID {
					continue
				}
				if at.Label != c.want {
					t.Errorf("label %s (%s), want %s; identifiers %+v", at.Label, at.Reason, c.want, a.Identifiers)
				}
				return
			}
			t.Fatalf("no attribution for %s", c.res.ID)
		})
	}
}
