package config

import (
	"os"
	"path/filepath"
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
