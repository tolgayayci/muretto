// Package hook implements `muretto hook <event>`: it writes one redacted,
// size-capped spool file and returns. It never blocks or alters the agent.
package hook

import (
	"io"
	"time"

	"github.com/tolgayayci/muretto/internal/paths"
)

// Run never returns an error, never writes to stdout or stderr and never
// touches the network. The caller always exits 0.
func Run(event string, stdin io.Reader, p paths.Paths, now time.Time) {
	// TODO(stream A): implement
}
