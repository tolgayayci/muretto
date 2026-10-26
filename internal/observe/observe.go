// Package observe turns Claude Code hook spool files and transcripts into
// sessions and raw actions: the story side of the join.
package observe

import (
	"errors"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/paths"
)

// RawAction is an agent action plus the redacted material adapter signatures
// match against. Action.Identifiers is empty until adapters.Registry.Extract fills it.
type RawAction struct {
	Action contract.AgentAction
	Input  any    // redacted tool_input as decoded JSON
	Output string // redacted result text (Bash stdout+stderr, MCP text content, or error text)
}

// Batch is the result of one ingest pass.
type Batch struct {
	Sessions []contract.Session
	Actions  []RawAction
	Files    []string // spool files consumed; delete with Ack only after the store commits
	Warnings []contract.Warning
}

// IngestSpool reads all spool files.
func IngestSpool(p paths.Paths, developerID string) (Batch, error) {
	return Batch{}, errors.New("not implemented") // TODO(stream A)
}

// ScanTranscripts reads Claude Code transcripts modified since the given time.
func ScanTranscripts(claudeDir, developerID string, since time.Time) (Batch, error) {
	return Batch{}, errors.New("not implemented") // TODO(stream A)
}

// Ack deletes consumed spool files.
func Ack(files []string) error {
	return errors.New("not implemented") // TODO(stream A)
}
