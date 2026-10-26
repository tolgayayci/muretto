// Package claudecfg adds Muretto's hooks to Claude Code settings additively,
// and removes exactly what it added.
package claudecfg

import (
	"errors"

	"github.com/tolgayayci/muretto/internal/paths"
)

// Status describes Muretto's hooks in the Claude Code settings.
type Status struct {
	SettingsPath  string
	Installed     bool
	Events        []string
	HooksDisabled bool // disableAllHooks is set
	ManagedOnly   bool // allowManagedHooksOnly is set
}

// Install backs up the settings file, adds Muretto's hooks and records them in the manifest.
func Install(p paths.Paths, binaryPath string) (Status, error) {
	return Status{}, errors.New("not implemented") // TODO(stream A)
}

// Uninstall removes exactly the hooks listed in the manifest.
func Uninstall(p paths.Paths) (Status, error) {
	return Status{}, errors.New("not implemented") // TODO(stream A)
}

// Inspect reports the current state without changing anything.
func Inspect(p paths.Paths, binaryPath string) (Status, error) {
	return Status{}, errors.New("not implemented") // TODO(stream A)
}
