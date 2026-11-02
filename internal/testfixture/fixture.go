// Package testfixture is a small, fully fake dataset shaped like the
// `muretto session` target view in docs/BRIEF.md §10. Tests for answer,
// render and the CLI share it. Every id, name and ref here is fake.
package testfixture

import (
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// Now is the fixed clock for fixture-based tests.
var Now = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

// Fixed ids.
const (
	AliceSession = "8f2a1c3e-0000-4000-8000-000000000001"
	BobSession   = "b0b5e551-0000-4000-8000-000000000002"
	EveSession   = "e7e00000-0000-4000-8000-000000000003"
	AliceDev     = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	BobDev       = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	OrgID        = "org-fake-acme"
	TeamID       = "team_FAKEteam000000000000"
	DevRef       = "projaaaaaaaaaaaaaaaa"
	ProdRef      = "projpppppppppppppppp"
	BranchRef    = "brchaaaaaaaaaaaaaaaa"
	DeployID     = "dpl_FAKEdeploya81f00000000001"
	ProjectID    = "prj_FAKEproject00000000000001"
)

func p[T any](v T) *T { return &v }

// Day is when alice's session ran: 7 days before Now.
var Day = Now.AddDate(0, 0, -7)

func at(day time.Time, hh, mm int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, time.UTC)
}

