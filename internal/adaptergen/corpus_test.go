package adaptergen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/adaptercheck"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/openapi"
)

// TestCorpus runs the generator over a directory of real OpenAPI files
// (third-party specs are not committed). Every spec must either generate an
// adapter the engine accepts or fail with a clear reason, never panic or
// produce an invalid file.
//
//	MURETTO_OPENAPI_CORPUS=<dir with manifest.json> [MURETTO_OPENAPI_OUT=<dir>] go test ./internal/adaptergen -run TestCorpus -v
func TestCorpus(t *testing.T) {
	dir := os.Getenv("MURETTO_OPENAPI_CORPUS")
	if dir == "" {
		t.Skip("set MURETTO_OPENAPI_CORPUS to a directory with manifest.json")
	}
	out := os.Getenv("MURETTO_OPENAPI_OUT")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct{ API, File string }
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, m := range manifest {
		t.Run(m.API, func(t *testing.T) {
			path := filepath.Join(dir, m.File)
			spec, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			doc, err := openapi.Parse(spec, openapi.Options{FS: os.DirFS(filepath.Dir(path)), Path: filepath.Base(path)})
			if err != nil {
				t.Logf("parse: %v", err)
				return
			}
			parsed := time.Since(start)
			res, err := Generate(doc, Options{SpecName: filepath.Base(path)})
			if err != nil && strings.Contains(err.Error(), "does not validate") {
				t.Errorf("%v\n%s", err, res.YAML)
				return
			}
			sel := 0
			if res != nil && res.Report != nil {
				for _, k := range res.Report.Kinds {
					if k.Selected {
						sel++
					}
				}
			}
			summary := fmt.Sprintf("parse %v, total %v, %d ops", parsed.Round(time.Millisecond), time.Since(start).Round(time.Millisecond), len(doc.Operations))
			if err != nil {
				t.Logf("%s: %v", summary, err)
				return
			}
			t.Logf("%s: %d kinds (%d candidates), %d skipped, %d signatures, %d rules; accounts %s; auth %s",
				summary, sel, len(res.Report.Kinds), len(res.Report.Skipped), len(res.Report.Signatures), len(res.Report.Rules), res.Report.Accounts, res.Report.Auth)
			// Scan the generated adapter end to end against a fake API built
			// from the same spec, through the real broker.
			sc := scan(t, doc, res, "")
			t.Logf("scan: %d resources, %d findings, %d checks, %d calls", len(sc.res.Resources), len(sc.res.Findings), len(sc.res.Checks), len(sc.calls))
			// Starter fixtures for it pass the strict gate CI applies to
			// shipped adapters.
			fdir := writeFixtures(t, res)
			reg, err := adapters.LoadFile(res.Provider+".yaml", res.YAML)
			if err != nil {
				t.Fatal(err)
			}
			rep, err := adaptercheck.Run(reg, res.Provider, fdir, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range rep.Problems() {
				t.Errorf("gate: %s", p)
			}
			t.Logf("gate: %d scenarios, %d extract cases, ok=%v", len(rep.Scenarios), rep.Cases, rep.OK())
			if out != "" {
				_ = os.MkdirAll(out, 0o755)
				_ = os.WriteFile(filepath.Join(out, m.API+".yaml"), res.YAML, 0o644)
				rep, _ := json.MarshalIndent(res.Report, "", "  ")
				_ = os.WriteFile(filepath.Join(out, m.API+".report.json"), rep, 0o644)
			}
		})
	}
}
