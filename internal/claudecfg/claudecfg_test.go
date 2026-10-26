package claudecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/paths"
)

const bin = "/usr/local/bin/muretto"

type env struct {
	p        paths.Paths
	settings string
}

func setup(t *testing.T, initial string) env {
	t.Helper()
	root := t.TempDir()
	cc := filepath.Join(root, "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", cc)
	managedDirs = []string{filepath.Join(root, "managed")}
	p := paths.At(filepath.Join(root, "muretto"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	s := filepath.Join(cc, "settings.json")
	if initial != "" {
		if err := os.MkdirAll(cc, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return env{p, s}
}

func (e env) read(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.settings)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("settings not valid JSON:\n%s", b)
	}
	return string(b)
}

// A messy, real-world-shaped settings file: unknown keys in a non-alphabetical
// order, existing hooks (including a PostToolUse group and an empty group),
// unicode escapes, big numbers, nested arrays, 4-space indent.
const messy = `{
    "$schema": "https://json.schemastore.org/claude-code-settings.json",
    "zeta": 1,
    "model": "claude-opus-5-5",
    "env": {"FOO": "b\u00e4r", "PATH_EXTRA": "/opt/x"},
    "permissions": {
        "allow": ["Bash(npm run lint)", "Bash(npm run test *)"],
        "deny": ["Read(./.env)"],
        "defaultMode": "acceptEdits"
    },
    "hooks": {
        "PostToolUse": [
            {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "prettier --write \"$FILE\"", "timeout": 30}]}
        ],
        "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
        "Notification": [{"matcher": "", "hooks": []}]
    },
    "bigNumber": 12345678901234567890,
    "float": 1.50,
    "nested": [[1, [2, {"a": null}]], true, false],
    "alpha": "last"
}
`

func TestInstallPreservesUserFile(t *testing.T) {
	e := setup(t, messy)
	st, err := Install(e.p, bin)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || len(st.Events) != 6 || st.SettingsPath != e.settings || st.HooksDisabled || st.ManagedOnly {
		t.Errorf("status: %+v", st)
	}
	got := e.read(t)
	// Key order kept, exact scalars kept, indent kept.
	order := []string{`"$schema"`, `"zeta"`, `"model"`, `"env"`, `"permissions"`, `"hooks"`, `"bigNumber"`, `"float"`, `"nested"`, `"alpha"`}
	last := -1
	for _, k := range order {
		i := strings.Index(got, k)
		if i < last {
			t.Errorf("key %s moved:\n%s", k, got)
		}
		last = i
	}
	for _, s := range []string{`"b\u00e4r"`, `12345678901234567890`, `1.50`, `"prettier --write \"$FILE\""`, "\n    \"zeta\": 1", `"Notification"`} {
		if !strings.Contains(got, s) {
			t.Errorf("lost %s:\n%s", s, got)
		}
	}
	// The user's PostToolUse group stays first; ours is appended.
	var parsed struct {
		Hooks map[string][]struct {
			Matcher *string `json:"matcher"`
			Hooks   []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Timeout int      `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatal(err)
	}
	ptu := parsed.Hooks["PostToolUse"]
	if len(ptu) != 2 || ptu[0].Hooks[0].Command != `prettier --write "$FILE"` || *ptu[1].Matcher != "*" ||
		ptu[1].Hooks[0].Command != bin || !slices.Equal(ptu[1].Hooks[0].Args, []string{"hook", "PostToolUse"}) || ptu[1].Hooks[0].Timeout != 5 {
		t.Errorf("PostToolUse: %+v", ptu)
	}
	if ss := parsed.Hooks["SessionStart"]; len(ss) != 1 || ss[0].Matcher != nil || ss[0].Hooks[0].Type != "command" {
		t.Errorf("SessionStart: %+v", ss)
	}
	if f := parsed.Hooks["PostToolUseFailure"]; len(f) != 1 || *f[0].Matcher != "*" {
		t.Errorf("PostToolUseFailure: %+v", f)
	}
	// Mode kept.
	if fi, _ := os.Stat(e.settings); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
	// Backup holds the exact original.
	backups, _ := filepath.Glob(filepath.Join(e.p.Backups, "settings.json.*.bak"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != messy {
		t.Error("backup differs from original")
	}
	// Manifest lists exactly six handlers.
	var m Manifest
	b, _ := os.ReadFile(e.p.Manifest)
	if err := json.Unmarshal(b, &m); err != nil || len(m.Handlers) != 6 || m.SettingsPath != e.settings || m.Backup != backups[0] {
		t.Errorf("manifest: %+v %v", m, err)
	}
	if fi, _ := os.Stat(e.p.Manifest); fi.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode %v", fi.Mode().Perm())
	}
}

func TestInstallIdempotent(t *testing.T) {
	e := setup(t, messy)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	first := e.read(t)
	for i := 0; i < 3; i++ {
		if _, err := Install(e.p, bin); err != nil {
			t.Fatal(err)
		}
	}
	if e.read(t) != first {
		t.Error("re-install changed the file")
	}
	backups, _ := filepath.Glob(filepath.Join(e.p.Backups, "*.bak"))
	if len(backups) != 1 {
		t.Errorf("re-install made extra backups: %d", len(backups))
	}
	if n := strings.Count(first, `"`+bin+`"`); n != 6 {
		t.Errorf("handler count %d", n)
	}
}

func TestUninstallRestoresOriginal(t *testing.T) {
	e := setup(t, messy)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	st, err := Uninstall(e.p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed || len(st.Events) != 0 {
		t.Errorf("status after uninstall: %+v", st)
	}
	// Same data and order as the original (whitespace normalised by Claude Code's layout).
	want, _ := parseJSON([]byte(messy))
	got, _ := parseJSON([]byte(e.read(t)))
	if string(want.encode("    ")) != string(got.encode("    ")) {
		t.Errorf("uninstall did not restore the original:\nwant %s\ngot  %s", want.encode("    "), got.encode("    "))
	}
	if _, err := os.Stat(e.p.Manifest); !os.IsNotExist(err) {
		t.Error("manifest not removed")
	}
	// A second uninstall is a no-op.
	if _, err := Uninstall(e.p); err != nil {
		t.Error(err)
	}
}

func TestInstallNoFile(t *testing.T) {
	e := setup(t, "")
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	got := e.read(t)
	if !strings.HasPrefix(got, "{\n  \"hooks\": {\n    \"SessionStart\": [") {
		t.Errorf("unexpected layout:\n%s", got)
	}
	if fi, _ := os.Stat(e.settings); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode %v", fi.Mode().Perm())
	}
	if b, _ := filepath.Glob(filepath.Join(e.p.Backups, "*")); len(b) != 0 {
		t.Error("backup of a non-existent file")
	}
	if _, err := Uninstall(e.p); err != nil {
		t.Fatal(err)
	}
	if got := e.read(t); strings.TrimSpace(got) != "{}" {
		t.Errorf("after uninstall: %q", got)
	}
}

func TestUninstallKeepsUserEdits(t *testing.T) {
	e := setup(t, `{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"echo bye"}]}]}}`)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	// The user adds their own handler into Muretto's PostToolUse group, tweaks
	// our timeout, and adds a new key, after install.
	var doc map[string]any
	_ = json.Unmarshal([]byte(e.read(t)), &doc)
	hooks := doc["hooks"].(map[string]any)
	ptu := hooks["PostToolUse"].([]any)[0].(map[string]any)
	ptu["hooks"] = append(ptu["hooks"].([]any), map[string]any{"type": "command", "command": "user-logger"})
	ss := hooks["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	ss["timeout"] = 9
	doc["theme"] = "dark"
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(e.settings, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(e.p); err != nil {
		t.Fatal(err)
	}
	got := e.read(t)
	if strings.Contains(got, bin) {
		t.Errorf("muretto handler left:\n%s", got)
	}
	for _, s := range []string{"user-logger", "echo bye", `"theme"`, `"PostToolUse"`} {
		if !strings.Contains(got, s) {
			t.Errorf("lost %s:\n%s", s, got)
		}
	}
	if strings.Contains(got, `"SessionStart"`) || strings.Contains(got, `"UserPromptSubmit"`) {
		t.Errorf("empty event keys left:\n%s", got)
	}
}

func TestUninstallOnlyManifestHandlers(t *testing.T) {
	// A handler identical to ours that the user had before install (not in
	// the manifest) is not ours to remove.
	pre := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + bin + `","args":["hook","Stop"]}]}]}}`
	e := setup(t, pre)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(e.p); err != nil {
		t.Fatal(err)
	}
	if got := e.read(t); !strings.Contains(got, `"Stop"`) || !strings.Contains(got, bin) {
		t.Errorf("removed a handler Muretto didn't add:\n%s", got)
	}
}

