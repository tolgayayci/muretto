// Allowed: net/http for types and constants, os for files, net/url to parse,
// os/exec.LookPath in the one excepted file, and the module's own packages.
// path: cmd/muretto/setup.go
package fixture

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/tolgayayci/muretto/internal/broker"
)

func f(req *http.Request, resp *http.Response) (int, string) {
	_, _ = exec.LookPath("claude")
	_, _ = os.ReadFile("x")
	_, _ = url.Parse("https://api.supabase.com")
	_ = broker.ErrRefused
	_ = http.Header{}
	_ = http.NoBody
	if req.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, http.StatusText(resp.StatusCode)
	}
	return http.StatusOK, ""
}
