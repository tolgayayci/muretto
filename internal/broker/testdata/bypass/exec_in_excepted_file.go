// The setup.go exception allows exec.LookPath, nothing else.
// path: cmd/muretto/setup.go
// want: os/exec.Command is not allowed here
// want: os/exec.CommandContext is not allowed here
package fixture

import (
	"context"
	"os/exec"
)

func f() {
	_, _ = exec.LookPath("claude")
	_ = exec.Command("claude")
	_ = exec.CommandContext(context.Background(), "curl")
}
