package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/answer"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/claudecfg"
	"github.com/tolgayayci/muretto/internal/config"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
	"github.com/tolgayayci/muretto/internal/render"
	"github.com/tolgayayci/muretto/internal/secrets"
	"github.com/tolgayayci/muretto/internal/store"
	"github.com/tolgayayci/muretto/internal/syncclient"
)

// scanTTL is how long a provider scan is reused before a new one runs.
const scanTTL = 60 * time.Second

// Registry is the part of *adapters.Registry the CLI uses (faked in tests).
type Registry interface {
	Providers() []adapters.ProviderInfo
	Accounts(ctx context.Context, b *broker.Broker, provider, token string) ([]adapters.Account, error)
	Scan(ctx context.Context, b *broker.Broker, provider string, c adapters.Credentials) (adapters.ScanResult, error)
	Extract(a observe.RawAction) []contract.Identifier
	RemoveHint(res contract.Resource) *contract.Fix
	Allowlist() []broker.Rule
}

// Hooks is claudecfg (faked in tests).
type Hooks interface {
	Install(p paths.Paths, binaryPath string) (claudecfg.Status, error)
	Uninstall(p paths.Paths) (claudecfg.Status, error)
	Inspect(p paths.Paths, binaryPath string) (claudecfg.Status, error)
}

// Sync is syncclient (faked in tests).
type Sync interface {
	BuildBatch(a contract.Answer, clientVersion string, lastRevs map[string]string) (contract.SyncBatch, map[string]string)
	Push(ctx context.Context, baseURL, token string, b contract.SyncBatch) error
	Login(ctx context.Context, baseURL string, out io.Writer) (string, error)
	Revoke(ctx context.Context, baseURL, token string) error
	Payload() []syncclient.FieldDoc
}

// app holds every dependency, so tests run commands in-process with fakes.
type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
	now    func() time.Time
	isTTY  func(f any) bool

	resolvePaths    func() (paths.Paths, error)
	claudeDir       func() (string, error)
	loadRegistry    func() (Registry, error)
	extractVersion  string
	ingestSpool     func(p paths.Paths, developerID, pathSalt string) (observe.Batch, error)
	scanTranscripts func(claudeDir, developerID, pathSalt string, since time.Time) (observe.Batch, error)
	ack             func(files []string) error
	hooks           Hooks
	sync            Sync
	secrets         *secrets.Store
	readPassword    func() (string, error)
	executable      func() (string, error)
	// newBroker builds the read-only provider broker. It is nil in the binary
	// (broker.New with the default client); end-to-end tests give the broker a
	// client that serves fixtures. The allowlist applies either way.
	newBroker func(allow []broker.Rule) *broker.Broker

	in *bufio.Reader
}

type realHooks struct{}

func (realHooks) Install(p paths.Paths, b string) (claudecfg.Status, error) {
	return claudecfg.Install(p, b)
}
func (realHooks) Uninstall(p paths.Paths) (claudecfg.Status, error) { return claudecfg.Uninstall(p) }
func (realHooks) Inspect(p paths.Paths, b string) (claudecfg.Status, error) {
	return claudecfg.Inspect(p, b)
}

type realSync struct{}

func (realSync) BuildBatch(a contract.Answer, v string, revs map[string]string) (contract.SyncBatch, map[string]string) {
	return syncclient.BuildBatch(a, v, revs)
}
func (realSync) Push(ctx context.Context, u, t string, b contract.SyncBatch) error {
	return syncclient.Push(ctx, u, t, b)
}
func (realSync) Login(ctx context.Context, u string, out io.Writer) (string, error) {
	return syncclient.Login(ctx, u, out)
}
func (realSync) Revoke(ctx context.Context, u, t string) error { return syncclient.Revoke(ctx, u, t) }
func (realSync) Payload() []syncclient.FieldDoc                { return syncclient.Payload() }

func newApp() *app {
	a := &app{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		getenv: os.Getenv,
		now:    func() time.Time { return time.Now().UTC() },
		isTTY: func(f any) bool {
			if file, ok := f.(*os.File); ok {
				return term.IsTerminal(int(file.Fd())) // #nosec G115 -- fd fits in int
			}
			return false
		},
		resolvePaths:    paths.Resolve,
		claudeDir:       paths.ClaudeConfigDir,
		extractVersion:  extractVersion(adapterfiles.Files),
		ingestSpool:     observe.IngestSpool,
		scanTranscripts: observe.ScanTranscripts,
		ack:             observe.Ack,
		hooks:           realHooks{},
		sync:            realSync{},
		secrets:         secrets.New(),
		readPassword: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd())) // #nosec G115 -- fd fits in int
			return string(b), err
		},
		executable: binaryPath,
	}
	a.loadRegistry = a.loadAllAdapters
	return a
}