// Developer is the local developer (alice).
func Developer() contract.Developer {
	return contract.Developer{ID: AliceDev, DisplayName: "alice", MachineIDHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

// Names maps developer ids to names.
func Names() map[string]string { return map[string]string{AliceDev: "alice", BobDev: "bob"} }

// Sessions returns the fixture sessions.
func Sessions() []contract.Session {
	hook := []contract.SessionSourcesElem{contract.SessionSourcesElemHook, contract.SessionSourcesElemTranscript}
	return []contract.Session{
		{
			ID: AliceSession, DeveloperID: AliceDev, Agent: contract.SessionAgentClaudeCode, AgentVersion: p("2.1.310"),
			StartedAt: at(Day, 14, 2), EndedAt: p(at(Day, 14, 31)), Sources: hook,
			TaskTitle: p("add customer import with file uploads"), TitleSource: contract.SessionTitleSourceFirstPrompt,
			GitBranch: p("feat/import"),
			Repo:      &contract.SessionRepo{PathHash: "0123456789abcdef0123456789abcdef", DisplayName: p("acme/web")},
		},
		{
			ID: BobSession, DeveloperID: BobDev, Agent: contract.SessionAgentClaudeCode,
			StartedAt: at(Now, 9, 15), EndedAt: p(at(Now, 9, 40)), Sources: hook,
			TaskTitle: p("prototype orders table"), TitleSource: contract.SessionTitleSourceFirstPrompt,
			GitBranch: p("main"), Repo: &contract.SessionRepo{PathHash: "fedcba9876543210fedcba9876543210", DisplayName: p("acme/api")},
		},
		{
			ID: EveSession, DeveloperID: AliceDev, Agent: contract.SessionAgentClaudeCode,
			StartedAt: at(Now, 11, 0), Sources: []contract.SessionSourcesElem{contract.SessionSourcesElemTranscript},
			TaskTitle: p("look around the projects"), TitleSource: contract.SessionTitleSourceAiTitle,
		},
	}
}

func ident(kind, value string, container *string, role contract.IdentifierRole, sig string) contract.Identifier {
	return contract.Identifier{Kind: kind, Provider: kind[:indexDot(kind)], Value: value, Container: container, Role: role, SignatureID: sig}
}

func indexDot(s string) int {
	for i := range s {
		if s[i] == '.' {
			return i
		}
	}
	return len(s)
}

func action(id, session string, t time.Time, tool, input, result string, ids ...contract.Identifier) contract.AgentAction {
	return contract.AgentAction{
		ID: id, SessionID: session, OccurredAt: t, ToolName: tool, Status: contract.AgentActionStatusOk,
		Source: contract.AgentActionSourceHook, InputExcerpt: p(input), ResultExcerpt: p(result), Identifiers: ids,
	}
}

// Actions returns the fixture actions with identifiers already extracted.
func Actions() []contract.AgentAction {
	c, r := contract.IdentifierRoleCreated, contract.IdentifierRoleReferenced
	return []contract.AgentAction{
		action("toolu_01FAKEaaaaaaaaaaaaaaaaa1", AliceSession, at(Day, 14, 6), "mcp__supabase__apply_migration",
			`{"project_id":"`+DevRef+`","name":"create_customers","query":"create table public.customers (id bigint primary key, email text);"}`,
			`{"success": true}`,
			ident("supabase.table", "public.customers", p(DevRef), c, "supabase.mcp.apply_migration")),
		action("toolu_01FAKEaaaaaaaaaaaaaaaaa2", AliceSession, at(Day, 14, 9), "mcp__supabase__execute_sql",
			`{"project_id":"`+DevRef+`","query":"insert into storage.buckets (id, name, public) values ('customer-files','customer-files', true);"}`,
			`{"result":"[]"}`,
			ident("supabase.bucket", "customer-files", p(DevRef), c, "supabase.sql.insert_bucket")),
		action("toolu_01FAKEaaaaaaaaaaaaaaaaa3", AliceSession, at(Day, 14, 14), "Bash",
			"supabase functions deploy process-import --project-ref "+DevRef,
			"Deployed Functions on project "+DevRef+": process-import",
			ident("supabase.function", "process-import", p(DevRef), c, "supabase.cli.functions_deploy")),
		action("toolu_01FAKEaaaaaaaaaaaaaaaaa4", AliceSession, at(Day, 14, 18), "mcp__supabase__create_branch",
			`{"project_id":"`+DevRef+`","name":"import-test"}`,
			`{"id":"00000000-0000-4000-8000-00000000b001","name":"import-test","project_ref":"`+BranchRef+`","parent_project_ref":"`+DevRef+`"}`,
			ident("supabase.branch", BranchRef, nil, c, "supabase.mcp.create_branch")),
		action("toolu_01FAKEaaaaaaaaaaaaaaaaa5", AliceSession, at(Day, 14, 27), "Bash",
			"vercel deploy",
			`{"status":"ok","deployment":{"id":"`+DeployID+`","url":"https://preview-a81f-example-team.vercel.app"}}`,
			ident("vercel.deployment", DeployID, nil, c, "vercel.cli.deploy_json")),
		action("toolu_01FAKEbbbbbbbbbbbbbbbbb1", BobSession, at(Now, 9, 20), "Bash",
			"supabase db push",
			"Applying migration 20261026091900_create_orders.sql...\nFinished supabase db push.",
			ident("supabase.table", "public.orders", p(DevRef), c, "supabase.cli.db_push")),
		action("toolu_01FAKEeeeeeeeeeeeeeeeee1", EveSession, at(Now, 11, 5), "mcp__supabase__list_projects",
			`{}`, `{"projects":[{"ref":"`+ProdRef+`"}]}`,
			ident("supabase.project", ProdRef, nil, r, "supabase.mcp.list_projects")),
	}
}

func res(kind, pid, name, account string, parent *string, created time.Time) contract.Resource {
	return contract.Resource{
		ID: kind + ":" + pid, Kind: kind, Provider: kind[:indexDot(kind)], ProviderID: pid, Name: name, AccountID: account,
		ParentID: parent, CreatedAt: p(created), Attributes: contract.ResourceAttributes{},
		Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList,
		FirstSeenAt: at(Day, 14, 40), LastSeenAt: Now.Add(-3 * time.Minute),
	}
}

// Resources returns the stored (listed) resources.
func Resources() []contract.Resource {
	devProj := "supabase.project:" + DevRef
	vproj := "vercel.project:" + ProjectID
	table := res("supabase.table", DevRef+"/public.customers", "customers", OrgID, &devProj, time.Time{})
	table.CreatedAt, table.SeenVia = nil, contract.ResourceSeenViaAdvisor
	bucket := res("supabase.bucket", DevRef+"/customer-files", "customer-files", OrgID, &devProj, at(Day, 14, 9))
	bucket.Attributes["public"] = true
	dep := res("vercel.deployment", DeployID, "preview-a81f", TeamID, &vproj, at(Day, 14, 26))
	dep.Url = p("https://preview-a81f-example-team.vercel.app")
	dep.Attributes["target"] = "preview"
	return []contract.Resource{
		res("supabase.project", DevRef, "acme-dev", OrgID, nil, Day.AddDate(0, -1, 0)),
		res("supabase.project", ProdRef, "acme-prod", OrgID, nil, Day.AddDate(0, -2, 0)),
		table, bucket,
		res("supabase.function", DevRef+"/process-import", "process-import", OrgID, &devProj, at(Day, 14, 14)),
		res("supabase.branch", BranchRef, "import-test", OrgID, &devProj, at(Day, 14, 18)),
		res("vercel.project", ProjectID, "acme-web", TeamID, nil, Day.AddDate(0, -1, 0)),
		dep,
	}
}

func check(provider, account, kind string, scope, rule *string) contract.ProviderCheck {
	t := Now.Add(-3 * time.Minute)
	return contract.ProviderCheck{Provider: provider, AccountID: account, Kind: kind, Scope: scope, RuleID: rule,
		Status: contract.ProviderCheckStatusOk, AttemptedAt: t, LastSuccessAt: &t, Items: p(1)}
}

// Checks returns the latest successful checks, 3 minutes before Now.
func Checks() []contract.ProviderCheck {
	return []contract.ProviderCheck{
		check("supabase", OrgID, "supabase.project", nil, nil),
		check("supabase", OrgID, "supabase.branch", p(DevRef), nil),
		check("supabase", OrgID, "supabase.function", p(DevRef), nil),
		check("supabase", OrgID, "supabase.bucket", p(DevRef), nil),
		check("supabase", OrgID, "supabase.bucket", p(DevRef), p("public_bucket")),
		check("supabase", OrgID, "supabase.table", p(DevRef), p("rls_disabled")),
		check("vercel", TeamID, "vercel.project", nil, nil),
		check("vercel", TeamID, "vercel.deployment", nil, nil),
		check("vercel", TeamID, "vercel.deployment", nil, p("public_preview")),
	}
}

// Findings returns the adapters' exposed findings.
func Findings() []contract.ExposureFinding {
	t := Now.Add(-3 * time.Minute)
	table := "supabase.table:" + DevRef + "/public.customers"
	bucket := "supabase.bucket:" + DevRef + "/customer-files"
	dep := "vercel.deployment:" + DeployID
	return []contract.ExposureFinding{
		{ID: "rls_disabled:" + table, ResourceID: table, RuleID: "rls_disabled", State: contract.ExposureFindingStateExposed,
			Severity: contract.ExposureFindingSeverityCritical, Signal: contract.ExposureFindingSignalOfficialAdvisor,
			Title: "RLS off: publicly readable", CheckedAt: &t,
			Explanation: "Table public.customers is in an API-exposed schema and row level security is off, so anyone with the public key can read it."},
		{ID: "public_bucket:" + bucket, ResourceID: bucket, RuleID: "public_bucket", State: contract.ExposureFindingStateExposed,
			Severity: contract.ExposureFindingSeverityHigh, Signal: contract.ExposureFindingSignalApiField,
			Title: "public", CheckedAt: &t, Explanation: "Anyone with a file URL can download objects from this bucket."},
		{ID: "public_preview:" + dep, ResourceID: dep, RuleID: "public_preview", State: contract.ExposureFindingStateExposed,
			Severity: contract.ExposureFindingSeverityMedium, Signal: contract.ExposureFindingSignalApiField,
			Title: "public preview", CheckedAt: &t, Caveat: p("Protection exceptions are not visible to the API."),
			Explanation: "Deployment protection is off for this project, so the preview URL is reachable by anyone."},
	}
}

// Costs returns provider cost records.
func Costs() []contract.CostRecord {
	monthly := contract.CostRecordIntervalMonthly
	return []contract.CostRecord{
		{ResourceID: "supabase.branch:" + BranchRef, Source: contract.CostRecordSourceEstimated, AmountMicros: p(10_000_000),
			Currency: p("USD"), Interval: &monthly, Basis: "Supabase branch compute price: Micro", AsOf: Now.Add(-3 * time.Minute)},
	}
}

// Reviews returns recorded decisions: alice kept everything but the branch.
func Reviews() []contract.ReviewItem {
	t := Now.Add(-time.Hour)
	var out []contract.ReviewItem
	for _, id := range []string{
		"supabase.table:" + DevRef + "/public.customers", "supabase.bucket:" + DevRef + "/customer-files",
		"supabase.function:" + DevRef + "/process-import", "vercel.deployment:" + DeployID,
	} {
		out = append(out, contract.ReviewItem{ResourceID: id, State: contract.ReviewItemStateKeep, DecidedAt: &t, DecidedBy: p(AliceDev), DueAt: t})
	}
	return out
}
