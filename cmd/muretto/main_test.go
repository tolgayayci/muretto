package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/claudecfg"
	"github.com/tolgayayci/muretto/internal/config"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/render"
	"github.com/tolgayayci/muretto/internal/secrets"
	"github.com/tolgayayci/muretto/internal/store"
	"github.com/tolgayayci/muretto/internal/syncclient"
	tf "github.com/tolgayayci/muretto/internal/testfixture"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// ---- fakes for streams A, B and E ----

type fakeRegistry struct {
	scans    int
	accounts map[string][]adapters.Account
	ids      map[string][]contract.Identifier
}

func (r *fakeRegistry) Providers() []adapters.ProviderInfo {
	return []adapters.ProviderInfo{
		{Name: "supabase", DisplayName: "Supabase", TokenHelp: "Create a scoped token with Read permissions.",
			Coverage: []adapters.CoverageRow{{Kind: "supabase.table", Status: "partial", Note: "attributed and advisor-flagged tables"}}},
		{Name: "vercel", DisplayName: "Vercel", Coverage: []adapters.CoverageRow{{Kind: "vercel.deployment", Status: "full"}}},
	}
}

func (r *fakeRegistry) Accounts(_ context.Context, _ *broker.Broker, provider, token string) ([]adapters.Account, error) {
	if token == "" {
		return nil, errors.New("no token")
	}
	return r.accounts[provider], nil
}

func (r *fakeRegistry) Scan(_ context.Context, _ *broker.Broker, provider string, c adapters.Credentials) (adapters.ScanResult, error) {
	r.scans++
	if c.Token == "" {
		return adapters.ScanResult{}, errors.New("no token")
	}
	var out adapters.ScanResult
	ids := map[string]bool{}
	for _, x := range tf.Resources() {
		if x.Provider == provider {
			out.Resources = append(out.Resources, x)
			ids[x.ID] = true
		}
	}
	for _, f := range tf.Findings() {
		if ids[f.ResourceID] {
			out.Findings = append(out.Findings, f)
		}
	}
	for _, c := range tf.Costs() {
		if ids[c.ResourceID] {
			out.Costs = append(out.Costs, c)
		}
	}
	for _, c := range tf.Checks() {
		if c.Provider == provider {
			out.Checks = append(out.Checks, c)
		}
	}
	return out, nil
}

func (r *fakeRegistry) Extract(a observe.RawAction) []contract.Identifier { return r.ids[a.Action.ID] }

func (r *fakeRegistry) RemoveHint(res contract.Resource) *contract.Fix {
	if res.Kind == "supabase.table" {
		return &contract.Fix{Kind: contract.FixKindSql, Text: `DROP TABLE "public"."` + res.Name + `";`}
	}
	return nil
}

func (r *fakeRegistry) Allowlist() []broker.Rule { return nil }

type fakeHooks struct{ installed, uninstalled bool }

func (h *fakeHooks) Install(p paths.Paths, b string) (claudecfg.Status, error) {
	h.installed = true
	return claudecfg.Status{SettingsPath: "/fake/.claude/settings.json", Installed: true}, nil
}
func (h *fakeHooks) Uninstall(paths.Paths) (claudecfg.Status, error) {
	h.uninstalled = true
	return claudecfg.Status{SettingsPath: "/fake/.claude/settings.json"}, nil
}
func (h *fakeHooks) Inspect(paths.Paths, string) (claudecfg.Status, error) {
	return claudecfg.Status{SettingsPath: "/fake/.claude/settings.json", Installed: true, Events: []string{"PostToolUse"}}, nil
}

type fakeSync struct {
	revoked int
	pushes  int
	logins  int
	failErr error
	revsIn  int // how many stored revisions the last BuildBatch got
}

