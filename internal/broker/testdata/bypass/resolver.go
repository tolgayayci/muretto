// Reviewer case: DNS lookups leak data too.
// want: import of net is not on the allowlist
package fixture

import (
	"context"
	n "net"
)

func f() {
	r := &n.Resolver{PreferGo: true}
	_, _ = r.LookupHost(context.Background(), "secret.example.com")
}
