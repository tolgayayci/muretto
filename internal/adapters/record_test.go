package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tolgayayci/muretto/internal/broker"
)

// recording is one response the broker returned, after broker stripping.
type recording struct {
	Method string
	Path   string
	Query  map[string]string
	Status int
	Body   []byte
}

// pseudonymizer replaces identifying values with stable, obviously fake ones.
// The same input always maps to the same fake, so ids still join across files.
type pseudonymizer struct {
	keys map[string]bool // "name" (any depth) or "parent.name"
	keep map[string]bool
	m    map[string]string
	n    int
}

var (
	reRef    = regexp.MustCompile(`^[a-z]{20}$`)
	rePrefix = regexp.MustCompile(`^([A-Za-z]+_)`)
)

func newPseudonymizer(spec recorderSpec) *pseudonymizer {
	p := &pseudonymizer{keys: map[string]bool{}, keep: map[string]bool{}, m: map[string]string{}}
	for _, k := range spec.Pseudonymize {
		p.keys[k] = true
	}
	for _, k := range spec.Keep {
		p.keep[k] = true
	}
	return p
}

func (p *pseudonymizer) collect(v any, parent string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok && (p.keys[k] || p.keys[parent+"."+k]) {
				p.add(s, k)
			}
			p.collect(child, k)
		}
	case []any:
		for _, el := range t {
			p.collect(el, parent)
		}
	}
}

func (p *pseudonymizer) add(orig, key string) {
	if len(orig) < 3 || p.keep[orig] || p.m[orig] != "" {
		return
	}
	p.n++
	var fake string
	switch {
	case reRef.MatchString(orig):
		fake = "fake" + letters(p.n, 16)
	case strings.Contains(orig, "@"):
		fake = fmt.Sprintf("user%d@example.com", p.n)
	case rePrefix.MatchString(orig):
		fake = rePrefix.FindString(orig) + fmt.Sprintf("FAKE%06d", p.n)
	default:
		fake = fmt.Sprintf("fake-%s-%d", strings.ToLower(key), p.n)
	}
	p.m[orig] = fake
}

func letters(n, width int) string {
	b := bytes.Repeat([]byte{'a'}, width)
	for i := width - 1; i >= 0 && n > 0; i-- {
		b[i] = byte('a' + n%26)
		n /= 26
	}
	return string(b)
}

// apply replaces every collected value (longest first) wherever it appears.
func (p *pseudonymizer) apply(s string) string {
	origs := make([]string, 0, len(p.m))
	for o := range p.m {
		origs = append(origs, o)
	}
	sort.Slice(origs, func(i, j int) bool { return len(origs[i]) > len(origs[j]) })
	for _, o := range origs {
		s = strings.ReplaceAll(s, o, p.m[o])
	}
	return s
}

// sanitizeRecordings pseudonymizes recordings in place and refuses to return
// anything that still looks like a secret.
func sanitizeRecordings(recs []recording, spec recorderSpec) error {
	p := newPseudonymizer(spec)
	docs := make([]any, len(recs))
	for i, r := range recs {
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		if err := dec.Decode(&docs[i]); err == nil {
			p.collect(docs[i], "")
		}
	}
	for i := range recs {
		body := string(recs[i].Body)
		if docs[i] != nil {
			out, _ := json.MarshalIndent(docs[i], "", "  ")
			body = string(out)
		}
		recs[i].Body = []byte(p.apply(body) + "\n")
		recs[i].Path = p.apply(recs[i].Path)
		for k, v := range recs[i].Query {
			recs[i].Query[k] = p.apply(v)
		}
		if hits := findSecrets(string(recs[i].Body) + recs[i].Path); len(hits) > 0 {
			return fmt.Errorf("%s %s: still secret-shaped after sanitizing: %v", recs[i].Method, recs[i].Path, hits)
		}
	}
	return nil
}

