// Package config reads and writes ~/.muretto/config.json: the developer
// identity, connected accounts (ids only), sync settings and options.
// It never holds tokens.
package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// Version is the config file format version.
const Version = 1

// DefaultSyncServer is used when sync.server is empty.
const DefaultSyncServer = "https://api.muretto.ai"

// Config is the whole config.json.
type Config struct {
	Version   int       `json:"version"`
	Developer Developer `json:"developer"`
	Accounts  []Account `json:"accounts"`
	Sync      Sync      `json:"sync"`
	Options   Options   `json:"options"`
}

// Developer is the local identity. InstallID is random and never leaves the
// machine; only its salted hash is used as the developer id.
type Developer struct {
	InstallID    string `json:"install_id"`
	DisplayName  string `json:"display_name"`
	AccountLogin string `json:"account_login,omitempty"`
}

// Account is one connected provider account. The token lives in the keychain
// under secrets.ProviderKey(Provider, ID), or in an env var.
type Account struct {
	Provider    string    `json:"provider"`
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Slug        string    `json:"slug,omitempty"` // Vercel team slug / Supabase org slug, for name-based attribution
	ConnectedAt time.Time `json:"connected_at"`
}

// Sync holds the opt-in sync settings.
type Sync struct {
	Enabled   bool       `json:"enabled"`
	Server    string     `json:"server,omitempty"`
	EnabledAt *time.Time `json:"enabled_at,omitempty"`
}

// Options are user choices made in `muretto init`.
type Options struct {
	// SupabaseCatalogRead opts into listing every table through the read-only
	// SQL endpoint. That permission can read every row; off by default.
	SupabaseCatalogRead bool `json:"supabase_catalog_read"`
	// ReviewAfterDays sets the default review due date; 0 means 7.
	ReviewAfterDays int `json:"review_after_days,omitempty"`
}

// Load reads path. A missing file gives a fresh config (not yet saved).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the Muretto config path
	if errors.Is(err, fs.ErrNotExist) {
		return New()
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if c.Version > Version {
		return nil, fmt.Errorf("%s was written by a newer muretto (version %d)", path, c.Version)
	}
	if c.Developer.InstallID == "" {
		fresh, err := New()
		if err != nil {
			return nil, err
		}
		c.Developer.InstallID = fresh.Developer.InstallID
	}
	if c.Developer.DisplayName == "" {
		c.Developer.DisplayName = defaultName()
	}
	c.Version = Version
	return &c, nil
}

// New returns a fresh config with a random install id.
func New() (*Config, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Config{
		Version:   Version,
		Developer: Developer{InstallID: hex.EncodeToString(b), DisplayName: defaultName()},
		Accounts:  []Account{},
	}, nil
}

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// MachineIDHash is the salted hash of the install id (contract.Developer).
func (c *Config) MachineIDHash() string {
	sum := sha256.Sum256([]byte("muretto-machine-v1:" + c.Developer.InstallID))
	return hex.EncodeToString(sum[:16])
}

// DeveloperID is the local developer id: the machine hash, prefixed.
func (c *Config) DeveloperID() string { return "dev_" + c.MachineIDHash() }

// PathHashSalt keys observe.PathHash: HMAC-SHA256(install id, "muretto path
// hash v1"), hex. It derives from the install id, which never leaves the
// machine, and is never sent itself, so the sync server can't test guesses of
// likely paths against a path hash. (Unlike the developer id, which is synced.)
func (c *Config) PathHashSalt() string {
	m := hmac.New(sha256.New, []byte(c.Developer.InstallID))
	m.Write([]byte("muretto path hash v1"))
	return hex.EncodeToString(m.Sum(nil))
}

// SyncServer returns the configured server or the default.
func (c *Config) SyncServer() string {
	if c.Sync.Server != "" {
		return strings.TrimRight(c.Sync.Server, "/")
	}
	return DefaultSyncServer
}

// ReviewAfter is the default time until a review is due.
func (c *Config) ReviewAfter() time.Duration {
	d := c.Options.ReviewAfterDays
	if d <= 0 {
		d = 7
	}
	return time.Duration(d) * 24 * time.Hour
}

// AddAccount adds or replaces an account by provider and id.
func (c *Config) AddAccount(a Account) {
	for i, x := range c.Accounts {
		if x.Provider == a.Provider && x.ID == a.ID {
			c.Accounts[i] = a
			return
		}
	}
	c.Accounts = append(c.Accounts, a)
}

// RemoveAccount removes an account; it reports whether one was removed.
func (c *Config) RemoveAccount(provider, id string) bool {
	for i, x := range c.Accounts {
		if x.Provider == provider && x.ID == id {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

func defaultName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		if i := strings.LastIndex(name, `\`); i >= 0 {
			name = name[i+1:]
		}
		return name
	}
	return "me"
}