func TestInstallKeepsPreexistingIdenticalHandler(t *testing.T) {
	pre := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"` + bin + `","args":["hook","SessionStart"],"timeout":5}]}]}}`
	e := setup(t, pre)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.read(t), `"SessionStart"`); n != 2 { // key + args value, not duplicated
		t.Errorf("SessionStart duplicated (%d)", n)
	}
	b, _ := os.ReadFile(e.p.Manifest)
	var m Manifest
	_ = json.Unmarshal(b, &m)
	if len(m.Handlers) != 5 {
		t.Errorf("manifest should not claim the pre-existing handler: %d", len(m.Handlers))
	}
}

func TestReinstallWithNewBinaryReplacesOld(t *testing.T) {
	e := setup(t, `{}`)
	if _, err := Install(e.p, "/old/muretto"); err != nil {
		t.Fatal(err)
	}
	st, err := Install(e.p, bin)
	if err != nil {
		t.Fatal(err)
	}
	got := e.read(t)
	if strings.Contains(got, "/old/muretto") || !st.Installed {
		t.Errorf("old handlers kept:\n%s", got)
	}
	if _, err := Uninstall(e.p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.read(t), "muretto") {
		t.Error("uninstall left handlers")
	}
}

// sameJSON reports whether two settings files hold the same data in the same
// order (whitespace aside).
func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	x, err := parseJSON([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	y, err := parseJSON([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	return string(x.encode("  ")) == string(y.encode("  "))
}

func readManifestFile(t *testing.T, e env) Manifest {
	t.Helper()
	b, err := os.ReadFile(e.p.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Uninstall removes only containers Install created: a user's own empty
// "hooks" object, event array or matcher group survives install + uninstall.
func TestUninstallKeepsPreexistingContainers(t *testing.T) {
	for name, pre := range map[string]string{
		"empty hooks":       `{"hooks": {}}`,
		"empty event":       `{"hooks": {"PostToolUse": []}}`,
		"two empty events":  `{"hooks": {"SessionEnd": [], "PostToolUse": []}, "x": 1}`,
		"empty group":       `{"hooks": {"PostToolUse": [{"matcher": "Bash", "hooks": []}]}}`,
		"user group":        `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]}}`,
		"empty event other": `{"hooks": {"Stop": []}}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, pre)
			if _, err := Install(e.p, bin); err != nil {
				t.Fatal(err)
			}
			if _, err := Uninstall(e.p); err != nil {
				t.Fatal(err)
			}
			if got := e.read(t); !sameJSON(t, pre, got) {
				t.Errorf("not restored:\nwant %s\ngot  %s", pre, got)
			}
		})
	}
}

