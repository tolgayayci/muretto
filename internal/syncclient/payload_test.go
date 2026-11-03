package syncclient

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// jsonFields lists the JSON field paths of a contract type, e.g.
// "repo.path_hash" or "evidence[].excerpt".
func jsonFields(t reflect.Type, prefix string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case ft.Kind() == reflect.Struct && ft != reflect.TypeFor[time.Time]():
			jsonFields(ft, prefix+name+".", out)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			jsonFields(ft.Elem(), prefix+name+"[].", out)
		default:
			*out = append(*out, prefix+name)
		}
	}
}

func TestPayloadMatchesSyncBatchType(t *testing.T) {
	var want []string
	add := func(entity string, v any) {
		var fs []string
		jsonFields(reflect.TypeOf(v), "", &fs)
		for _, f := range fs {
			want = append(want, entity+" "+f)
		}
	}
	for _, f := range []string{"schema_version", "batch_id", "sent_at", "client_version"} {
		want = append(want, "batch "+f)
	}
	add("developer", contract.Developer{})
	for entity, v := range map[string]any{
		"session": contract.SyncSession{}, "resource": contract.SyncResource{}, "attribution": contract.SyncAttribution{},
		"finding": contract.SyncFinding{}, "cost": contract.SyncCost{}, "review": contract.SyncReview{}, "check": contract.SyncCheck{},
	} {
		var fs []string
		jsonFields(reflect.TypeOf(v), "", &fs)
		for _, f := range fs {
			want = append(want, entity+" "+strings.TrimPrefix(f, "entity."))
		}
	}
	// Every SyncBatch top-level field is covered by an entity above.
	var top []string
	jsonFields(reflect.TypeFor[contract.SyncBatch](), "", &top)
	if len(top) < 10 {
		t.Fatalf("unexpected SyncBatch shape: %v", top)
	}

	var got []string
	seen := map[string]bool{}
	for _, d := range Payload() {
		k := d.Entity + " " + d.Field
		if seen[k] {
			t.Errorf("duplicate payload entry %q", k)
		}
		seen[k] = true
		if d.Note == "" {
			t.Errorf("%q has no note", k)
		}
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Fatalf("Payload() does not match the SyncBatch type.\nmissing: %v\nextra: %v", diff(want, got), diff(got, want))
	}
}

func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	return out
}

const (
	docPath    = "SYNC_PAYLOAD.md"
	beginTable = "<!-- payload:begin (generated from syncclient.Payload(); run MURETTO_UPDATE_SYNC_PAYLOAD=1 go test ./internal/syncclient) -->"
	endTable   = "<!-- payload:end -->"
)

func renderTable() string {
	var b strings.Builder
	b.WriteString("| Entity | Field | What is sent |\n|---|---|---|\n")
	for _, d := range Payload() {
		b.WriteString("| " + d.Entity + " | `" + d.Field + "` | " + d.Note + " |\n")
	}
	return b.String()
}

func TestPayloadDocInSync(t *testing.T) {
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	i, j := strings.Index(doc, beginTable), strings.Index(doc, endTable)
	if i < 0 || j < i {
		t.Fatalf("%s: payload markers missing", docPath)
	}
	want := beginTable + "\n" + renderTable() + endTable
	got := doc[i : j+len(endTable)]
	if got == want {
		return
	}
	if os.Getenv("MURETTO_UPDATE_SYNC_PAYLOAD") == "1" {
		if err := os.WriteFile(docPath, []byte(doc[:i]+want+doc[j+len(endTable):]), 0o644); err != nil { //nolint:gosec // a doc file
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("internal/syncclient/SYNC_PAYLOAD.md is out of date with Payload(); run MURETTO_UPDATE_SYNC_PAYLOAD=1 go test ./internal/syncclient")
}
