package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const acmeSpec = "../../internal/adaptergen/testdata/acme-3.1.yaml"

// The whole flow a user follows: draft from OpenAPI, check, install, list,
// remove.
func TestAdapterCommands(t *testing.T) {
	h := newHarness(t)
	h.p.Adapters = filepath.Join(h.p.Home, "adapters")
	dir := t.TempDir()
	out := filepath.Join(dir, "acme.yaml")

	stdout, stderr, code := h.run("adapter", "new", acmeSpec, "-o", out)
	if code != 0 {
		t.Fatalf("new: exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"3 kinds:", "database", "per project", "public_database", "muretto adapter install"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("new output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "[REDACTED]") {
		t.Errorf("report output was redacted:\n%s", stdout)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := h.run("adapter", "new", acmeSpec, "-o", out); code == 0 || !strings.Contains(stderr, "--force") {
		t.Errorf("overwrite without --force: exit %d %s", code, stderr)
	}
	if stdout, _, code := h.run("adapter", "new", acmeSpec, "--list"); code != 0 || !strings.Contains(stdout, "no: looks like events") {
		t.Errorf("--list: exit %d\n%s", code, stdout)
	}
	if _, stderr, code := h.run("adapter", "new", "https://api.acme.dev/openapi.json"); code != 2 || !strings.Contains(stderr, "local file") {
		t.Errorf("URL spec: exit %d %s", code, stderr)
	}

	stdout, stderr, code = h.run("adapter", "check", out)
	if code != 0 || !strings.Contains(stdout, "valid") || !strings.Contains(stdout, "GET  https://api.acme.dev/v1/projects?cursor,limit,org_id") {
		t.Errorf("check: exit %d\n%s%s", code, stdout, stderr)
	}

	if stdout, stderr, code := h.run("adapter", "install", out); code != 0 || !strings.Contains(stdout, "Installed Acme Cloud") {
		t.Fatalf("install: exit %d\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(h.p.Adapters, "acme-cloud.yaml")); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := h.run("adapter", "list"); code != 0 || !strings.Contains(stdout, "acme-cloud") || !strings.Contains(stdout, "supabase") {
		t.Errorf("list: exit %d\n%s", code, stdout)
	}

	// A built-in provider name is refused.
	builtin := filepath.Join(dir, "supabase.yaml")
	data, _ := os.ReadFile(out)
	if err := os.WriteFile(builtin, bytes.Replace(data, []byte("provider: acme-cloud"), []byte("provider: supabase"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := h.run("adapter", "install", builtin); code == 0 || !strings.Contains(stderr, "built-in") {
		t.Errorf("install over a built-in: exit %d %s", code, stderr)
	}

	if stdout, _, code := h.run("adapter", "remove", "acme-cloud"); code != 0 || !strings.Contains(stdout, "Removed") {
		t.Errorf("remove: exit %d %s", code, stdout)
	}
	if _, _, code := h.run("adapter", "remove", "acme-cloud"); code == 0 {
		t.Error("second remove succeeded")
	}
	if _, _, code := h.run("adapter", "remove", "vercel"); code == 0 {
		t.Error("removed a built-in")
	}
}

// Installed adapters load with the built-in ones; a broken one is reported
// and skipped, never fatal.
func TestLoadInstalledAdapters(t *testing.T) {
	h := newHarness(t)
	h.p.Adapters = filepath.Join(h.p.Home, "adapters")
	if err := os.MkdirAll(h.p.Adapters, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "acme.yaml")
	if _, stderr, code := h.run("adapter", "new", acmeSpec, "-o", out, "--install"); code != 0 {
		t.Fatalf("new --install: %s", stderr)
	}
	if err := os.WriteFile(filepath.Join(h.p.Adapters, "broken.yaml"), []byte("format: 1\nprovider: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	a := h.app(&bytes.Buffer{}, &errb)
	reg, err := a.loadAllAdapters()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range reg.Providers() {
		names[p.Name] = true
	}
	if !names["acme-cloud"] || !names["supabase"] || !names["vercel"] {
		t.Errorf("providers = %v", names)
	}
	if !strings.Contains(errb.String(), "ignoring installed adapter") || !strings.Contains(errb.String(), "broken.yaml") {
		t.Errorf("no warning for the broken adapter: %q", errb.String())
	}
	if a.extractVersion == "test" {
		t.Error("extract version not recomputed from installed adapters")
	}
}

// The pull-request flow: starter fixtures pass the strict gate, a YAML
// without fixtures fails it, and a dry run against the spec passes.
func TestAdapterTestCommand(t *testing.T) {
	h := newHarness(t)
	h.p.Adapters = filepath.Join(h.p.Home, "adapters")
	dir := t.TempDir()
	yml := filepath.Join(dir, "acme-cloud.yaml")
	fx := filepath.Join(dir, "testdata", "acme-cloud")
	if stdout, stderr, code := h.run("adapter", "new", acmeSpec, "-o", yml, "--fixtures", fx); code != 0 || !strings.Contains(stdout, "fixture files") {
		t.Fatalf("new --fixtures: exit %d\n%s%s", code, stdout, stderr)
	}
	stdout, stderr, code := h.run("adapter", "test", yml, "--strict")
	if code != 0 || !strings.Contains(stdout, "PASS") || !strings.Contains(stdout, "scan/failures") {
		t.Fatalf("test --strict on generated fixtures: exit %d\n%s%s", code, stdout, stderr)
	}
	if stdout, _, code := h.run("adapter", "test", yml, "--openapi", acmeSpec); code != 0 || !strings.Contains(stdout, "dry run (spec)    ok") {
		t.Errorf("dry run: exit %d\n%s", code, stdout)
	}

	only := filepath.Join(t.TempDir(), "acme-cloud.yaml")
	data, _ := os.ReadFile(yml)
	if err := os.WriteFile(only, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = h.run("adapter", "test", only, "--strict")
	if code != 1 || !strings.Contains(stdout, "no scan scenarios in") || !strings.Contains(stdout, filepath.Join("testdata", "acme-cloud")) {
		t.Errorf("YAML-only strict: exit %d\n%s", code, stdout)
	}
	if _, stderr, code := h.run("adapter", "test", only); code != 2 || !strings.Contains(stderr, "nothing to test") {
		t.Errorf("no fixtures, not strict: exit %d %s", code, stderr)
	}

	// A broken expectation fails.
	exp := filepath.Join(fx, "scan", "basic", "expect.yaml")
	e, _ := os.ReadFile(exp)
	if err := os.WriteFile(exp, []byte(strings.Replace(string(e), "resources:\n", "resources:\n  - acme-cloud.project:not-there\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := h.run("adapter", "test", yml, "--strict"); code != 1 || !strings.Contains(stdout, "missing acme-cloud.project:not-there") {
		t.Errorf("broken expectation: exit %d\n%s", code, stdout)
	}
}
