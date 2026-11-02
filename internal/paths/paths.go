// Package paths resolves Muretto's local file layout and Claude Code's config dir.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths is the layout of the Muretto home directory (~/.muretto or MURETTO_HOME).
type Paths struct {
	Home     string
	Config   string
	DB       string
	Spool    string
	HookLog  string
	Backups  string
	Manifest string
	Adapters string // adapters installed by the user (muretto adapter install)
}

// DirPerm and FilePerm are the permissions of everything under Home.
const (
	DirPerm  os.FileMode = 0o700
	FilePerm os.FileMode = 0o600
)

// Resolve returns the layout rooted at MURETTO_HOME, or ~/.muretto. It creates
// the home, spool and backups directories with 0700 if they don't exist.
func Resolve() (Paths, error) {
	home := os.Getenv("MURETTO_HOME")
	if home == "" {
		uh, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("find home directory: %w", err)
		}
		home = filepath.Join(uh, ".muretto")
	} else {
		var err error
		if home, err = expand(home); err != nil {
			return Paths{}, err
		}
	}
	p := At(home)
	if err := p.Ensure(); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// At returns the layout rooted at home without touching the file system.
func At(home string) Paths {
	return Paths{
		Home:     home,
		Config:   filepath.Join(home, "config.json"),
		DB:       filepath.Join(home, "muretto.db"),
		Spool:    filepath.Join(home, "spool"),
		HookLog:  filepath.Join(home, "hook-errors.log"),
		Backups:  filepath.Join(home, "backups"),
		Manifest: filepath.Join(home, "install-manifest.json"),
		Adapters: filepath.Join(home, "adapters"),
	}
}

// Ensure creates the home, spool and backups directories (0700). An existing
// home with group or world access is tightened to 0700.
func (p Paths) Ensure() error {
	if p.Home == "" {
		return errors.New("muretto home is not set")
	}
	for _, d := range []string{p.Home, p.Spool, p.Backups} {
		if err := os.MkdirAll(d, DirPerm); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	if fi, err := os.Stat(p.Home); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(p.Home, DirPerm) // best effort: we may not own a custom MURETTO_HOME
	}
	return nil
}

// ClaudeConfigDir returns CLAUDE_CONFIG_DIR, or ~/.claude.
func ClaudeConfigDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return expand(d)
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(uh, ".claude"), nil
}

// ClaudeGlobalConfig returns Claude Code's global config file, which holds
// user- and local-scope MCP servers: $CLAUDE_CONFIG_DIR/.claude.json when
// CLAUDE_CONFIG_DIR is set, else ~/.claude.json (outside ~/.claude).
func ClaudeGlobalConfig() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dir, err := expand(d)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, ".claude.json"), nil
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(uh, ".claude.json"), nil
}

// expand resolves a leading ~ and makes the path absolute.
func expand(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		uh, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		p = filepath.Join(uh, p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	return abs, nil
}
