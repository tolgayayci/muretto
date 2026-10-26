// want: import of net/rpc is not on the allowlist
package fixture

import "net/rpc"

func f() { _, _ = rpc.Dial("tcp", "x:1") }
