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

type refusingBackend struct{ MemBackend }

func (refusingBackend) Set(string, string, string) error { return errors.New("exit status 36") }

func TestCheck(t *testing.T) {
	mem := &MemBackend{}
	s := &Store{Backend: mem, Getenv: func(string) string { return "" }}
	if err := s.Check(); err != nil || len(mem.M) != 0 {
		t.Fatalf("check must pass and leave nothing behind: %v %v", err, mem.M)
	}
	s.Backend = &refusingBackend{}
	if err := s.Check(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a keychain that won't store is unavailable: %v", err)
	}
	if err := s.Set("k", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("set: %v", err)
	}
}
