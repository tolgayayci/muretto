// os is allowed for files, but not for running programs.
// want: os.StartProcess is not allowed here
package fixture

import "os"

func f() { _, _ = os.StartProcess("/usr/bin/curl", nil, &os.ProcAttr{}) }
