package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/tolgayayci/muretto/server/internal/fixture"
)

func TestSyncBatch(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatal(err)
	}
	good := fixture.JSON(fixture.Batch("alice", "abcdefghijklmnopqrst"))
	if err := v.SyncBatch(good); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	cases := map[string]string{
		"unknown field": strings.Replace(string(good), `"client_version"`, `"extra":1,"client_version"`, 1),
		"bad format":    strings.Replace(string(good), `"sent_at":"2026-10-26T14:00:00Z"`, `"sent_at":"yesterday"`, 1),
		"bad enum":      strings.Replace(string(good), `"label":"exact"`, `"label":"likely"`, 1),
		"not json":      `{"schema_version":`,
	}
	for name, body := range cases {
		err := v.SyncBatch([]byte(body))
		var ve *Error
		if !errors.As(err, &ve) || len(ve.Problems) == 0 {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}
