package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMurettoHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mh")
	t.Setenv("MURETTO_HOME", home)
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Home != home || p.Spool != filepath.Join(home, "spool") || p.HookLog != filepath.Join(home, "hook-errors.log") ||
		p.Manifest != filepath.Join(home, "install-manifest.json") || p.DB != filepath.Join(home, "muretto.db") ||
		p.Config != filepath.Join(home, "config.json") || p.Backups != filepath.Join(home, "backups") {
		t.Fatalf("unexpected layout: %+v", p)
	}
	for _, d := range []string{p.Home, p.Spool, p.Backups} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s has mode %v, want 0700", d, fi.Mode().Perm())
		}
	}
}

func TestResolveTightensHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MURETTO_HOME", home)
	if _, err := Resolve(); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(home)
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("home mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestResolveDefault(t *testing.T) {
	uh := t.TempDir()
	t.Setenv("HOME", uh)
	t.Setenv("MURETTO_HOME", "")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Home != filepath.Join(uh, ".muretto") {
		t.Errorf("home = %s", p.Home)
	}
}

func TestResolveTilde(t *testing.T) {
	uh := t.TempDir()
	t.Setenv("HOME", uh)
	t.Setenv("MURETTO_HOME", "~/alt")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Home != filepath.Join(uh, "alt") {
		t.Errorf("home = %s", p.Home)
	}
}

func TestClaudeConfigDir(t *testing.T) {
	uh := t.TempDir()
	t.Setenv("HOME", uh)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	d, err := ClaudeConfigDir()
	if err != nil || d != filepath.Join(uh, ".claude") {
		t.Errorf("default = %q, %v", d, err)
	}
	alt := filepath.Join(uh, "cc")
	t.Setenv("CLAUDE_CONFIG_DIR", alt)
	d, err = ClaudeConfigDir()
	if err != nil || d != alt {
		t.Errorf("override = %q, %v", d, err)
	}
}
