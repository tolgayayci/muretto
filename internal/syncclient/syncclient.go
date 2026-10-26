// Package syncclient is the CLI side of the optional login and sync.
package syncclient

import (
	"context"
	"errors"
	"io"

	"github.com/tolgayayci/muretto/internal/contract"
)

// FieldDoc describes one field `muretto sync on` will send.
type FieldDoc struct {
	Entity string
	Field  string
	Note   string
}

// BuildBatch projects an Answer into a sync batch, skipping entities whose rev
// is unchanged since lastRevs. It returns the new rev map.
func BuildBatch(a contract.Answer, clientVersion string, lastRevs map[string]string) (contract.SyncBatch, map[string]string) {
	return contract.SyncBatch{}, lastRevs // TODO(stream E)
}

// Push uploads a batch.
func Push(ctx context.Context, baseURL, token string, b contract.SyncBatch) error {
	return errors.New("not implemented") // TODO(stream E)
}

// Login runs the device flow and returns a Muretto API token.
func Login(ctx context.Context, baseURL string, out io.Writer) (string, error) {
	return "", errors.New("not implemented") // TODO(stream E)
}

// Payload lists exactly what sync sends; it must match docs/SYNC_PAYLOAD.md.
func Payload() []FieldDoc { return nil }
