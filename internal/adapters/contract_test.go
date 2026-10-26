package adapters

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/contract"
)

// attribMatch is Stream D's identifier matching contract (docs/decisions/0401
// on D's branch), reimplemented here so this package fails if its identifiers
// stop fitting it:
//   - id_in_result: value == provider_id;
//   - host_in_result: value == attributes.host;
//   - with a container: container+"/"+value == provider_id, or value == name
//     and container is the account id, account name, attributes.account_slug,
//     or the parent's provider id or name.
func attribMatch(id contract.Identifier, res contract.Resource, byID map[string]contract.Resource) bool {
	if id.Kind != res.Kind {
		return false
	}
	if id.Value == res.ProviderID {
		return true
	}
	if h, ok := res.Attributes["host"].(string); ok && h != "" && id.Value == h {
		return true // host_in_result (docs/decisions/0901)
	}
	if id.Container == nil {
		return false
	}
	c := *id.Container
	if c+"/"+id.Value == res.ProviderID {
		return true
	}
	if id.Value != res.Name {
		return false
	}
	cands := []string{res.AccountID}
	if res.AccountName != nil {
		cands = append(cands, *res.AccountName)
	}
	if s, ok := res.Attributes["account_slug"].(string); ok {
		cands = append(cands, s)
	}
	if res.ParentID != nil {
		if p, ok := byID[*res.ParentID]; ok {
			cands = append(cands, p.ProviderID, p.Name)
		}
	}
	for _, x := range cands {
		if x != "" && x == c {
			return true
		}
	}
	return false
}

// Realistic creating actions aimed at the scan fixtures. Each must yield
// identifiers that match exactly the named resource, under both D's contract
// and Registry.Matches.
var contractActions = []struct {
	tool   string
	input  map[string]any
	output string
	want   []string
}{
	{"mcp__supabase__apply_migration",
		map[string]any{"project_id": "abcdefghijklmnopqrst", "query": "create table public.customers (id int); insert into storage.buckets (id, name, public) values ('avatars', 'avatars', true);"},
		`{"success":true}`,
		[]string{"supabase.table:abcdefghijklmnopqrst/public.customers", "supabase.bucket:abcdefghijklmnopqrst/avatars"}},
	{"Bash", map[string]any{"command": "supabase functions deploy stripe-webhook"},
		"Deployed Functions on project abcdefghijklmnopqrst: stripe-webhook\n",
		[]string{"supabase.function:abcdefghijklmnopqrst/stripe-webhook"}},
	{"mcp__claude_ai_Supabase__create_branch", map[string]any{"project_id": "abcdefghijklmnopqrst", "name": "develop"},
		`{"project_ref":"tsrqponmlkjihgfedcba","parent_project_ref":"abcdefghijklmnopqrst"}`,
		[]string{"supabase.branch:tsrqponmlkjihgfedcba"}},
	{"Bash", map[string]any{"command": "supabase projects create waitlist-demo"},
		"Created a new project at https://supabase.com/dashboard/project/abcdefghijklmnopqrst\n",
		[]string{"supabase.project:abcdefghijklmnopqrst"}},
	{"Bash", map[string]any{"command": "vercel deploy"},
		`{"status":"ok","deployment":{"id":"dpl_EXAMPLEPREVIEW000000000001","url":"https://example-waitlist-abc123def-example-team.vercel.app"}}`,
		[]string{"vercel.deployment:dpl_EXAMPLEPREVIEW000000000001"}},
	{"Bash", map[string]any{"command": "vercel deploy --non-interactive=false"}, // human mode: bare URL on stdout
		"https://example-waitlist-abc123def-example-team.vercel.app\n",
		[]string{"vercel.deployment:dpl_EXAMPLEPREVIEW000000000001"}},
	{"Bash", map[string]any{"command": "vercel env add NEXT_PUBLIC_STRIPE_SECRET_KEY production"},
		"✓ Added           NEXT_PUBLIC_STRIPE_SECRET_KEY\n  Project         example-team/example-waitlist\n",
		[]string{"vercel.env_var:prj_EXAMPLE000000000000000001/NEXT_PUBLIC_STRIPE_SECRET_KEY/production"}},
	{"Bash", map[string]any{"command": "vercel project create example-admin"},
		"> Success! Project example-admin added (example-team) [412ms]",
		[]string{"vercel.project:prj_EXAMPLE000000000000000002"}},
	{"Bash", map[string]any{"command": "vercel domains add example-waitlist.dev"},
		"> Success! Domain example-waitlist.dev added to example-team. [1s]",
		[]string{"vercel.domain:example-waitlist.dev"}},
	{"mcp__vercel__create_project_env", map[string]any{"idOrName": "prj_EXAMPLE000000000000000003"},
		`{"created":{"id":"EXAMPLEenvid0009","key":"VITE_API_TOKEN"}}`,
		[]string{"vercel.env_var:prj_EXAMPLE000000000000000003/VITE_API_TOKEN/preview,production"}},
}

func TestIdentifiersFitAttributionContract(t *testing.T) {
	r := loadEmbedded(t)
	var all []contract.Resource
	for _, name := range []string{"supabase/scan/basic", "vercel/scan/basic"} {
		parts := strings.Split(name, "/")
		res, _, _ := runScenario(t, r, scenario{provider: parts[0], name: parts[2], dir: filepath.Join(testdataDir, name)})
		all = append(all, res.Resources...)
	}
	byID := map[string]contract.Resource{}
	for _, res := range all {
		byID[res.ID] = res
	}
	for _, a := range contractActions {
		ids := r.Extract(rawAction(a.tool, "ok", a.input, a.output))
		var gotD, gotM []string
		for _, id := range ids {
			if id.Role != contract.IdentifierRoleCreated {
				continue
			}
			for _, res := range all {
				if attribMatch(id, res, byID) {
					gotD = append(gotD, res.ID)
				}
				if r.Matches(id, res) {
					gotM = append(gotM, res.ID)
				}
			}
		}
		sort.Strings(gotD)
		sort.Strings(gotM)
		want := append([]string(nil), a.want...)
		sort.Strings(want)
		if strings.Join(gotD, ",") != strings.Join(want, ",") {
			t.Errorf("%s %v: D contract matched %v, want %v", a.tool, a.input, gotD, want)
		}
		if strings.Join(gotM, ",") != strings.Join(want, ",") {
			t.Errorf("%s %v: Registry.Matches matched %v, want %v", a.tool, a.input, gotM, want)
		}
	}
}

// Every resource carries attributes.account_slug.
func TestAccountSlugAttribute(t *testing.T) {
	r := loadEmbedded(t)
	for _, sc := range scenarios(t) {
		res, exp, _ := runScenario(t, r, sc)
		want := exp.Account.Slug
		if want == "" {
			want = exp.Account.ID // Supabase: the org slug is the account id
		}
		for _, x := range res.Resources {
			if x.Attributes["account_slug"] != want {
				t.Errorf("%s: account_slug = %v, want %s", x.ID, x.Attributes["account_slug"], want)
			}
		}
	}
}
