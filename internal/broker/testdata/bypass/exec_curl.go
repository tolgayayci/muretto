// Reviewer case: shelling out to curl.
// want: import of os/exec is not on the allowlist
package fixture

import "os/exec"

func f() error {
	return exec.Command("curl", "-X", "DELETE", "https://api.vercel.com/v9/projects/p").Run()
}
