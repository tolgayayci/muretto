// want: go:linkname can reach unexported network code
// want: import of unsafe is not on the allowlist
package fixture

import _ "unsafe"

//go:linkname dial net.dial
func dial()