func (s *fakeSync) BuildBatch(a contract.Answer, v string, revs map[string]string) (contract.SyncBatch, map[string]string) {
	s.revsIn = len(revs)
	b := contract.SyncBatch{ClientVersion: v, Developer: a.Developer}
	for _, r := range a.Resources {
		b.Resources = append(b.Resources, contract.SyncResource{Entity: r, Rev: "0123456789abcdef"})
	}
	return b, map[string]string{"resources": "0123456789abcdef"}
}
func (s *fakeSync) Push(context.Context, string, string, contract.SyncBatch) error {
	s.pushes++
	return s.failErr
}
func (s *fakeSync) Login(context.Context, string, io.Writer) (string, error) {
	s.logins++
	return "mrt_" + strings.Repeat("0", 32), nil
}
func (s *fakeSync) Revoke(context.Context, string, string) error {
	s.revoked++
	return nil
}
func (s *fakeSync) Payload() []syncclient.FieldDoc {
	return []syncclient.FieldDoc{{Entity: "session", Field: "task_title", Note: "redacted, at most 120 chars"}, {Entity: "resource", Field: "name"}}
}

// ---- harness ----

type harness struct {
	t           *testing.T
	p           paths.Paths
	reg         *fakeRegistry
	hooks       *fakeHooks
	sync        *fakeSync
	sec         *secrets.Store
	env         map[string]string
	now         time.Time
	spool       observe.Batch
	transcripts observe.Batch
	ackErr      error
	acked       []string
	devID       string
	stdin       string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := filepath.Join(t.TempDir(), "muretto")
	p := paths.Paths{Home: home, Config: filepath.Join(home, "config.json"), DB: filepath.Join(home, "muretto.db"),
		Spool: filepath.Join(home, "spool"), HookLog: filepath.Join(home, "hook-errors.log"),
		Backups: filepath.Join(home, "backups"), Manifest: filepath.Join(home, "install-manifest.json")}
	cfg, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Developer.DisplayName = "alice"
	cfg.AddAccount(config.Account{Provider: "supabase", ID: tf.OrgID, Name: "acme", ConnectedAt: tf.Now})
	cfg.AddAccount(config.Account{Provider: "vercel", ID: tf.TeamID, Name: "example-team", ConnectedAt: tf.Now})
	if err := cfg.Save(p.Config); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, p: p, hooks: &fakeHooks{}, sync: &fakeSync{}, now: tf.Now, devID: cfg.DeveloperID(),
		env: map[string]string{
			"MURETTO_SUPABASE_TOKEN": "fake-supabase-token-for-tests",
			"MURETTO_VERCEL_TOKEN":   "fake-vercel-token-for-tests",
		}}
	h.sec = &secrets.Store{Backend: &secrets.MemBackend{}, Getenv: func(k string) string { return h.env[k] }}
	h.reg = &fakeRegistry{ids: map[string][]contract.Identifier{}, accounts: map[string][]adapters.Account{
		"supabase": {{ID: tf.OrgID, Name: "acme"}}, "vercel": {{ID: tf.TeamID, Name: "example-team"}},
	}}

	// The spool holds the fixture sessions; identifiers come from the fake Extract.
	for _, s := range tf.Sessions() {
		if s.DeveloperID == tf.AliceDev {
			s.DeveloperID = h.devID
		}
		h.spool.Sessions = append(h.spool.Sessions, s)
	}
	for _, act := range tf.Actions() {
		h.reg.ids[act.ID] = act.Identifiers
		act.Identifiers = nil
		h.spool.Actions = append(h.spool.Actions, observe.RawAction{Action: act, Output: deref(act.ResultExcerpt)})
	}
	h.spool.Files = []string{filepath.Join(p.Spool, "1-1-a.json")}

	// Recorded review decisions.
	st, err := store.Open(context.Background(), p.DB)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range tf.Reviews() {
		r.DecidedBy = &h.devID
		if err := st.SetReview(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	return h
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *harness) app(stdout, stderr io.Writer) *app {
	return &app{
		stdin: strings.NewReader(h.stdin), stdout: stdout, stderr: stderr,
		getenv:         func(k string) string { return h.env[k] },
		now:            func() time.Time { return h.now },
		isTTY:          func(any) bool { return false },
		resolvePaths:   func() (paths.Paths, error) { return h.p, nil },
		claudeDir:      func() (string, error) { return "/fake/.claude", nil },
		loadRegistry:   func() (Registry, error) { return h.reg, nil },
		extractVersion: "test",
		// Like the real spool, files stay until they are acked.
		ingestSpool: func(paths.Paths, string, string) (observe.Batch, error) { return h.spool, nil },
		scanTranscripts: func(string, string, string, time.Time) (observe.Batch, error) {
			b := h.transcripts
			h.transcripts = observe.Batch{}
			return b, nil
		},
		ack: func(files []string) error {
			if h.ackErr != nil {
				return h.ackErr
			}
			h.acked = append(h.acked, files...)
			h.spool = observe.Batch{}
			return nil
		},
		hooks: h.hooks, sync: h.sync, secrets: h.sec,
		readPassword: func() (string, error) { return "", errors.New("no tty") },
		executable:   func() (string, error) { return "/usr/local/bin/muretto", nil },
	}
}

func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	var out, errb bytes.Buffer
	code := h.app(&out, &errb).run(context.Background(), args)
	return out.String(), errb.String(), code
}

