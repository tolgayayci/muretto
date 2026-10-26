package secrets

import (
	"errors"
	"testing"
)

func TestEnvOverridesKeychain(t *testing.T) {
	mem := &MemBackend{}
	env := map[string]string{}
	s := &Store{Backend: mem, Getenv: func(k string) string { return env[k] }}

	if _, src, err := s.Provider("supabase", "org1"); !errors.Is(err, ErrNotFound) || src != SourceNone {
		t.Fatalf("empty store: %v %v", src, err)
	}
	if err := s.Set(ProviderKey("supabase", "org1"), "kc-value"); err != nil {
		t.Fatal(err)
	}
	if v, src, _ := s.Provider("supabase", "org1"); v != "kc-value" || src != SourceKeychain {
		t.Fatalf("keychain: %q %v", v, src)
	}
	env["MURETTO_SUPABASE_TOKEN"] = "env-value"
	if v, src, _ := s.Provider("supabase", "org1"); v != "env-value" || src != SourceEnv {
		t.Fatalf("env: %q %v", v, src)
	}
	if err := s.Delete(ProviderKey("supabase", "org1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ProviderKey("supabase", "org1")); err != nil {
		t.Fatalf("deleting a missing token: %v", err)
	}
	if ProviderEnv("vercel") != "MURETTO_VERCEL_TOKEN" || SyncKey("api.example.test") != "sync/api.example.test" {
		t.Fatal("key names")
	}
}
