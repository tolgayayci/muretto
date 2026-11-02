package adaptergen

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/openapi"
)

// TestLiveCompare scans a TEST account twice, read-only: with an adapter
// generated from the provider's own OpenAPI file and with the hand-written
// built-in adapter, and compares what each finds. It prints counts only.
//
//	MURETTO_LIVE_COMPARE=1 MURETTO_OPENAPI_CORPUS=<dir> MURETTO_VERCEL_TOKEN=... MURETTO_SUPABASE_TOKEN=... \
//	  go test ./internal/adaptergen -run TestLiveCompare -v
func TestLiveCompare(t *testing.T) {
	if os.Getenv("MURETTO_LIVE_COMPARE") == "" {
		t.Skip("set MURETTO_LIVE_COMPARE=1 with test-account tokens")
	}
	dir := os.Getenv("MURETTO_OPENAPI_CORPUS")
	cases := []struct {
		provider, spec, env string
		kinds               map[string]string // generated kind -> built-in kind
	}{
		{"vercel", "vercel/openapi.vercel.sh", "MURETTO_VERCEL_TOKEN", map[string]string{
			"project": "project", "deployment": "deployment", "domain": "domain", "env": "env_var", "project_domain": "project_domain"}},
		{"supabase", "supabase/v1-json", "MURETTO_SUPABASE_TOKEN", map[string]string{
			"project": "project", "branch": "branch", "function": "function", "bucket": "bucket"}},
	}
	builtin, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			token := os.Getenv(c.env)
			if token == "" {
				t.Skipf("%s not set", c.env)
			}
			path := filepath.Join(dir, c.spec)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := openapi.Parse(data, openapi.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for k := range c.kinds {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			res, err := Generate(doc, Options{Provider: c.provider + "-oas", Kinds: kinds})
			if err != nil {
				t.Fatal(err)
			}
			gen, err := adapters.LoadFile("gen.yaml", res.YAML)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			gb := broker.New(nil, gen.Allowlist())
			accts, err := gen.Accounts(ctx, gb, res.Provider, token)
			if err != nil || len(accts) == 0 {
				t.Fatalf("generated adapter accounts: %v (%d)", err, len(accts))
			}
			bb := broker.New(nil, builtin.Allowlist())
			bAccts, err := builtin.Accounts(ctx, bb, c.provider, token)
			if err != nil || len(bAccts) == 0 {
				t.Fatalf("built-in accounts: %v", err)
			}
			if accts[0].ID != bAccts[0].ID {
				t.Errorf("account ids differ: generated %d accounts, built-in %d; first ids equal: false", len(accts), len(bAccts))
			}
			a := bAccts[0]
			cred := adapters.Credentials{Token: token, AccountID: a.ID, AccountName: a.Name, AccountSlug: a.Slug}
			gsr, err := gen.Scan(ctx, gb, res.Provider, cred)
			if err != nil {
				t.Fatal(err)
			}
			bsr, err := builtin.Scan(ctx, bb, c.provider, cred)
			if err != nil {
				t.Fatal(err)
			}
			gIDs, bIDs := idsByKind(gsr), idsByKind(bsr)
			for _, gk := range kinds {
				bk := c.kinds[gk]
				g, b := gIDs[gk], bIDs[bk]
				both := 0
				for id := range g {
					if b[id] || b[lastPart(id)] || hasSuffixKey(b, lastPart(id)) {
						both++
					}
				}
				t.Logf("%-15s generated %3d  built-in %-15s %3d  same ids %3d", gk, len(g), bk, len(b), both)
			}
			for _, ch := range gsr.Checks {
				if ch.Status != contract.ProviderCheckStatusOk {
					t.Errorf("generated check %s failed: %s", ch.Kind, deref(ch.Error))
				}
			}
			t.Logf("generated: %d resources, %d findings; built-in: %d resources, %d findings",
				len(gsr.Resources), len(gsr.Findings), len(bsr.Resources), len(bsr.Findings))
		})
	}
}

func idsByKind(sr adapters.ScanResult) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, r := range sr.Resources {
		k := strings.SplitN(r.Kind, ".", 2)[1]
		if out[k] == nil {
			out[k] = map[string]bool{}
		}
		out[k][r.ProviderID] = true
	}
	return out
}

func lastPart(id string) string { return id[strings.LastIndex(id, "/")+1:] }

func hasSuffixKey(m map[string]bool, s string) bool {
	for k := range m {
		if strings.HasSuffix(k, "/"+s) {
			return true
		}
	}
	return false
}
