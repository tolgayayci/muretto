// want: import of crypto/tls is not on the allowlist
package fixture

import "crypto/tls"

func f() { _, _ = tls.Dial("tcp", "api.vercel.com:443", nil) }