func TestManifestRecordsCreatedContainers(t *testing.T) {
	e := setup(t, `{"hooks": {"PostToolUse": []}}`)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	m := readManifestFile(t, e)
	if m.Created == nil || m.Created.Hooks || slices.Contains(m.Created.Events, "PostToolUse") || len(m.Created.Events) != len(Events)-1 {
		t.Errorf("created: %+v", m.Created)
	}
	// Re-installing (here with a new binary path) keeps the record: the
	// arrays it created are still Muretto's, the user's still the user's.
	if _, err := Install(e.p, "/new/muretto"); err != nil {
		t.Fatal(err)
	}
	if m2 := readManifestFile(t, e); m2.Created == nil || m2.Created.Hooks || len(m2.Created.Events) != len(Events)-1 ||
		slices.Contains(m2.Created.Events, "PostToolUse") {
		t.Errorf("created after re-install: %+v", m2.Created)
	}
	if _, err := Uninstall(e.p); err != nil {
		t.Fatal(err)
	}
	if got := e.read(t); !sameJSON(t, `{"hooks": {"PostToolUse": []}}`, got) {
		t.Errorf("after re-install + uninstall: %s", got)
	}
	// From nothing, everything is Muretto's and goes away.
	e = setup(t, `{}`)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	if m := readManifestFile(t, e); m.Created == nil || !m.Created.Hooks || len(m.Created.Events) != len(Events) {
		t.Errorf("created from empty: %+v", m.Created)
	}
}