func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	out, errOut, code := h.run(args...)
	if code != 0 {
		h.t.Fatalf("muretto %v: exit %d\nstdout:\n%s\nstderr:\n%s", args, code, out, errOut)
	}
	return out
}

// ---- tests ----

func TestSessionRendersTargetLookAndJSONMatchesAnswer(t *testing.T) {
	h := newHarness(t)
	text := h.mustRun("session", "8f2a")
	for _, want := range []string{
		"Session 8f2a · alice · Claude Code · Oct 19 14:02–14:31\n",
		"Task: \"add customer import with file uploads\"\n",
		"Repo: acme/web (feat/import)\n",
		"  SUPABASE  project acme-dev\n",
		"  ✓ table      customers        exact   ⚠ RLS off: publicly readable\n",
		"  ✓ bucket     customer-files   exact   ⚠ public\n",
		"  ✓ function   process-import   exact\n",
		"  ✓ branch     import-test      exact   7 days old · review due\n",
		"  ✓ deployment preview-a81f     exact   ⚠ public preview\n",
		"  3 exposed · 1 needs review · checked 3 min ago\n",
		"  → muretto why customers   → muretto fix customers\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}

	raw := h.mustRun("session", "8f2a", "--json")
	if err := tf.ValidateAnswer([]byte(raw)); err != nil {
		t.Fatalf("--json is not a valid Answer: %v", err)
	}
	var an contract.Answer
	if err := json.Unmarshal([]byte(raw), &an); err != nil {
		t.Fatal(err)
	}
	// The text view is a pure rendering of exactly this JSON.
	var again bytes.Buffer
	if err := render.Session(&again, an, tf.AliceSession, render.Options{Now: h.now, Loc: time.UTC}); err != nil {
		t.Fatal(err)
	}
	if again.String() != text {
		t.Fatalf("text and --json disagree:\n%s\nvs\n%s", text, again.String())
	}
	if len(an.Sessions) != 1 || len(an.Sessions[0].ResourceIds) != 5 || an.Summary.Exposed != 3 {
		t.Fatalf("session subset: %+v", an.Summary)
	}
}

func TestIngestAckAndScanCache(t *testing.T) {
	h := newHarness(t)
	h.mustRun()
	if len(h.acked) != 1 {
		t.Fatalf("spool files should be acked after the store commits: %v", h.acked)
	}
	if h.reg.scans != 2 {
		t.Fatalf("want one scan per account, got %d", h.reg.scans)
	}
	h.now = h.now.Add(30 * time.Second)
	h.mustRun("sessions")
	if h.reg.scans != 2 {
		t.Fatalf("scan younger than 60 s must be reused, got %d scans", h.reg.scans)
	}
	h.mustRun("sessions", "--refresh")
	if h.reg.scans != 4 {
		t.Fatalf("--refresh must rescan, got %d", h.reg.scans)
	}
	h.now = h.now.Add(2 * time.Minute)
	h.mustRun("exposed")
	if h.reg.scans != 6 {
		t.Fatalf("stale cache must rescan, got %d", h.reg.scans)
	}
	// Actions were stored once and keep their identifiers.
	out := h.mustRun("why", "customers")
	if !strings.Contains(out, "table customers: exact") || !strings.Contains(out, "toolu_01FAKEaaaaaaaaaaaaaaaaa1") {
		t.Fatalf("why:\n%s", out)
	}
}

// A crash (here: a failed Ack) after the store commits must re-ingest the
// same spool files next time without changing anything.
func TestReingestAfterFailedAckIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.ackErr = errors.New("simulated crash before ack")
	first := h.mustRun("--json")
	if len(h.acked) != 0 || len(h.spool.Files) != 1 {
		t.Fatal("spool must stay when ack fails")
	}
	h.ackErr = nil
	h.now = h.now.Add(time.Second)
	second := h.mustRun("--json")
	if len(h.acked) != 1 {
		t.Fatal("second run should ack")
	}
	var a, b contract.Answer
	_ = json.Unmarshal([]byte(first), &a)
	_ = json.Unmarshal([]byte(second), &b)
	if len(a.Attributions) != len(b.Attributions) || a.Summary.AgentCreated != b.Summary.AgentCreated || a.Summary.Sessions != b.Summary.Sessions {
		t.Fatalf("re-ingest changed the answer: %+v vs %+v", a.Summary, b.Summary)
	}
}

