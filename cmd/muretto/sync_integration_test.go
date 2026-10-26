package main

import (
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/internal/config"
	"github.com/tolgayayci/muretto/internal/secrets"
	"github.com/tolgayayci/muretto/server/testserver"
)

// TestSyncAgainstRealClientAndServer runs the real syncclient against the
// in-process server: the Answer computed from the fixture must be accepted
// (it is schema-validated server-side), and logout must revoke the token.
func TestSyncAgainstRealClientAndServer(t *testing.T) {
	srv := testserver.Start(t, testserver.Options{})
	h := newHarness(t)
	cfg, err := config.Load(h.p.Config)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sync.Server = srv.URL
	cfg.Sync.Enabled = true
	if err := cfg.Save(h.p.Config); err != nil {
		t.Fatal(err)
	}
	if err := h.sec.Set(secrets.SyncKey(hostOf(srv.URL)), srv.Token(t, "alice")); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, string) {
		t.Helper()
		var out, errb strings.Builder
		a := h.app(&out, &errb)
		a.sync = realSync{}
		if code := a.run(t.Context(), args); code != 0 {
			t.Fatalf("muretto %v: exit %d\n%s\n%s", args, code, out.String(), errb.String())
		}
		return out.String(), errb.String()
	}

	_, errOut := run("sessions")
	if errOut != "" {
		t.Fatalf("push failed: %s", errOut)
	}
	out, _ := run("sync", "status")
	if !strings.Contains(out, "Sync: on") || strings.Contains(out, " 0 entities") {
		t.Fatalf("status after push:\n%s", out)
	}
	out, _ = run("sync", "now")
	if !strings.Contains(out, "Synced.") {
		t.Fatal(out)
	}
	out, _ = run("logout")
	if strings.Contains(out, "Could not revoke") || !strings.Contains(out, "Logged out") {
		t.Fatalf("logout:\n%s", out)
	}
	if _, _, err := h.sec.Sync(hostOf(srv.URL)); err == nil {
		t.Fatal("token still stored after logout")
	}
}
