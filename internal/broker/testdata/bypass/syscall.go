// want: import of syscall is not on the allowlist
package fixture

import "syscall"

func f() { _, _ = syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0) }