// observe.Merge reports a tool call held by two sessions with identical
// timing; the resource it created must be unknown and the warning shown.
func TestMergeAmbiguityMakesResourceUnknown(t *testing.T) {
	h := newHarness(t)
	var deploy observe.RawAction
	for _, ra := range h.spool.Actions {
		if ra.Action.ID == "toolu_01FAKEaaaaaaaaaaaaaaaaa5" {
			deploy = ra
		}
	}
	twin := tf.Sessions()[0]
	twin.ID = "8f2a1c3e-0000-4000-8000-0000000000ff"
	twin.DeveloperID = h.devID
	copyAct := deploy
	copyAct.Action.SessionID = twin.ID
	// Make both copies equal in source, time and session start.
	for i := range h.spool.Actions {
		if h.spool.Actions[i].Action.ID == deploy.Action.ID {
			h.spool.Actions[i].Action.Source = contract.AgentActionSourceTranscript
		}
	}
	copyAct.Action.Source = contract.AgentActionSourceTranscript
	h.transcripts = observe.Batch{Sessions: []contract.Session{twin}, Actions: []observe.RawAction{copyAct}}

	raw := h.mustRun("--json")
	var an contract.Answer
	if err := json.Unmarshal([]byte(raw), &an); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range an.Warnings {
		found = found || w.Code == "action_owner_ambiguous"
	}
	if !found {
		t.Fatalf("warning not surfaced: %+v", an.Warnings)
	}
	for _, at := range an.Attributions {
		if at.ResourceID == "vercel.deployment:"+tf.DeployID && at.Label != contract.AttributionLabelUnknown {
			t.Fatalf("ambiguous owner must give unknown: %+v", at)
		}
	}
	// Stays unknown on later runs (the flag is stored, not just warned once).
	h.now = h.now.Add(2 * time.Minute)
	_ = json.Unmarshal([]byte(h.mustRun("--json")), &an)
	for _, at := range an.Attributions {
		if at.ResourceID == "vercel.deployment:"+tf.DeployID && at.Label != contract.AttributionLabelUnknown {
			t.Fatal("ambiguity forgotten on the next run")
		}
	}
}

