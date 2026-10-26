// Package paths resolves Muretto's local file layout and Claude Code's config dir.
package paths

import "errors"

// Paths is the layout of the Muretto home directory (~/.muretto or MURETTO_HOME).
type Paths struct {
	Home     string
	Config   string
	DB       string
	Spool    string
	HookLog  string
	Backups  string
	Manifest string
}

// Resolve returns the layout rooted at MURETTO_HOME, or ~/.muretto.
func Resolve() (Paths, error) {
	return Paths{}, errors.New("not implemented") // TODO(stream A)
}

// ClaudeConfigDir returns CLAUDE_CONFIG_DIR, or ~/.claude.
func ClaudeConfigDir() (string, error) {
	return "", errors.New("not implemented") // TODO(stream A)
}