func TestSanitizeRecordings(t *testing.T) {
	r := loadEmbedded(t)
	spec := r.byName["supabase"].Recorder
	recs := []recording{
		{Method: "GET", Path: "/v1/projects", Status: 200, Body: []byte(`[{"ref":"qwertyuiopasdfghjklz","name":"acme-prod","organization_slug":"acme-corp","created_at":"2026-10-20T09:15:00Z"}]`)},
		{Method: "GET", Path: "/v1/projects/qwertyuiopasdfghjklz/advisors/security", Query: map[string]string{"lint_type": "sql"}, Status: 200,
			Body: []byte(`{"lints":[{"name":"rls_disabled_in_public","detail":"Table public.payroll is public","metadata":{"schema":"public","name":"payroll"}}]}`)},
	}
	if err := sanitizeRecordings(recs, spec); err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, r := range recs {
		all += r.Path + string(r.Body)
	}
	for _, real := range []string{"qwertyuiopasdfghjklz", "acme-prod", "acme-corp", "payroll"} {
		if strings.Contains(all, real) {
			t.Errorf("real value %q survived", real)
		}
	}
	for _, kept := range []string{"rls_disabled_in_public", `"schema": "public"`} {
		if !strings.Contains(all, kept) {
			t.Errorf("value %q should be kept", kept)
		}
	}
	if !strings.Contains(recs[1].Path, "/v1/projects/fake") {
		t.Errorf("path not pseudonymized consistently: %s", recs[1].Path)
	}

	leak := []recording{{Method: "GET", Path: "/x", Body: []byte(`{"note":"contact ` + "someone" + "@" + `gmail.com"}`)}}
	if err := sanitizeRecordings(leak, recorderSpec{}); err == nil {
		t.Error("a secret-shaped value outside pseudonymized keys must refuse the recording")
	}
}

// TestRecordLive records real responses from a test account, through the
// broker, and writes sanitized fixtures to adapters/testdata/<provider>/recorded/
// (git-ignored; review before copying anything into a scenario). Opt-in:
//
//	MURETTO_LIVE=1 MURETTO_SUPABASE_TOKEN=... [MURETTO_SUPABASE_ACCOUNT=org-slug] \
//	MURETTO_VERCEL_TOKEN=... [MURETTO_VERCEL_ACCOUNT=team_...] \
//	[MURETTO_RECORD_OPTIONS=catalog_rls] go test ./internal/adapters -run TestRecordLive
func TestRecordLive(t *testing.T) {
	if os.Getenv("MURETTO_LIVE") != "1" {
		t.Skip("set MURETTO_LIVE=1 to record from a real test account")
	}
	r := loadEmbedded(t)
	for _, p := range r.Providers() {
		up := strings.ToUpper(p.Name)
		token := os.Getenv("MURETTO_" + up + "_TOKEN")
		if token == "" {
			t.Logf("%s: MURETTO_%s_TOKEN not set, skipping", p.Name, up)
			continue
		}
		t.Run(p.Name, func(t *testing.T) { recordProvider(t, r, p.Name, token, os.Getenv("MURETTO_"+up+"_ACCOUNT")) })
	}
}

func recordProvider(t *testing.T, r *Registry, provider, token, account string) {
	var mu sync.Mutex
	var recs []recording
	b := broker.New(nil, r.Allowlist())
	b.Observe(func(req *http.Request, status int, body []byte) {
		q := map[string]string{}
		for k, v := range req.URL.Query() {
			q[k] = v[0]
		}
		mu.Lock()
		recs = append(recs, recording{Method: req.Method, Path: req.URL.Path, Query: q, Status: status, Body: append([]byte(nil), body...)})
		mu.Unlock()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	accts, err := r.Accounts(ctx, b, provider, token)
	if err != nil {
		t.Fatalf("accounts: %v", err)
	}
	var acct Account
	for _, a := range accts {
		if account == "" || a.ID == account || a.Slug == account {
			acct = a
			break
		}
	}
	if acct.ID == "" {
		t.Fatalf("account %q not visible to this token", account)
	}
	opts := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("MURETTO_RECORD_OPTIONS"), ",") {
		if o != "" {
			opts[o] = true
		}
	}
	res, err := r.Scan(ctx, b, provider, Credentials{Token: token, AccountID: acct.ID, AccountName: acct.Name, AccountSlug: acct.Slug, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d resources, %d findings, %d checks, %d responses", provider, len(res.Resources), len(res.Findings), len(res.Checks), len(recs))

	if err := sanitizeRecordings(recs, r.byName[provider].Recorder); err != nil {
		t.Fatalf("refusing to write: %v", err)
	}
	dir := filepath.Join(testdataDir, provider, "recorded", time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var routes []map[string]any
	for i, rec := range recs {
		file := fmt.Sprintf("%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, file), rec.Body, 0o600); err != nil {
			t.Fatal(err)
		}
		rt := map[string]any{"method": rec.Method, "path": rec.Path, "status": rec.Status, "file": file}
		if len(rec.Query) > 0 {
			rt["query"] = rec.Query
		}
		routes = append(routes, rt)
	}
	out, _ := yaml.Marshal(routes)
	if err := os.WriteFile(filepath.Join(dir, "routes.yaml"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (git-ignored). Review it, write expect.yaml, then move it under scan/.", dir)
}