func TestSummaryJSONIsFullAnswer(t *testing.T) {
	h := newHarness(t)
	raw := h.mustRun("--json")
	if err := tf.ValidateAnswer([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	var an contract.Answer
	_ = json.Unmarshal([]byte(raw), &an)
	if an.Summary.AgentCreated != 5 || an.Summary.Sessions != 3 || an.Summary.Exposed != 3 {
		t.Fatalf("summary: %+v", an.Summary)
	}
	text := h.mustRun()
	if !strings.Contains(text, "5 agent-created resources · 5 still exist · 3 exposed · 1 needs review") {
		t.Fatalf("summary text:\n%s", text)
	}
}

func TestResourceArgumentsAcceptPrefixesAndNames(t *testing.T) {
	h := newHarness(t)
	for _, arg := range []string{"customers", "CUSTOMERS", "supabase.table:" + tf.DevRef + "/public.customers", tf.DevRef + "/public.cust"} {
		out := h.mustRun("show", arg)
		if !strings.HasPrefix(out, "table customers") {
			t.Fatalf("show %s:\n%s", arg, out)
		}
	}
	if out := h.mustRun("show", "process-i"); !strings.HasPrefix(out, "function process-import") {
		t.Fatalf("name prefix:\n%s", out)
	}
	_, errOut, code := h.run("show", "cust")
	if code != 1 || !strings.Contains(errOut, "matches 2 resources") {
		t.Fatalf("ambiguous name prefix: code %d %s", code, errOut)
	}
	_, errOut, code = h.run("show", "proj")
	if code != 1 || !strings.Contains(errOut, "matches") {
		t.Fatalf("ambiguous prefix: code %d %s", code, errOut)
	}
	_, errOut, code = h.run("session", "nope")
	if code != 1 || !strings.Contains(errOut, "no session matches") {
		t.Fatalf("missing session: %d %s", code, errOut)
	}
	out := h.mustRun("session", "add customer import with file uploads")
	if !strings.HasPrefix(out, "Session 8f2a") {
		t.Fatal("session by title")
	}
}

func TestFixPrintsAndReviewRecords(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun("fix", "customers")
	for _, want := range []string{`ALTER TABLE "public"."customers" ENABLE ROW LEVEL SECURITY;`, `DROP TABLE "public"."customers";`, "never runs"} {
		if !strings.Contains(out, want) {
			t.Errorf("fix missing %q:\n%s", want, out)
		}
	}
	out = h.mustRun("review")
	if !strings.Contains(out, "import-test") {
		t.Fatalf("review:\n%s", out)
	}
	out = h.mustRun("review", "import-test", "not-needed", "--note", "demo branch")
	if !strings.Contains(out, "Recorded") {
		t.Fatal(out)
	}
	out = h.mustRun("review")
	if !strings.HasPrefix(out, "Nothing to review.") || !strings.Contains(out, "1 not needed") {
		t.Fatalf("after decision:\n%s", out)
	}
	_, _, code := h.run("review", "acme-prod", "keep")
	if code != 1 {
		t.Fatal("reviewing a resource nobody created should fail")
	}
}

func TestResourcesFilters(t *testing.T) {
	h := newHarness(t)
	raw := h.mustRun("resources", "--unknown", "--json")
	var an contract.Answer
	_ = json.Unmarshal([]byte(raw), &an)
	for _, at := range an.Attributions {
		if at.Label != contract.AttributionLabelUnknown {
			t.Fatalf("--unknown returned %s", at.ResourceID)
		}
	}
	out := h.mustRun("resources", "--provider", "vercel", "--exposed")
	if !strings.Contains(out, "preview-a81f") || strings.Contains(out, "customers") || strings.Contains(out, "acme-web") {
		t.Fatalf("filters:\n%s", out)
	}
	if err := tf.ValidateAnswer([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

// lockedKeychain refuses to store keys with the given prefix, like a locked
// macOS keychain over SSH ("exit status 36").
type lockedKeychain struct {
	secrets.MemBackend
	prefix string
}

func (k *lockedKeychain) Set(service, key, value string) error {
	if strings.HasPrefix(key, k.prefix) {
		return errors.New("exit status 36")
	}
	return k.MemBackend.Set(service, key, value)
}

func TestLoginNeedsAKeychainFirst(t *testing.T) {
	h := newHarness(t)
	h.sec.Backend = &lockedKeychain{}
	_, errOut, code := h.run("login")
	if code == 0 || h.sync.logins != 0 || !strings.Contains(errOut, "No OS keychain is available") || strings.Contains(errOut, "exit status") {
		t.Fatalf("login must refuse before asking for an approval: code %d, logins %d, %q", code, h.sync.logins, errOut)
	}

	// The keychain takes the check but not the token: the token is revoked.
	h.sec.Backend = &lockedKeychain{prefix: "sync/"}
	if _, _, code := h.run("login"); code == 0 || h.sync.logins != 1 || h.sync.revoked != 1 {
		t.Fatalf("an unkept token must be revoked: code %d, logins %d, revoked %d", code, h.sync.logins, h.sync.revoked)
	}
}

func TestSyncSendsEverythingToANewServerOrLogin(t *testing.T) {
	h := newHarness(t)
	h.mustRun("login")
	h.mustRun("sync", "on", "--yes")
	h.mustRun("sessions")
	h.mustRun("sessions")
	if h.sync.revsIn == 0 {
		t.Fatal("the same server should only get what changed")
	}

	cfg, _ := config.Load(h.p.Config)
	cfg.Sync.Server = "https://api.other.example"
	if err := cfg.Save(h.p.Config); err != nil {
		t.Fatal(err)
	}
	h.env["MURETTO_SYNC_TOKEN"] = "mrt_" + strings.Repeat("1", 32)
	h.mustRun("sessions")
	if h.sync.revsIn != 0 {
		t.Fatalf("a new server must get everything, but %d revisions carried over", h.sync.revsIn)
	}

	h.mustRun("sessions")
	h.mustRun("login")
	h.mustRun("sessions")
	if h.sync.revsIn != 0 {
		t.Fatalf("a new login must send everything, but %d revisions carried over", h.sync.revsIn)
	}
}

func TestSyncNeverFailsTheCommand(t *testing.T) {
	h := newHarness(t)
	h.mustRun("login")
	_, _, code := h.run("sync", "on")
	if code != 2 {
		t.Fatalf("sync on without a terminal needs --yes, got %d", code)
	}
	out := h.mustRun("sync", "on", "--yes")
	if !strings.Contains(out, "task_title") || !strings.Contains(out, "never sends tokens") {
		t.Fatalf("sync on must print the payload:\n%s", out)
	}
	h.mustRun("sessions")
	if h.sync.pushes != 1 {
		t.Fatalf("want one push, got %d", h.sync.pushes)
	}
	h.sync.failErr = errors.New("server unreachable")
	out, errOut, code := h.run("sessions")
	if code != 0 || !strings.Contains(errOut, "sync: server unreachable") || !strings.Contains(out, "3 sessions") {
		t.Fatalf("sync failure must not fail the command: %d %q", code, errOut)
	}
	h.mustRun("sync", "off")
	before := h.sync.pushes
	h.mustRun("sessions")
	if h.sync.pushes != before {
		t.Fatal("sync off must not push")
	}
}

func TestMissingTokenIsAWarningNotAFailure(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "MURETTO_VERCEL_TOKEN")
	out, _, code := h.run()
	if code != 0 || !strings.Contains(out, "No token for vercel account example-team") {
		t.Fatalf("code %d:\n%s", code, out)
	}
}

func TestInitNonInteractiveUsesEnvTokens(t *testing.T) {
	h := newHarness(t)
	_ = os.Remove(h.p.Config)
	out := h.mustRun("init", "--name", "alice")
	if !h.hooks.installed || !strings.Contains(out, "Connected acme") || !strings.Contains(out, "Next steps") {
		t.Fatalf("init:\n%s", out)
	}
	cfg, _ := config.Load(h.p.Config)
	if len(cfg.Accounts) != 2 || cfg.Developer.DisplayName != "alice" {
		t.Fatalf("config: %+v", cfg)
	}
	raw, _ := os.ReadFile(h.p.Config)
	if strings.Contains(string(raw), "fake-supabase-token") {
		t.Fatal("token written to config.json")
	}
	out = h.mustRun("uninstall")
	if !h.hooks.uninstalled || !strings.Contains(out, "--purge") {
		t.Fatal(out)
	}
}

func TestProvidersDoctorVersionAndUsage(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun("providers", "--coverage")
	if !strings.Contains(out, "Supabase") || !strings.Contains(out, "token from env") || !strings.Contains(out, "partial") {
		t.Fatalf("providers:\n%s", out)
	}
	if err := json.Unmarshal([]byte(h.mustRun("providers", "--json")), &providersJSON{}); err != nil {
		t.Fatal(err)
	}
	out = h.mustRun("doctor")
	if !strings.Contains(out, "✓ hooks") || !strings.Contains(out, "supabase acme token") {
		t.Fatalf("doctor:\n%s", out)
	}
	if out := h.mustRun("version"); !strings.HasPrefix(out, "muretto dev (commit none, built unknown)") {
		t.Fatal(out)
	}
	if _, _, code := h.run("bogus"); code != 2 {
		t.Fatal("unknown command should exit 2")
	}
	if _, _, code := h.run("session"); code != 2 {
		t.Fatal("missing argument should exit 2")
	}
}

func TestBinaryPathWarning(t *testing.T) {
	if binaryPathWarning("/Users/x/.npm/_npx/abc/node_modules/muretto-darwin-arm64/bin/muretto") == "" {
		t.Fatal("npx cache path must warn")
	}
	if binaryPathWarning("/repo/node_modules/muretto-linux-x64/bin/muretto") == "" {
		t.Fatal("project node_modules must warn")
	}
	if binaryPathWarning("/opt/homebrew/bin/muretto") != "" {
		t.Fatal("stable path must not warn")
	}
}

// TestHookFastPath builds the real binary and feeds it a PostToolUse fixture:
// it must exit 0 with no output, and start fast.
func TestHookFastPath(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "muretto")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fixture, err := os.ReadFile("../../docs/research/fixtures/claude-code/hook-posttooluse-bash.json")
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	var best time.Duration
	for i := 0; i < 5; i++ {
		cmd := exec.Command(bin, "hook", "PostToolUse")
		cmd.Stdin = bytes.NewReader(fixture)
		cmd.Env = append(os.Environ(), "MURETTO_HOME="+home)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		start := time.Now()
		if err := cmd.Run(); err != nil {
			t.Fatalf("hook exited non-zero: %v", err)
		}
		d := time.Since(start)
		if best == 0 || d < best {
			best = d
		}
		if out.Len() != 0 || errb.Len() != 0 {
			t.Fatalf("hook must be silent, got stdout=%q stderr=%q", out.String(), errb.String())
		}
	}
	// Garbage and an empty event also exit 0 silently.
	for _, args := range [][]string{{"hook"}, {"hook", "PostToolUse"}} {
		cmd := exec.Command(bin, args...)
		cmd.Stdin = strings.NewReader("{not json")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil || out.Len() != 0 {
			t.Fatalf("%v: err=%v out=%q", args, err, out.String())
		}
	}
	t.Logf("hook process wall time (best of 5): %v", best)
	if best > 500*time.Millisecond {
		t.Errorf("hook start-up too slow: %v", best)
	}
}
