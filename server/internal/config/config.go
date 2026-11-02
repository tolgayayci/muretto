// Package config reads murettod's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is the server configuration. See server/README.md for each variable.
type Config struct {
	Addr         string // MURETTO_ADDR, or ":$PORT"
	PublicURL    string // MURETTO_PUBLIC_URL: the API's own base URL (https enables HSTS)
	DashboardURL string // MURETTO_DASHBOARD_URL: the only CORS origin; device and invite links

	DatabaseURL       string // MURETTO_DATABASE_URL; empty = in-memory store
	DatabaseListenURL string // MURETTO_DATABASE_LISTEN_URL; defaults to DatabaseURL
	DBMaxConns        int32  // MURETTO_DB_MAX_CONNS

	// SupabaseURL is the Supabase project URL (MURETTO_SUPABASE_URL). Dashboard
	// users sign in with Supabase Auth; the API verifies their access tokens
	// against <SupabaseURL>/auth/v1 (issuer) and its JWKS. Required unless
	// DevLogin is on.
	SupabaseURL string
	// SupabaseJWTSecret (MURETTO_SUPABASE_JWT_SECRET) enables legacy HS256
	// tokens. Empty: only asymmetric (ES256/RS256) tokens are accepted.
	SupabaseJWTSecret string

	DevLogin   bool  // MURETTO_DEV_LOGIN=1 — DEV ONLY, loopback only
	TrustProxy bool  // MURETTO_TRUST_PROXY=1: client IP from X-Forwarded-For
	MaxBody    int64 // MURETTO_MAX_BODY_BYTES

	IngestPerMinute int // MURETTO_INGEST_PER_MINUTE (per token)
}

// Load reads the environment through getenv.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:              getenv("MURETTO_ADDR"),
		PublicURL:         strings.TrimRight(getenv("MURETTO_PUBLIC_URL"), "/"),
		DashboardURL:      strings.TrimRight(getenv("MURETTO_DASHBOARD_URL"), "/"),
		DatabaseURL:       getenv("MURETTO_DATABASE_URL"),
		DatabaseListenURL: getenv("MURETTO_DATABASE_LISTEN_URL"),
		SupabaseURL:       strings.TrimRight(getenv("MURETTO_SUPABASE_URL"), "/"),
		SupabaseJWTSecret: getenv("MURETTO_SUPABASE_JWT_SECRET"),
		DBMaxConns:        5,
		MaxBody:           8 << 20,
		IngestPerMinute:   30,
	}
	if c.Addr == "" {
		if p := getenv("PORT"); p != "" { // Railway and most PaaS hosts set PORT
			c.Addr = ":" + p
		} else {
			c.Addr = "127.0.0.1:8080"
		}
	}
	if c.PublicURL == "" {
		c.PublicURL = "http://" + c.Addr
		if strings.HasPrefix(c.Addr, ":") {
			c.PublicURL = "http://127.0.0.1" + c.Addr
		}
	}
	if c.DashboardURL == "" {
		c.DashboardURL = "http://localhost:3000"
	}
	if c.DatabaseListenURL == "" {
		c.DatabaseListenURL = c.DatabaseURL
	}
	var err error
	if c.DevLogin, err = boolEnv(getenv, "MURETTO_DEV_LOGIN"); err != nil {
		return c, err
	}
	if c.TrustProxy, err = boolEnv(getenv, "MURETTO_TRUST_PROXY"); err != nil {
		return c, err
	}
	if v := getenv("MURETTO_DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return c, errors.New("MURETTO_DB_MAX_CONNS must be a positive integer")
		}
		c.DBMaxConns = int32(n)
	}
	if v := getenv("MURETTO_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1024 {
			return c, errors.New("MURETTO_MAX_BODY_BYTES must be an integer >= 1024")
		}
		c.MaxBody = n
	}
	if v := getenv("MURETTO_INGEST_PER_MINUTE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, errors.New("MURETTO_INGEST_PER_MINUTE must be a positive integer")
		}
		c.IngestPerMinute = n
	}
	return c, c.validate()
}

func boolEnv(getenv func(string) string, k string) (bool, error) {
	switch getenv(k) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("%s must be 1 or 0", k)
}

func (c Config) validate() error {
	for _, u := range []struct{ name, val string }{{"MURETTO_PUBLIC_URL", c.PublicURL}, {"MURETTO_DASHBOARD_URL", c.DashboardURL}} {
		p, err := url.Parse(u.val)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil || (p.Path != "" && u.name == "MURETTO_DASHBOARD_URL") {
			return fmt.Errorf("%s must be an http(s) origin like https://app.example.com", u.name)
		}
		if p.Scheme == "http" && !IsLoopbackHost(p.Hostname()) {
			return fmt.Errorf("%s must use https unless it is a loopback address", u.name)
		}
	}
	if c.DevLogin && !IsLoopbackAddr(c.Addr) {
		return fmt.Errorf("MURETTO_DEV_LOGIN=1 is refused: listen address %q is not loopback (use 127.0.0.1:PORT). Dev login is for local development only", c.Addr)
	}
	if c.SupabaseURL == "" {
		if !c.DevLogin {
			return errors.New("MURETTO_SUPABASE_URL is required (the Supabase project URL, e.g. https://<ref>.supabase.co); only MURETTO_DEV_LOGIN=1 on loopback may run without it")
		}
	} else {
		p, err := url.Parse(c.SupabaseURL)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil ||
			p.Path != "" || p.RawQuery != "" || p.Fragment != "" || strings.HasSuffix(c.SupabaseURL, "?") {
			return errors.New("MURETTO_SUPABASE_URL must be the project origin like https://<ref>.supabase.co (no path)")
		}
		if p.Scheme == "http" && !IsLoopbackHost(p.Hostname()) {
			return errors.New("MURETTO_SUPABASE_URL must use https unless it is a loopback address")
		}
	}
	if c.SupabaseJWTSecret != "" {
		if c.SupabaseURL == "" {
			return errors.New("MURETTO_SUPABASE_JWT_SECRET needs MURETTO_SUPABASE_URL (the issuer)")
		}
		if len(c.SupabaseJWTSecret) < 32 {
			return errors.New("MURETTO_SUPABASE_JWT_SECRET must be at least 32 characters")
		}
	}
	return nil
}

// HTTPS reports whether the public URL is https (enables HSTS).
func (c Config) HTTPS() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// IsLoopbackAddr reports whether a listen address binds only to loopback.
// ":8080" binds every interface and is not loopback.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return IsLoopbackHost(host)
}

// IsLoopbackHost reports whether host is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ShutdownTimeout bounds graceful shutdown.
const ShutdownTimeout = 10 * time.Second
