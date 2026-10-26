// Reviewer case: a raw RoundTripper, implemented and called.
// want: RoundTrip method
// want: RoundTrip call bypasses the broker
// want: net/http.DefaultTransport is not allowed here
// want: net/http.RoundTripper is not allowed here
package fixture

import "net/http"

type rt struct{ next http.RoundTripper }

func (r rt) RoundTrip(req *http.Request) (*http.Response, error) { return r.next.RoundTrip(req) }

func f(req *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(req)
}
