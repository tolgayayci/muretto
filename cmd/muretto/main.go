// Command muretto shows what your AI coding agents created outside the repo.
// It is read-only: it never changes anything in a provider account.
package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/tolgayayci/muretto/internal/hook"
	"github.com/tolgayayci/muretto/internal/paths"
)

// Set at build time with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Fast path: Claude Code runs `muretto hook <event>` on every tool call.
	// Dispatch before opening the DB, loading adapters or reading config.
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		runHook(os.Args[2:])
		os.Exit(0)
	}
	// signal.NotifyContext keeps catching SIGINT until stop runs, so a second
	// Ctrl-C (the npm launcher forwards SIGINT too) is harmless.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := newApp().run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

// runHook never fails, never prints and always lets the process exit 0.
func runHook(args []string) {
	defer func() { _ = recover() }()
	event := ""
	if len(args) > 0 {
		event = args[0]
	}
	p, err := paths.Resolve()
	if err != nil {
		return
	}
	hook.Run(event, os.Stdin, p, time.Now().UTC())
}
