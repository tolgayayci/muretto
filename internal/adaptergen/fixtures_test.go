package adaptergen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/adaptercheck"
	"github.com/tolgayayci/muretto/internal/adapters"
)

// writeFixtures writes Fixtures(res) under a temp dir and returns it.
func writeFixtures(t *testing.T, res *Result) string {
	t.Helper()
	files, err := Fixtures(res)
	if err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The contributor flow: generate an adapter and its starter fixtures, and
// the result passes the same strict gate CI applies to shipped adapters.
func TestGeneratedFixturesPassTheGate(t *testing.T) {
	for _, tc := range []struct {
		spec string
		opt  Options
	}{
		{"acme-2.0.yaml", Options{}},
		{"acme-3.0.json", Options{}},
		{"acme-3.1.yaml", Options{}},
		{"acme-3.1.yaml", Options{MCPServer: "acme"}},
		{"acme-3.2.yaml", Options{}},
	} {
		t.Run(tc.spec+"/mcp="+tc.opt.MCPServer, func(t *testing.T) {
			res := generate(t, tc.spec, tc.opt)
			dir := writeFixtures(t, res)
			reg, err := adapters.LoadFile(res.Provider+".yaml", res.YAML)
			if err != nil {
				t.Fatal(err)
			}
			rep, err := adaptercheck.Run(reg, res.Provider, dir, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range rep.Problems() {
				t.Error(p)
			}
			if len(rep.Scenarios) != 2 || rep.Cases < 4 {
				t.Errorf("%d scenarios, %d cases", len(rep.Scenarios), rep.Cases)
			}
			// The failure scenario really fails one check, and the secret
			// marker is in the bodies and forbidden in the result.
			exp, _ := os.ReadFile(filepath.Join(dir, "scan", "failures", "expect.yaml"))
			if !strings.Contains(string(exp), "|error:permission") || !strings.Contains(string(exp), adaptercheck.FakeSecret) {
				t.Errorf("failures/expect.yaml:\n%s", exp)
			}
		})
	}
}

// Editing a fixture so the adapter no longer matches it must fail the gate:
// the fixtures pin behaviour, they do not just echo it.
func TestGateCatchesRegressions(t *testing.T) {
	res := generate(t, "acme-3.1.yaml", Options{})
	dir := writeFixtures(t, res)
	reg, err := adapters.LoadFile(res.Provider+".yaml", res.YAML)
	if err != nil {
		t.Fatal(err)
	}
	// 1. An edit stores a secret field: the strip is dropped and the field
	// becomes an attribute.
	y := strings.Replace(string(res.YAML), "strip: ['[].connection_string', '[].password']", "strip: ['[].password']", 1)
	y = strings.Replace(y, "          - engine\n", "          - engine\n          - connection_string\n", 1)
	leaky, err := adapters.LoadFile("leaky.yaml", []byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if rep, _ := adaptercheck.Run(leaky, res.Provider, dir, true); rep.OK() || !strings.Contains(strings.Join(rep.Problems(), "\n"), "forbidden") {
		t.Errorf("a dropped strip passed: %v", rep.Problems())
	}
	// 2. A signature is removed from the cases.
	cases := filepath.Join(dir, "extract", "cases.yaml")
	data, _ := os.ReadFile(cases)
	cut := strings.Index(string(data), "- name: curl POST /webhooks")
	if cut < 0 {
		t.Fatalf("no webhook case:\n%s", data)
	}
	end := strings.Index(string(data)[cut+1:], "- name:")
	trimmed := string(data)[:cut] + string(data)[cut+1+end:]
	if err := os.WriteFile(cases, []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}
	if rep, _ := adaptercheck.Run(reg, res.Provider, dir, true); rep.OK() || !strings.Contains(strings.Join(rep.Problems(), "\n"), "create_webhook is not exercised") {
		t.Errorf("a missing signature case passed: %v", rep.Problems())
	}
}