// extractVersion changes whenever the binary or the adapter files change, so
// stored actions are re-extracted with the new signatures. Installed
// (local) adapters count too.
func extractVersion(fsys ...fs.FS) string {
	h := sha256.New()
	h.Write([]byte(version + "\x00" + commit))
	for i, f := range fsys {
		if f == nil {
			continue
		}
		_ = fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // skip unreadable entries; hash what can be read
			}
			b, err := fs.ReadFile(f, path)
			if err == nil {
				fmt.Fprintf(h, "%d:%s\x00", i, path)
				h.Write(b)
			}
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// maxAdapterFile caps an installed adapter file.
const maxAdapterFile = 4 << 20

// localAdapters reads the adapter files installed in dir (sorted by name).
func localAdapters(dir string) (names []string, data [][]byte, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if fi, err := e.Info(); err == nil && fi.Size() > maxAdapterFile {
			errs = append(errs, fmt.Errorf("%s: larger than %d bytes", p, maxAdapterFile))
			continue
		}
		b, err := os.ReadFile(p) // #nosec G304 -- the user's own adapters directory
		if err != nil {
			errs = append(errs, err)
			continue
		}
		names = append(names, p)
		data = append(data, b)
	}
	return names, data, errs
}

// loadAllAdapters loads the built-in adapters plus the user's installed
// ones. A broken local adapter is reported and skipped, never fatal.
func (a *app) loadAllAdapters() (Registry, error) {
	r, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		return nil, err
	}
	p, err := a.resolvePaths()
	if err != nil {
		return r, nil //nolint:nilerr // no home directory: the built-in adapters only
	}
	// Installed adapters change what signatures extract: re-extract when
	// they change.
	a.extractVersion = extractVersion(adapterfiles.Files, os.DirFS(p.Adapters))
	names, data, errs := localAdapters(p.Adapters)
	for i, n := range names {
		if err := r.AddLocal(n, data[i]); err != nil {
			errs = append(errs, err)
		}
	}
	for _, e := range errs {
		a.errf("ignoring installed adapter: %v", e)
	}
	return r, nil
}

// binaryPath is the path written into Claude Code hooks: the real binary,
// never npx (docs/decisions/0802-hook-binary-path.md).
func binaryPath() (string, error) {
	if p := os.Getenv("MURETTO_LAUNCHER_BINARY"); p != "" {
		return filepath.EvalSymlinks(p)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// binaryPathWarning explains when the hook path may disappear.
func binaryPathWarning(path string) string {
	switch {
	case strings.Contains(path, "/_npx/"):
		return "Muretto is running from the npx cache, which can be cleaned at any time; the hooks would then stop working. Install it with `npm i -g muretto`, Homebrew or install.sh, then run `muretto init` again."
	case strings.Contains(path, "/node_modules/"):
		return "Muretto is running from a project's node_modules; the hooks break if that folder is deleted. A global install (`npm i -g muretto`, Homebrew or install.sh) is safer."
	}
	return ""
}

// ---- output helpers ----

func (a *app) out(format string, args ...any) {
	fmt.Fprintf(a.stdout, "%s\n", redact.String(fmt.Sprintf(format, args...)))
}

func (a *app) errf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "muretto: %s\n", redact.String(fmt.Sprintf(format, args...)))
}

func (a *app) interactive() bool { return a.isTTY(a.stdin) }

