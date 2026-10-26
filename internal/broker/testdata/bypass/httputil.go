// want: import of net/http/httputil is not on the allowlist
package fixture

import "net/http/httputil"

var _ = httputil.NewSingleHostReverseProxy
