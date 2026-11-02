// Package secrets reads and writes tokens in the OS keychain, with env-var
// overrides. It never falls back to a file: without a keychain and without an
// env var it refuses with a clear message.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// Service is the keychain service name.
const Service = "muretto"

// Source says where a token came from.
type Source string

// Token sources.
const (
	SourceEnv      Source = "env"
	SourceKeychain Source = "keychain"
	SourceNone     Source = "none"
)

// Errors.
var (
	ErrNotFound    = errors.New("no token stored")
	ErrUnavailable = errors.New("no OS keychain is available")
)

// Backend is the keychain. Tests use a map-backed fake.
type Backend interface {
	Get(service, key string) (string, error)
	Set(service, key, value string) error
	Delete(service, key string) error
}

// Store reads env vars first, then the keychain.
type Store struct {
	Backend Backend
	Getenv  func(string) string
	Timeout time.Duration
}

// New returns a store backed by the OS keychain.
func New() *Store {
	return &Store{Backend: osKeyring{}, Getenv: os.Getenv, Timeout: 3 * time.Second}
}

// ProviderKey is the keychain key for a provider account token.
func ProviderKey(provider, accountID string) string {
	return "provider/" + provider + "/" + accountID
}

// SyncKey is the keychain key for the sync token of a server host.
func SyncKey(host string) string { return "sync/" + host }

// ProviderEnv is the env var that overrides a provider token, e.g. MURETTO_SUPABASE_TOKEN.
func ProviderEnv(provider string) string {
	return "MURETTO_" + strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_TOKEN"
}

// SyncEnv overrides the sync token.
const SyncEnv = "MURETTO_SYNC_TOKEN"

// Provider returns the token for a provider account.
func (s *Store) Provider(provider, accountID string) (string, Source, error) {
	return s.get(ProviderEnv(provider), ProviderKey(provider, accountID))
}

// Sync returns the sync token for a server host.
func (s *Store) Sync(host string) (string, Source, error) {
	return s.get(SyncEnv, SyncKey(host))
}

func (s *Store) get(env, key string) (string, Source, error) {
	if v := strings.TrimSpace(s.Getenv(env)); v != "" {
		return v, SourceEnv, nil
	}
	if err := s.available(); err != nil {
		return "", SourceNone, err
	}
	var v string
	err := s.timed(func() error {
		var err error
		v, err = s.Backend.Get(Service, key)
		return err
	})
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && v == "") {
		return "", SourceNone, ErrNotFound
	}
	if err != nil {
		return "", SourceNone, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return v, SourceKeychain, nil
}

// Set stores a token in the keychain. It refuses when no keychain exists or
// the keychain won't take the item (a locked macOS keychain over SSH).
func (s *Store) Set(key, value string) error {
	if err := s.available(); err != nil {
		return err
	}
	err := s.timed(func() error { return s.Backend.Set(Service, key, value) })
	if err != nil && !errors.Is(err, ErrUnavailable) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

// Check stores and removes a probe item, so a caller can find out before
// asking for an approval that the keychain will keep the token.
func (s *Store) Check() error {
	if err := s.Set("check", "ok"); err != nil {
		return err
	}
	return s.Delete("check")
}

// Delete removes a token; a missing token is not an error.
func (s *Store) Delete(key string) error {
	if err := s.available(); err != nil {
		return err
	}
	err := s.timed(func() error { return s.Backend.Delete(Service, key) })
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// RefuseMessage explains what to do when no keychain is available.
func RefuseMessage(env string) string {
	return fmt.Sprintf("No OS keychain is available. Muretto never stores tokens in files. Set %s in the environment instead.%s", env, sshHint())
}

// LoginRefuseMessage explains why login can't keep its token. An env var
// doesn't help here: login is the only way to get a sync token.
func LoginRefuseMessage() string {
	return "No OS keychain is available to keep the login token. Muretto never stores tokens in files." + sshHint()
}

func sshHint() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	return " Over SSH the macOS keychain stays locked: run `security unlock-keychain` first."
}

func (s *Store) available() error {
	if _, ok := s.Backend.(osKeyring); !ok {
		return nil
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
		// Avoid godbus autolaunching dbus-launch on headless machines.
		if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			if _, err := os.Stat(fmt.Sprintf("/run/user/%d/bus", os.Getuid())); err != nil {
				return ErrUnavailable
			}
		}
	}
	return nil
}

func (s *Store) timed(f func() error) error {
	if s.Timeout <= 0 {
		return f()
	}
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(s.Timeout):
		return fmt.Errorf("%w: keychain did not answer within %s", ErrUnavailable, s.Timeout)
	}
}

type osKeyring struct{}

func (osKeyring) Get(service, key string) (string, error) { return keyring.Get(service, key) }
func (osKeyring) Set(service, key, value string) error    { return keyring.Set(service, key, value) }
func (osKeyring) Delete(service, key string) error        { return keyring.Delete(service, key) }

// MemBackend is an in-memory Backend for tests.
type MemBackend struct{ M map[string]string }

// Get implements Backend.
func (m *MemBackend) Get(service, key string) (string, error) {
	v, ok := m.M[service+"|"+key]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

// Set implements Backend.
func (m *MemBackend) Set(service, key, value string) error {
	if m.M == nil {
		m.M = map[string]string{}
	}
	m.M[service+"|"+key] = value
	return nil
}

// Delete implements Backend.
func (m *MemBackend) Delete(service, key string) error {
	if _, ok := m.M[service+"|"+key]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.M, service+"|"+key)
	return nil
}
