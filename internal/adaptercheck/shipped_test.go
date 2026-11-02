package adaptercheck

import (
	"path/filepath"
	"strings"
	"testing"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/adapters"
)

const testdata = "../../adapters/testdata"

// TestShippedAdapters is the gate for a pull request that adds or changes an
// adapter: every adapter in adapters/ must pass its scan scenarios and
// extract cases offline and meet the shipping rules (a failure scenario,
// secret markers for stripped fields, an extract case per signature, a case
// that proves nothing).
func TestShippedAdapters(t *testing.T) {
	reg, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Providers() {
		t.Run(p.Name, func(t *testing.T) {
			rep, err := Run(reg, p.Name, filepath.Join(testdata, p.Name), true)
			if err != nil {
				t.Fatal(err)
			}
			for _, prob := range rep.Problems() {
				t.Error(prob)
			}
			if len(rep.Scenarios) == 0 || rep.Cases == 0 {
				t.Errorf("%d scenarios, %d extract cases", len(rep.Scenarios), rep.Cases)
			}
		})
	}
}

// An adapter with no fixtures (a pull request with only a YAML file) fails.
func TestAdapterWithoutFixturesFails(t *testing.T) {
	reg, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(reg, "vercel", t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("an adapter with no fixtures passed")
	}
	joined := strings.Join(rep.Problems(), "\n")
	for _, want := range []string{"no scan scenarios", "no extract cases"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems lack %q:\n%s", want, joined)
		}
	}
}
