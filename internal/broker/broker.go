// Package broker is the only provider network client in Muretto. It refuses,
// in code and before any network I/O, every request that is not an
// allowlisted read.
package broker

import (
	"context"
	"errors"
	"net/http"
)

// Rule allows one read endpoint.
type Rule struct {
	Provider    string
	Host        string   // e.g. api.supabase.com
	Method      string   // GET, or POST only with BodySHA256 set in Go code
	PathPattern string   // e.g. /v1/projects/{ref}/functions; {x} matches one path segment
	Query       []string // allowed query parameter names
	BodySHA256  string   // required for POST: hex sha256 of the only allowed body
}

// Broker sends allowlisted requests.
type Broker struct {
	client *http.Client
	allow  []Rule
}

// New returns a broker that allows only the given rules.
func New(client *http.Client, allow []Rule) *Broker {
	return &Broker{client: client, allow: allow}
}

// Do sends req if it matches an allowlist rule, and fails otherwise.
func (b *Broker) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented") // TODO(stream B)
}
