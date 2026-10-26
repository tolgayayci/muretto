package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:8080" || c.PublicURL != "http://127.0.0.1:8080" || c.DashboardURL != "http://localhost:3000" || c.DevLogin {
		t.Fatalf("defaults: %+v", c)
	}
	if c.SecureCookies() || c.GitHubConfigured() {
		t.Fatal("defaults must be dev-safe")
	}
}

func TestDevLoginRefusedOffLoopback(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "10.0.0.5:8080"} {
		_, err := Load(env(map[string]string{"MURETTO_DEV_LOGIN": "1", "MURETTO_ADDR": addr, "MURETTO_PUBLIC_URL": "http://127.0.0.1:8080"}))
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
		{"MURETTO_GITHUB_CLIENT_ID": "only-id"},
		{"MURETTO_INGEST_PER_MINUTE": "0"},
		{"MURETTO_DEV_LOGIN": "yes"},
	}
	for _, m := range bad {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%v: want error", m)
		}
	}
	c, err := Load(env(map[string]string{"PORT": "7000", "MURETTO_PUBLIC_URL": "https://api.example.com", "MURETTO_DASHBOARD_URL": "https://app.example.com"}))
	if err != nil || c.Addr != ":7000" || !c.SecureCookies() {
		t.Fatalf("prod config: %+v %v", c, err)
	}
}
