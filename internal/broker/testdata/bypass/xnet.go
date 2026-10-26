// want: import of golang.org/x/net/http2 is not on the allowlist
// want: import of golang.org/x/net/proxy is not on the allowlist
package fixture

import (
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

var _ = http2.Transport{}
var _ = proxy.Direct