func (a *app) prompt(q string) (string, error) {
	fmt.Fprint(a.stdout, q)
	if a.in == nil {
		a.in = bufio.NewReader(a.stdin)
	}
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (a *app) confirm(q string, def bool) bool {
	suffix := " [y/N] "
	if def {
		suffix = " [Y/n] "
	}
	ans, err := a.prompt(q + suffix)
	if err != nil {
		return false
	}
	switch strings.ToLower(ans) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

// ---- the pipeline every read command runs ----

type flags struct {
	json, refresh, noColor bool
}

type state struct {
	p        paths.Paths
	cfg      *config.Config
	st       *store.Store
	reg      Registry
	warnings []contract.Warning
	answer   contract.Answer
	in       answer.Input
}

func (s *state) close() {
	if s.st != nil {
		s.st.Close()
	}
}

func (a *app) warn(s *state, code, msg, fix string) {
	w := contract.Warning{Code: code, Message: clip(msg, 500)}
	if fix != "" {
		w.Fix = &fix
	}
	s.warnings = append(s.warnings, w)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// open resolves paths, loads config and opens the store, without ingest.
func (a *app) open(ctx context.Context) (*state, error) {
	p, err := a.resolvePaths()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the Muretto home directory: %w", err)
	}
	if err := os.MkdirAll(p.Home, 0o700); err != nil {
		return nil, err
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p.Config); errors.Is(err, fs.ErrNotExist) {
		if err := cfg.Save(p.Config); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(ctx, p.DB)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", p.DB, err)
	}
	return &state{p: p, cfg: cfg, st: st}, nil
}

// load runs ingest, a cached scan and answer.Compute.
func (a *app) load(ctx context.Context, f flags) (*state, error) {
	s, err := a.open(ctx)
	if err != nil {
		return nil, err
	}
	reg, err := a.loadRegistry()
	if err != nil {
		a.warn(s, "adapters_unavailable", "Provider adapters could not be loaded: "+err.Error(), "")
	} else {
		s.reg = reg
	}
	a.ingest(ctx, s)
	a.scan(ctx, s, f.refresh)
	if err := a.compute(ctx, s); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (a *app) toStoreActions(s *state, raws []observe.RawAction) []store.Action {
	out := make([]store.Action, 0, len(raws))
	for _, r := range raws {
		act := store.Action{Action: r.Action, Input: r.Input, Output: r.Output, MCP: r.MCP}
		if s.reg != nil {
			act.Action.Identifiers = s.reg.Extract(r)
			act.Extracted = true
		}
		out = append(out, act)
	}
	return out
}

func (a *app) ingest(ctx context.Context, s *state) {
	dev, salt := s.cfg.DeveloperID(), s.cfg.PathHashSalt()
	var batches []observe.Batch
	if b, err := a.ingestSpool(s.p, dev, salt); err != nil {
		a.warn(s, "spool_unreadable", "Could not read the hook spool: "+err.Error(), "muretto doctor")
	} else {
		batches = append(batches, b)
	}
	scannedTranscripts := false
	started := a.now()
	if dir, err := a.claudeDir(); err == nil {
		since, _ := s.st.MetaTime(ctx, store.MetaTranscriptsScannedAt)
		if tb, err := a.scanTranscripts(dir, dev, salt, since); err != nil {
			a.warn(s, "transcripts_unreadable", "Could not read Claude Code transcripts: "+err.Error(), "")
		} else {
			batches = append(batches, tb)
			scannedTranscripts = true
		}
	}
	// One merged batch, one transaction. Spool files are acked only after
	// the commit; a crash in between re-ingests them (SaveBatch is idempotent).
	m := observe.Merge(batches...)
	s.warnings = append(s.warnings, m.Warnings...)
	if err := s.st.SaveBatch(ctx, m.Sessions, a.toStoreActions(s, m.Actions), m.Ambiguous...); err != nil {
		a.warn(s, "store_error", "Could not store hook and transcript data: "+err.Error(), "")
	} else {
		if len(m.Files) > 0 {
			if err := a.ack(m.Files); err != nil {
				a.warn(s, "spool_ack_failed", "Could not remove processed spool files: "+err.Error(), "")
			}
		}
		if scannedTranscripts {
			_ = s.st.SetMeta(ctx, store.MetaTranscriptsScannedAt, started.Format(time.RFC3339Nano))
		}
	}

	// Re-run adapter signatures on actions stored before the adapters
	// loaded, or with an older binary/adapter set.
	if s.reg == nil {
		return
	}
	prev, _, _ := s.st.Meta(ctx, store.MetaExtractVersion)
	raws, err := s.st.RawActions(ctx, prev == a.extractVersion)
	if err != nil {
		return
	}
	ids := map[string][]contract.Identifier{}
	for _, r := range raws {
		ids[r.Action.ID] = s.reg.Extract(observe.RawAction{Action: r.Action, Input: r.Input, Output: r.Output, MCP: observe.MCPParams(r.MCP)})
	}
	if len(ids) > 0 {
		if err := s.st.SetIdentifiers(ctx, ids); err != nil {
			return
		}
	}
	_ = s.st.SetMeta(ctx, store.MetaExtractVersion, a.extractVersion)
}

func (a *app) scan(ctx context.Context, s *state, refresh bool) {
	if s.reg == nil {
		return
	}
	now := a.now()
	for _, acct := range s.cfg.Accounts {
		last, _ := s.st.MetaTime(ctx, store.ScanKey(acct.Provider, acct.ID))
		if !refresh && !last.IsZero() && now.Sub(last) < scanTTL {
			continue
		}
		label := acct.Provider + " account " + acctLabel(acct)
		token, _, err := a.secrets.Provider(acct.Provider, acct.ID)
		if err != nil {
			msg := "No token for " + label + ": " + err.Error()
			if errors.Is(err, secrets.ErrUnavailable) {
				msg += ". " + secrets.RefuseMessage(secrets.ProviderEnv(acct.Provider))
			}
			a.warn(s, "token_missing", msg, "muretto init")
			_ = s.st.RecordCheckFailure(ctx, acct.Provider, acct.ID, now, contract.ProviderCheckErrorCodeAuth, "no token available")
			continue
		}
		b := a.broker(s.reg)
		sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		res, err := s.reg.Scan(sctx, b, acct.Provider, adapters.Credentials{
			Token: token, AccountID: acct.ID, AccountName: acct.Name, AccountSlug: acct.Slug,
			// Opt-ins chosen in `muretto init`; adapters ignore keys they don't declare.
			Options: map[string]bool{"catalog_rls": s.cfg.Options.SupabaseCatalogRead},
		})
		cancel()
		if err != nil {
			a.warn(s, "scan_failed", "Scan of "+label+" failed: "+err.Error()+". Showing the last data Muretto has, with its age.", "muretto doctor")
			_ = s.st.RecordCheckFailure(ctx, acct.Provider, acct.ID, now, contract.ProviderCheckErrorCodeProvider, clip(redact.String(err.Error()), 500))
			continue
		}
		if err := s.st.ApplyScan(ctx, acct.Provider, acct.ID, store.Scan(res), now); err != nil {
			a.warn(s, "store_error", "Could not store the scan of "+label+": "+err.Error(), "")
		}
	}
}

// broker returns a read-only broker for reg's allowlist.
func (a *app) broker(reg Registry) *broker.Broker {
	if a.newBroker != nil {
		return a.newBroker(reg.Allowlist())
	}
	return broker.New(nil, reg.Allowlist())
}

func acctLabel(acct config.Account) string {
	if acct.Name != "" {
		return acct.Name
	}
	return acct.ID
}

func (a *app) developer(cfg *config.Config) contract.Developer {
	d := contract.Developer{ID: cfg.DeveloperID(), DisplayName: cfg.Developer.DisplayName, MachineIDHash: cfg.MachineIDHash()}
	if d.DisplayName == "" {
		d.DisplayName = "me"
	}
	if cfg.Developer.AccountLogin != "" {
		l := cfg.Developer.AccountLogin
		d.AccountLogin = &l
	}
	return d
}

func (a *app) compute(ctx context.Context, s *state) error {
	in := answer.Input{
		Developer:      a.developer(s.cfg),
		DeveloperNames: map[string]string{s.cfg.DeveloperID(): s.cfg.Developer.DisplayName},
		ReviewAfter:    s.cfg.ReviewAfter(),
		Accounts:       len(s.cfg.Accounts),
		Warnings:       s.warnings,
	}
	var err error
	if in.Sessions, err = s.st.Sessions(ctx); err != nil {
		return err
	}
	if in.Actions, err = s.st.Actions(ctx); err != nil {
		return err
	}
	if in.Resources, err = s.st.Resources(ctx); err != nil {
		return err
	}
	if in.Findings, err = s.st.Findings(ctx); err != nil {
		return err
	}
	if in.Costs, err = s.st.Costs(ctx); err != nil {
		return err
	}
	if in.Checks, err = s.st.Checks(ctx); err != nil {
		return err
	}
	if in.Reviews, err = s.st.Reviews(ctx); err != nil {
		return err
	}
	if in.AmbiguousActions, err = s.st.AmbiguousActions(ctx); err != nil {
		return err
	}
	if exe, err := a.executable(); err == nil {
		if hs, err := a.hooks.Inspect(s.p, exe); err == nil {
			in.Hooks = &answer.HookState{Installed: hs.Installed, HooksDisabled: hs.HooksDisabled, ManagedOnly: hs.ManagedOnly}
		}
	}
	s.in = in
	s.answer = answer.Compute(in, a.now())
	return nil
}

// syncAfter pushes the full Answer when sync is on. It never fails the command.
func (a *app) syncAfter(ctx context.Context, s *state) {
	if !s.cfg.Sync.Enabled {
		return
	}
	if err := a.push(ctx, s); err != nil {
		a.errf("sync: %v (the next command will retry)", err)
	}
}

func (a *app) push(ctx context.Context, s *state) error {
	server := s.cfg.SyncServer()
	token, _, err := a.secrets.Sync(hostOf(server))
	if err != nil {
		return fmt.Errorf("not logged in (%w); run `muretto login`", err)
	}
	revs, err := s.st.SyncRevs(ctx)
	if err != nil {
		return err
	}
	// The revisions say what this server already has. A different server
	// starts from nothing, so it gets everything, not just what changed.
	if sent, _, _ := s.st.Meta(ctx, syncRevsServer); sent != server {
		revs = map[string]string{}
	}
	batch, newRevs := a.sync.BuildBatch(s.answer, version, revs)
	if batchEmpty(batch) {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := a.sync.Push(pctx, server, token, batch); err != nil {
		return err
	}
	if err := s.st.SetSyncRevs(ctx, newRevs); err != nil {
		return err
	}
	if err := s.st.SetMeta(ctx, syncRevsServer, server); err != nil {
		return err
	}
	return s.st.SetMeta(ctx, "sync_pushed_at", a.now().Format(time.RFC3339Nano))
}

// syncRevsServer names the server the stored sync revisions were pushed to.
const syncRevsServer = "sync_revs_server"

func batchEmpty(b contract.SyncBatch) bool {
	return len(b.Sessions)+len(b.Resources)+len(b.Attributions)+len(b.Findings)+len(b.Costs)+len(b.Reviews)+len(b.Checks) == 0
}

func hostOf(server string) string {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return server
	}
	return u.Host
}

func (a *app) renderOpts(f flags, w io.Writer) render.Options {
	return render.Options{
		Color: render.ColorEnabled(a.isTTY(w), a.getenv, f.json, f.noColor),
		Now:   a.now(),
		Loc:   time.Local,
	}
}

// ---- argument resolution ----

func resolveSession(an contract.Answer, arg string) (contract.SessionView, error) {
	var exact, prefix, titled []contract.SessionView
	for _, v := range an.Sessions {
		id := v.Session.ID
		switch {
		case id == arg:
			exact = append(exact, v)
		case strings.HasPrefix(id, arg):
			prefix = append(prefix, v)
		case v.Session.TaskTitle != nil && strings.EqualFold(*v.Session.TaskTitle, arg):
			titled = append(titled, v)
		}
	}
	for _, set := range [][]contract.SessionView{exact, prefix, titled} {
		if len(set) == 1 {
			return set[0], nil
		}
		if len(set) > 1 {
			var c []string
			for _, v := range set {
				c = append(c, v.Session.ID)
			}
			return contract.SessionView{}, fmt.Errorf("%q matches %d sessions: %s; use a longer prefix", arg, len(set), strings.Join(c, ", "))
		}
	}
	return contract.SessionView{}, fmt.Errorf("no session matches %q (see `muretto sessions`)", arg)
}

func resolveResource(an contract.Answer, arg string) (contract.Resource, error) {
	tiers := make([][]contract.Resource, 4)
	for _, r := range an.Resources {
		switch {
		case r.ID == arg || r.ProviderID == arg:
			tiers[0] = append(tiers[0], r)
		case render.DisplayName(r) == arg || r.Name == arg:
			tiers[1] = append(tiers[1], r)
		case strings.EqualFold(render.DisplayName(r), arg) || strings.EqualFold(r.Name, arg):
			tiers[2] = append(tiers[2], r)
		case strings.HasPrefix(r.ID, arg) || strings.HasPrefix(r.ProviderID, arg) || strings.HasPrefix(render.DisplayName(r), arg):
			tiers[3] = append(tiers[3], r)
		}
	}
	for _, set := range tiers {
		if len(set) == 1 {
			return set[0], nil
		}
		if len(set) > 1 {
			var c []string
			for _, r := range set {
				c = append(c, r.ID)
			}
			sort.Strings(c)
			return contract.Resource{}, fmt.Errorf("%q matches %d resources:\n  %s\nuse the full id", arg, len(set), strings.Join(c, "\n  "))
		}
	}
	return contract.Resource{}, fmt.Errorf("no resource matches %q (see `muretto resources`)", arg)
}
