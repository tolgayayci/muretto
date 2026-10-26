package attrib

import (
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// An identifier without its own excerpt falls back to a narrow, re-redacted
// window of the action's excerpts, never the whole input.
func TestEvidenceFallbackWindow(t *testing.T) {
	pw := "pw" + strings.Repeat("Fk9", 6) // built at runtime, never a literal
	ref := "abcdefghijklmnopqrst"
	far := strings.Repeat("x", 1000)
	// A secret that slipped past record-time redaction, close to the value,
	// and unrelated text far away on both sides.
	in := "FARHEAD " + far + " DB_PASS=" + pw + " supabase link --project-ref " + ref + " " + far + " FARTAIL"
	out := "Linked to " + ref + "."
	act := &contract.AgentAction{ID: "toolu_1", SessionID: "s1", ToolName: "Bash", OccurredAt: time.Unix(0, 0), InputExcerpt: &in, ResultExcerpt: &out}
	ev := evidence([]claim{{action: act, ident: contract.Identifier{SignatureID: "sig", Value: ref}}})
	if len(ev) != 1 {
		t.Fatalf("got %d evidence rows", len(ev))
	}
	ex := ev[0].Excerpt
	if strings.Contains(ex, pw) {
		t.Errorf("secret in evidence excerpt: %s", ex)
	}
	for _, mark := range []string{"FARHEAD", "FARTAIL"} {
		if strings.Contains(ex, mark) {
			t.Errorf("excerpt not narrowed, has %q (%d bytes)", mark, len(ex))
		}
	}
	for _, k := range []string{"--project-ref " + ref, "\n→ Linked to " + ref} {
		if !strings.Contains(ex, k) {
			t.Errorf("excerpt lost %q: %s", k, ex)
		}
	}
	if limit := 2*(2*windowRadius+len(ref)) + 4; len(ex) > limit {
		t.Errorf("excerpt is %d bytes, want <= %d", len(ex), limit)
	}

	// The identifier's own excerpt still wins.
	own := "own excerpt"
	ev = evidence([]claim{{action: act, ident: contract.Identifier{SignatureID: "sig", Value: ref, Excerpt: &own}}})
	if ev[0].Excerpt != own {
		t.Errorf("own excerpt replaced: %q", ev[0].Excerpt)
	}
}

func TestExcerptWindow(t *testing.T) {
	s := strings.Repeat("é", 300) + "VALUE" + strings.Repeat("ü", 300)
	got := excerptWindow(&s, "VALUE")
	if !strings.Contains(got, "VALUE") || len(got) > 2*windowRadius+len("VALUE")+4 {
		t.Errorf("bad window (%d bytes)", len(got))
	}
	if !strings.HasPrefix(got, "é") || !strings.HasSuffix(got, "ü") {
		t.Error("window split a UTF-8 rune")
	}
	// Value not found: the start of the text, still bounded.
	if got := excerptWindow(&s, "missing"); len(got) > 2*windowRadius+2 || !strings.HasPrefix(s, got) {
		t.Errorf("fallback window wrong (%d bytes)", len(got))
	}
	if excerptWindow(nil, "x") != "" {
		t.Error("nil excerpt")
	}
}
