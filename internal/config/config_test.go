package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home", "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Developer.InstallID == "" || len(c.MachineIDHash()) != 32 {
		t.Fatalf("identity: %+v", c.Developer)
	}
	c.AddAccount(Account{Provider: "vercel", ID: "team_FAKE"})
	c.AddAccount(Account{Provider: "vercel", ID: "team_FAKE", Name: "renamed"})
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	d, _ := Load(path)
	if d.DeveloperID() != c.DeveloperID() || len(d.Accounts) != 1 || d.Accounts[0].Name != "renamed" {
		t.Fatalf("reload: %+v", d)
	}
	if !d.RemoveAccount("vercel", "team_FAKE") || d.RemoveAccount("vercel", "team_FAKE") {
		t.Fatal("remove")
	}
	if d.ReviewAfter().Hours() != 168 {
		t.Fatal("default review window")
	}
}

func TestPathHashSalt(t *testing.T) {
	a := &Config{Developer: Developer{InstallID: "install-fake-a"}}
	b := &Config{Developer: Developer{InstallID: "install-fake-b"}}
	sa := a.PathHashSalt()
	if sa != a.PathHashSalt() || len(sa) != 64 {
		t.Fatalf("salt not a stable 32-byte hex: %q", sa)
	}
	if sa == b.PathHashSalt() {
		t.Error("salt does not depend on the install id")
	}
	// Neither the synced ids nor the install id itself may be the salt.
	if strings.Contains(sa, a.Developer.InstallID) || strings.Contains(sa, a.MachineIDHash()) || sa == a.DeveloperID() {
		t.Error("salt derivable from synced or raw ids")
	}
}
