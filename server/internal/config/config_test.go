package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

const supa = "https://abcdefgh.supabase.co"

func TestDefaults(t *testing.T) {
	if _, err := Load(env(nil)); err == nil || !strings.Contains(err.Error(), "MURETTO_SUPABASE_URL") {
		t.Fatalf("no Supabase URL and no dev login must be refused: %v", err)
	}
	c, err := Load(env(map[string]string{"MURETTO_SUPABASE_URL": supa + "/"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:8080" || c.PublicURL != "http://127.0.0.1:8080" || c.DashboardURL != "http://localhost:3000" || c.DevLogin {
		t.Fatalf("defaults: %+v", c)
	}
	if c.SupabaseURL != supa || c.SupabaseJWTSecret != "" || c.HTTPS() {
		t.Fatalf("supabase defaults: %+v", c)
	}
	// Dev login on loopback may run without Supabase.
	if c, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1"})); err != nil || c.SupabaseURL != "" {
		t.Fatalf("dev login without Supabase: %+v %v", c, err)
	}
}

func TestSupabaseURL(t *testing.T) {
	bad := []string{
		"http://abcdefgh.supabase.co",          // http off loopback
		"https://abcdefgh.supabase.co/auth/v1", // path
		"https://abcdefgh.supabase.co?x=1",
		"https://abcdefgh.supabase.co#f",
		"https://user:pw@abcdefgh.supabase.co",
		"ftp://abcdefgh.supabase.co",
		"abcdefgh.supabase.co",
		"https://",
	}
	for _, u := range bad {
		if _, err := Load(env(map[string]string{"MURETTO_SUPABASE_URL": u})); err == nil {
			t.Errorf("%s: want error", u)
		}
	}
	for _, u := range []string{supa, supa + "/", "http://127.0.0.1:54321", "http://localhost:54321/"} {
		if _, err := Load(env(map[string]string{"MURETTO_SUPABASE_URL": u})); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	secret := "0123456789abcdef0123456789abcdef"
	if c, err := Load(env(map[string]string{"MURETTO_SUPABASE_URL": supa, "MURETTO_SUPABASE_JWT_SECRET": secret})); err != nil || c.SupabaseJWTSecret != secret {
		t.Fatalf("secret: %v", err)
	}
	if _, err := Load(env(map[string]string{"MURETTO_SUPABASE_URL": supa, "MURETTO_SUPABASE_JWT_SECRET": "short"})); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1", "MURETTO_SUPABASE_JWT_SECRET": secret})); err == nil {
		t.Fatal("secret without URL accepted")
	}
}

func TestDevLoginRefusedOffLoopback(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "10.0.0.5:8080"} {
		_, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1", "MURETTO_ADDR": addr, "MURETTO_PUBLIC_URL": "http://127.0.0.1:8080", "MURETTO_SUPABASE_URL": supa}))
		if err == nil || !strings.Contains(err.Error(), "MURETTO_DEV_LOGIN") {
			t.Errorf("%s: dev login must be refused, got %v", addr, err)
		}
	}
	if _, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1", "MURETTO_ADDR": "[::1]:9000"})); err != nil {
		t.Fatalf("ipv6 loopback: %v", err)
	}
	// PORT alone means every interface.
	if _, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1", "PORT": "8080"})); err == nil {
		t.Fatal("dev login with PORT must be refused")
	}
}

func TestValidation(t *testing.T) {
	bad := []map[string]string{
		{"MURETTO_DASHBOARD_URL": "http://app.example.com"}, // http off loopback
		{"MURETTO_PUBLIC_URL": "ftp://x"},
		{"MURETTO_INGEST_PER_MINUTE": "0"},
		{"MURETTO_DEV_LOGIN": "yes"},
	}
	for _, m := range bad {
		m["MURETTO_SUPABASE_URL"] = supa
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%v: want error", m)
		}
	}
	c, err := Load(env(map[string]string{"PORT": "7000", "MURETTO_PUBLIC_URL": "https://api.example.com", "MURETTO_DASHBOARD_URL": "https://app.example.com", "MURETTO_SUPABASE_URL": supa}))
	if err != nil || c.Addr != ":7000" || !c.HTTPS() {
		t.Fatalf("prod config: %+v %v", c, err)
	}
}