// A manifest written before Created existed says nothing about which
// containers were there before: Uninstall keeps "hooks" and every event
// array, removing only the handlers (and the groups Install appended for
// them, which never pre-exist).
func TestUninstallLegacyManifestIsConservative(t *testing.T) {
	for name, pre := range map[string]string{
		"from nothing": `{}`,
		"empty event":  `{"hooks": {"PostToolUse": []}}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, pre)
			if _, err := Install(e.p, bin); err != nil {
				t.Fatal(err)
			}
			// Rewrite the manifest as an old version would have: no "created".
			b, _ := os.ReadFile(e.p.Manifest)
			var raw map[string]any
			_ = json.Unmarshal(b, &raw)
			if _, ok := raw["created"]; !ok {
				t.Fatal("new manifest has no created field")
			}
			delete(raw, "created")
			b, _ = json.Marshal(raw)
			if err := os.WriteFile(e.p.Manifest, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Uninstall(e.p); err != nil {
				t.Fatal(err)
			}
			got := e.read(t)
			if strings.Contains(got, bin) || strings.Contains(got, `"matcher"`) || strings.Contains(got, `"args"`) {
				t.Errorf("handlers or their groups left:\n%s", got)
			}
			var doc struct{ Hooks map[string][]any }
			_ = json.Unmarshal([]byte(got), &doc)
			if doc.Hooks == nil || len(doc.Hooks) < len(Events) {
				t.Errorf("legacy uninstall removed containers:\n%s", got)
			}
			for _, ev := range Events {
				if arr, ok := doc.Hooks[ev.Name]; !ok || len(arr) != 0 {
					t.Errorf("event %s: %v", ev.Name, arr)
				}
			}
		})
	}
	// Re-installing over a legacy manifest doesn't claim existing containers.
	e := setup(t, `{}`)
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(e.p.Manifest)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	delete(raw, "created")
	b, _ = json.Marshal(raw)
	if err := os.WriteFile(e.p.Manifest, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(e.p, "/new/muretto"); err != nil {
		t.Fatal(err)
	}
	if m := readManifestFile(t, e); m.Created == nil || m.Created.Hooks || len(m.Created.Events) != 0 {
		t.Errorf("legacy re-install claimed containers: %+v", m.Created)
	}
}

func TestRefusesBadFiles(t *testing.T) {
	for name, content := range map[string]string{
		"comment":         "{\n  // comment\n  \"a\": 1\n}",
		"trailing comma":  `{"a": 1,}`,
		"array root":      `[1,2]`,
		"hooks not obj":   `{"hooks": []}`,
		"event not array": `{"hooks": {"PostToolUse": {"matcher": "*"}}}`,
		"truncated":       `{"a": `,
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, content)
			if _, err := Install(e.p, bin); err == nil {
				t.Error("install accepted a bad file")
			}
			b, _ := os.ReadFile(e.settings)
			if string(b) != content {
				t.Error("bad file was modified")
			}
			if _, err := os.Stat(e.p.Manifest); err == nil {
				t.Error("manifest written")
			}
		})
	}
	e := setup(t, `{}`)
	if _, err := Install(e.p, "relative/muretto"); err == nil {
		t.Error("relative binary path accepted")
	}
}

func TestEmptyFileAndTabs(t *testing.T) {
	e := setup(t, "\n")
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	e = setup(t, "{\n\t\"a\": [\n\t\t1\n\t]\n}")
	if _, err := Install(e.p, bin); err != nil {
		t.Fatal(err)
	}
	if got := e.read(t); !strings.HasPrefix(got, "{\n\t\"a\": [\n\t\t1\n\t],\n\t\"hooks\"") || strings.HasSuffix(got, "\n") {
		t.Errorf("tab layout or missing-newline not kept:\n%q", got)
	}
}

func TestInspectDetectsDisabled(t *testing.T) {
	e := setup(t, `{"disableAllHooks": true}`)
	st, err := Inspect(e.p, bin)
	if err != nil {
		t.Fatal(err)
	}
	if !st.HooksDisabled || st.ManagedOnly || st.Installed {
		t.Errorf("%+v", st)
	}
	// Managed settings: the file and a drop-in.
	md := managedDirs[0]
	if err := os.MkdirAll(filepath.Join(md, "managed-settings.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(md, "managed-settings.d", "20-hooks.json"), []byte(`{"allowManagedHooksOnly": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(md, "managed-settings.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ = Inspect(e.p, bin)
	if !st.ManagedOnly {
		t.Errorf("managed drop-in not detected: %+v", st)
	}
	e = setup(t, `{"allowManagedHooksOnly": false, "disableAllHooks": "true"}`)
	st, _ = Inspect(e.p, bin)
	if st.ManagedOnly || st.HooksDisabled {
		t.Errorf("false/quoted values detected as set: %+v", st)
	}
}

func TestInspectMissingFile(t *testing.T) {
	e := setup(t, "")
	st, err := Inspect(e.p, bin)
	if err != nil || st.Installed || st.SettingsPath != e.settings {
		t.Errorf("%+v %v", st, err)
	}
}

// The plugin dir ships the same hooks as Install (with muretto from PATH).
func TestPluginMatchesInstall(t *testing.T) {
	b, err := os.ReadFile("../../plugin/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher *string `json:"matcher"`
			Hooks   []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Timeout int      `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Hooks) != len(Events) {
		t.Errorf("plugin has %d events, install has %d", len(doc.Hooks), len(Events))
	}
	for _, e := range Events {
		g := doc.Hooks[e.Name]
		if len(g) != 1 || len(g[0].Hooks) != 1 {
			t.Errorf("%s: %+v", e.Name, g)
			continue
		}
		m := ""
		if g[0].Matcher != nil {
			m = *g[0].Matcher
		}
		h := g[0].Hooks[0]
		if m != e.Matcher || h.Type != "command" || h.Command != "muretto" || !slices.Equal(h.Args, []string{"hook", e.Name}) || h.Timeout != Timeout {
			t.Errorf("%s differs from install: matcher %q %+v", e.Name, m, h)
		}
	}
	var pj struct {
		Name string `json:"name"`
	}
	pb, err := os.ReadFile("../../plugin/.claude-plugin/plugin.json")
	if err != nil || json.Unmarshal(pb, &pj) != nil || pj.Name != "muretto" {
		t.Errorf("plugin.json: %v %+v", err, pj)
	}
}

func TestOrderedJSONRoundTrip(t *testing.T) {
	for _, in := range []string{
		`{}`, `[]`, `{"a":{"b":[]}}`, `{"k":"\u2028\"\\","n":-0.0e+10,"z":[true,null,{"q":"é"}]}`,
		`{"dup":1,"dup":2}`, `"str"`, `123`,
	} {
		n, err := parseJSON([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		out := n.encode("  ")
		var a, b any
		if json.Unmarshal([]byte(in), &a) != nil || json.Unmarshal(out, &b) != nil {
			t.Fatalf("invalid output for %s: %s", in, out)
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Errorf("round trip changed %s -> %s", in, out)
		}
		if n2, _ := parseJSON(out); string(n2.encode("  ")) != string(out) {
			t.Errorf("encode not stable for %s", in)
		}
	}
	if n, _ := parseJSON([]byte(`{"dup":1,"dup":2}`)); string(n.get("dup").raw) != "2" {
		t.Error("get should return the last duplicate")
	}
}
