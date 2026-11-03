package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/answer"
	"github.com/tolgayayci/muretto/internal/config"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/render"
	"github.com/tolgayayci/muretto/internal/secrets"
)

const catalogWarning = `Optional: list every Supabase table.
  Without this, Muretto sees tables that sessions created and tables the
  security advisor flags. Listing all tables needs the "Database (Read)"
  permission on your token, and that permission can read every row of every
  table. Muretto only runs one fixed catalog query, but the token could do more.`

func (a *app) cmdInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	name := fs.String("name", "", "your display name (default: your OS user name)")
	noHooks := fs.Bool("no-hooks", false, "do not add Claude Code hooks")
	yes := fs.Bool("yes", false, "accept defaults without prompting")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("init takes no arguments")
	}
	s, err := a.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	interactive := a.interactive() && !*yes
	if *name != "" {
		s.cfg.Developer.DisplayName = *name
	}

	a.out("Muretto is read-only: it never creates, changes or deletes anything in your accounts.\n")

	// 1. Claude Code and hooks.
	dir, dirErr := a.claudeDir()
	_, statErr := os.Stat(dir)
	_, pathErr := exec.LookPath("claude")
	switch {
	case dirErr == nil && statErr == nil:
		a.out("Claude Code: found (%s)", dir)
	case pathErr == nil:
		a.out("Claude Code: found on PATH")
	default:
		a.out("Claude Code: not found. Muretto adds its hooks anyway; they start working once Claude Code is installed.")
	}
	if !*noHooks {
		exe, err := a.executable()
		if err != nil {
			return fmt.Errorf("cannot find the muretto binary path: %w", err)
		}
		if w := binaryPathWarning(exe); w != "" {
			a.out("Warning: %s", w)
		}
		st, err := a.hooks.Install(s.p, exe)
		if err != nil {
			a.out("Hooks: not installed (%v). Sessions will still be read from transcripts.", err)
		} else {
			a.out("Hooks: added to %s (a backup is in %s)", st.SettingsPath, s.p.Backups)
			if st.HooksDisabled {
				a.out("  Note: disableAllHooks is set, so no hooks run until you remove it.")
			}
			if st.ManagedOnly {
				a.out("  Note: only managed hooks are allowed here; ask your admin to add Muretto's hooks.")
			}
		}
	}

	// 2. Providers.
	reg, err := a.loadRegistry()
	if err != nil {
		a.out("\nProvider adapters are not available (%v); skipping provider setup.", err)
	} else {
		for _, p := range reg.Providers() {
			if err := a.connectProvider(ctx, s, reg, p, interactive); err != nil {
				if errors.Is(err, context.Canceled) {
					return err
				}
				a.out("  %s: %v", p.DisplayName, err)
			}
		}
	}
	if err := s.cfg.Save(s.p.Config); err != nil {
		return err
	}

	a.out("\nNext steps:")
	a.out("  1. Use Claude Code as usual. Muretto's hooks record what sessions create.")
	a.out("  2. Run `muretto` for a summary, or `muretto sessions`.")
	a.out("  3. Optional: `muretto login` and `muretto sync on` for the dashboard.")
	return nil
}

func (a *app) connectProvider(ctx context.Context, s *state, reg Registry, p adapters.ProviderInfo, interactive bool) error {
	a.out("\n%s", p.DisplayName)
	envName := secrets.ProviderEnv(p.Name)
	token := strings.TrimSpace(a.getenv(envName))
	fromEnv := token != ""
	switch {
	case fromEnv:
		a.out("  Using %s from the environment (not stored).", envName)
	case !interactive:
		a.out("  Skipped (no terminal). Set %s and run `muretto init` again to connect.", envName)
		return nil
	default:
		if !a.confirm("  Connect "+p.DisplayName+"?", true) {
			return nil
		}
		if p.TokenHelp != "" {
			for _, l := range strings.Split(strings.TrimSpace(p.TokenHelp), "\n") {
				a.out("  %s", l)
			}
		}
		fmt.Fprint(a.stdout, "  Paste the token (input hidden): ")
		t, err := a.readSecret(ctx)
		fmt.Fprintln(a.stdout)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(t)
		if token == "" {
			return errors.New("no token entered; skipped")
		}
	}

	var accounts []adapters.Account
	if p.AccountPrompt != "" {
		// The adapter cannot list accounts: the user names the one to scan.
		acctEnv := strings.TrimSuffix(envName, "_TOKEN") + "_ACCOUNT"
		id := strings.TrimSpace(a.getenv(acctEnv))
		if id == "" && interactive {
			id, _ = a.prompt("  " + p.AccountPrompt + ": ")
			id = strings.TrimSpace(id)
		}
		if id == "" {
			return fmt.Errorf("no account entered; set %s or run `muretto init` in a terminal", acctEnv)
		}
		accounts = []adapters.Account{{ID: id, Name: id, Slug: id}}
	} else {
		b := a.broker(reg)
		actx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var err error
		accounts, err = reg.Accounts(actx, b, p.Name, token)
		cancel()
		if err != nil {
			return fmt.Errorf("could not list accounts with this token: %w", err)
		}
	}
	if len(accounts) == 0 {
		return errors.New("this token can see no accounts")
	}
	chosen := accounts
	if len(accounts) > 1 && interactive {
		for i, acct := range accounts {
			a.out("  %d. %s (%s)", i+1, acct.Name, acct.ID)
		}
		ans, _ := a.prompt("  Which accounts? (e.g. 1,3 or all) [all] ")
		if ans != "" && ans != "all" {
			chosen = nil
			for _, part := range strings.Split(ans, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil || n < 1 || n > len(accounts) {
					return fmt.Errorf("not a choice: %q", part)
				}
				chosen = append(chosen, accounts[n-1])
			}
		}
	}
	for _, acct := range chosen {
		if !fromEnv {
			if err := a.secrets.Set(secrets.ProviderKey(p.Name, acct.ID), token); err != nil {
				if errors.Is(err, secrets.ErrUnavailable) {
					return errors.New(secrets.RefuseMessage(envName))
				}
				return fmt.Errorf("could not store the token in the keychain: %w", err)
			}
		}
		s.cfg.AddAccount(config.Account{Provider: p.Name, ID: acct.ID, Name: acct.Name, Slug: acct.Slug, ConnectedAt: a.now()})
		where := "keychain"
		if fromEnv {
			where = envName
		}
		a.out("  Connected %s (%s); token in %s.", acct.Name, acct.ID, where)
	}
	if p.Name == "supabase" && interactive {
		a.out("")
		for _, l := range strings.Split(catalogWarning, "\n") {
			a.out("  %s", l)
		}
		s.cfg.Options.SupabaseCatalogRead = a.confirm("  Enable full table listing?", false)
	}
	return nil
}

// readSecret reads a hidden line; Ctrl-C restores the terminal and cancels.
func (a *app) readSecret(ctx context.Context) (string, error) {
	fd := int(os.Stdin.Fd()) // #nosec G115 -- fd fits in int
	var restore func()
	if f, ok := a.stdin.(*os.File); ok && f == os.Stdin && term.IsTerminal(fd) {
		if st, err := term.GetState(fd); err == nil {
			restore = func() { _ = term.Restore(fd, st) }
		}
	}
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := a.readPassword()
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-ctx.Done():
		if restore != nil {
			restore()
		}
		return "", ctx.Err()
	}
}

// ---- login, logout, sync ----

func (a *app) cmdLogin(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usageErr("login takes no arguments")
	}
	s, err := a.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	server := s.cfg.SyncServer()
	// Check the keychain first, so an approval is never spent on a token
	// that can't be kept.
	if err := a.secrets.Check(); err != nil {
		return errors.New(secrets.LoginRefuseMessage())
	}
	token, err := a.sync.Login(ctx, server, a.stdout)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	if err := a.secrets.Set(secrets.SyncKey(hostOf(server)), token); err != nil {
		// Nothing holds the token now, so don't leave it live on the server.
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = a.sync.Revoke(rctx, server, token)
		cancel()
		if errors.Is(err, secrets.ErrUnavailable) {
			return errors.New(secrets.LoginRefuseMessage())
		}
		return err
	}
	// A new login may be another account: its server has none of this yet.
	if err := s.st.SetSyncRevs(ctx, nil); err != nil {
		return err
	}
	a.out("Logged in to %s. Sync is %s; `muretto sync on` shows exactly what would be sent.", server, onOff(s.cfg.Sync.Enabled))
	return nil
}

func (a *app) cmdLogout(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usageErr("logout takes no arguments")
	}
	s, err := a.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	server := s.cfg.SyncServer()
	if token, _, err := a.secrets.Sync(hostOf(server)); err == nil {
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := a.sync.Revoke(rctx, server, token); err != nil {
			a.out("Could not revoke the token on the server (%v); it was removed locally.", err)
		}
		cancel()
	}
	if err := a.secrets.Delete(secrets.SyncKey(hostOf(server))); err != nil && !errors.Is(err, secrets.ErrUnavailable) {
		return err
	}
	s.cfg.Sync.Enabled = false
	s.cfg.Developer.AccountLogin = ""
	if err := s.cfg.Save(s.p.Config); err != nil {
		return err
	}
	a.out("Logged out. Sync is off. Data already on the server stays there.")
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (a *app) cmdSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "enable without the confirmation prompt")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("sync needs one of: on, off, status, now")
	}
	switch pos[0] {
	case "on":
		s, err := a.open(ctx)
		if err != nil {
			return err
		}
		defer s.close()
		server := s.cfg.SyncServer()
		a.out("Sync uploads a projection of what `muretto --json` shows to %s.", server)
		a.out("It never sends tokens, raw transcripts, full prompts, local paths or secret values.\n")
		a.out("Exactly these fields are sent:")
		for _, f := range a.sync.Payload() {
			line := fmt.Sprintf("  %-14s %-22s", f.Entity, f.Field)
			if f.Note != "" {
				line += " " + f.Note
			}
			a.out("%s", strings.TrimRight(line, " "))
		}
		if len(a.sync.Payload()) == 0 {
			a.out("  (the field list is not available in this build; nothing will be enabled)")
			return errors.New("sync payload description unavailable")
		}
		a.out("")
		if _, _, err := a.secrets.Sync(hostOf(server)); err != nil {
			return errors.New("not logged in; run `muretto login` first")
		}
		if !*yes {
			if !a.interactive() {
				return usageErr("run `muretto sync on --yes` to confirm without a terminal")
			}
			if !a.confirm("Enable sync?", false) {
				a.out("Sync stays off.")
				return nil
			}
		}
		now := a.now()
		s.cfg.Sync.Enabled, s.cfg.Sync.EnabledAt = true, &now
		if err := s.cfg.Save(s.p.Config); err != nil {
			return err
		}
		a.out("Sync is on. Every muretto command now pushes changes; `muretto sync off` stops it.")
		return nil
	case "off":
		s, err := a.open(ctx)
		if err != nil {
			return err
		}
		defer s.close()
		s.cfg.Sync.Enabled, s.cfg.Sync.EnabledAt = false, nil
		if err := s.cfg.Save(s.p.Config); err != nil {
			return err
		}
		a.out("Sync is off. Nothing more will be sent.")
		return nil
	case "status":
		s, err := a.open(ctx)
		if err != nil {
			return err
		}
		defer s.close()
		server := s.cfg.SyncServer()
		_, src, terr := a.secrets.Sync(hostOf(server))
		login := "not logged in"
		if terr == nil {
			login = "logged in (token from " + string(src) + ")"
		}
		pushed, _ := s.st.MetaTime(ctx, "sync_pushed_at")
		revs, _ := s.st.SyncRevs(ctx)
		a.out("Sync: %s · %s · %s", onOff(s.cfg.Sync.Enabled), server, login)
		a.out("Last push: %s · %d entities synced", since(a.now(), pushed), len(revs))
		return nil
	case "now":
		s, err := a.load(ctx, flags{})
		if err != nil {
			return err
		}
		defer s.close()
		if err := a.push(ctx, s); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		a.out("Synced.")
		return nil
	}
	return usageErr("sync needs one of: on, off, status, now")
}

// ---- doctor ----

type doctorItem struct {
	OK     bool   `json:"ok"`
	Check  string `json:"check"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func (a *app) cmdDoctor(ctx context.Context, args []string) error {
	var f flags
	fs := readFlags("doctor", &f)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("doctor takes no arguments")
	}
	var items []doctorItem
	add := func(ok bool, check, detail, hint string) {
		items = append(items, doctorItem{ok, check, detail, hint})
	}
	s, err := a.load(ctx, f)
	if err != nil {
		return err
	}
	defer s.close()

	if st, err := os.Stat(s.p.Home); err == nil {
		perm := st.Mode().Perm()
		add(perm&0o077 == 0, "home", fmt.Sprintf("%s (%v)", s.p.Home, perm), "chmod 700 "+s.p.Home)
	}
	exe, exeErr := a.executable()
	if exeErr == nil {
		if w := binaryPathWarning(exe); w != "" {
			add(false, "binary", exe, w)
		} else {
			add(true, "binary", exe, "")
		}
		hs, err := a.hooks.Inspect(s.p, exe)
		switch {
		case err != nil:
			add(false, "hooks", "could not inspect Claude Code settings: "+err.Error(), "muretto init")
		case !hs.Installed && answer.HookEventsSeen(s.in.Sessions, a.now()):
			// Registered another way (claude --settings, a plugin, managed
			// settings): events arrive, but a plain `claude` may not run them.
			add(true, "hooks", "not in "+hs.SettingsPath+", but hook events arrived in the last 7 days (registered elsewhere, e.g. claude --settings)", "")
		case !hs.Installed:
			add(false, "hooks", "not installed in "+hs.SettingsPath, "muretto init")
		case hs.HooksDisabled:
			add(false, "hooks", "installed, but disableAllHooks is set", "remove disableAllHooks from "+hs.SettingsPath)
		case hs.ManagedOnly:
			add(false, "hooks", "installed, but only managed hooks may run", "ask your admin to add Muretto's hooks to managed settings")
		default:
			add(true, "hooks", "installed in "+hs.SettingsPath+" ("+strings.Join(hs.Events, ", ")+")", "")
		}
	}

	if entries, err := os.ReadDir(s.p.Spool); err == nil {
		n := 0
		var oldest time.Time
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			n++
			if info, err := e.Info(); err == nil && (oldest.IsZero() || info.ModTime().Before(oldest)) {
				oldest = info.ModTime()
			}
		}
		ok := n == 0 || a.now().Sub(oldest) < time.Hour
		add(ok, "spool", fmt.Sprintf("%d unprocessed files (oldest %s)", n, since(a.now(), oldest)), "files that stay are unreadable; see "+s.p.HookLog)
	} else {
		add(true, "spool", "empty", "")
	}
	if info, err := os.Stat(s.p.HookLog); err == nil && info.Size() > 0 {
		recent := a.now().Sub(info.ModTime()) < 24*time.Hour
		add(!recent, "hook errors", fmt.Sprintf("%s (%d bytes, last written %s)", s.p.HookLog, info.Size(), since(a.now(), info.ModTime())), "read the log; hooks never block Claude Code, but events may be missing")
	} else {
		add(true, "hook errors", "none", "")
	}

	if len(s.cfg.Accounts) == 0 {
		add(false, "providers", "no accounts connected", "muretto init")
	}
	for _, acct := range s.cfg.Accounts {
		label := acct.Provider + " " + acctLabel(acct)
		token, src, err := a.secrets.Provider(acct.Provider, acct.ID)
		if err != nil {
			add(false, label+" token", "none: "+err.Error(), "muretto init, or set "+secrets.ProviderEnv(acct.Provider))
			continue
		}
		detail := "from " + string(src)
		if s.reg != nil {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			accts, err := s.reg.Accounts(cctx, a.broker(s.reg), acct.Provider, token)
			cancel()
			valid := false
			for _, x := range accts {
				valid = valid || x.ID == acct.ID
			}
			if err != nil {
				add(false, label+" token", detail+", rejected: "+err.Error(), "create a new token and run muretto init")
				continue
			}
			if !valid {
				add(false, label+" token", detail+", but it cannot see this account", "muretto init")
				continue
			}
			detail += ", valid"
		}
		add(true, label+" token", detail, "")
		var last *time.Time
		failed := 0
		for _, c := range s.answer.Checks {
			if c.Provider != acct.Provider || c.AccountID != acct.ID {
				continue
			}
			if c.Status == contract.ProviderCheckStatusError {
				failed++
			}
			if c.LastSuccessAt != nil && (last == nil || c.LastSuccessAt.Before(*last)) {
				t := *c.LastSuccessAt
				last = &t
			}
		}
		if last == nil {
			add(false, label+" checks", "no successful check yet", "muretto --refresh")
		} else {
			add(failed == 0, label+" checks", fmt.Sprintf("last success %s, %d failing", since(a.now(), *last), failed), "see the warnings below")
		}
	}

	server := s.cfg.SyncServer()
	if s.cfg.Sync.Enabled {
		_, _, err := a.secrets.Sync(hostOf(server))
		add(err == nil, "sync", "on ("+server+")", "muretto login")
	} else {
		add(true, "sync", "off", "")
	}

	if f.json {
		return render.JSON(a.stdout, struct {
			Items    []doctorItem       `json:"items"`
			Warnings []contract.Warning `json:"warnings"`
		}{items, s.answer.Warnings})
	}
	o := a.renderOpts(f, a.stdout)
	for _, it := range items {
		mark := "✓"
		if !it.OK {
			mark = "✗"
		}
		a.out("%s %-18s %s", mark, it.Check, it.Detail)
		if !it.OK && it.Hint != "" {
			a.out("  → %s", it.Hint)
		}
	}
	return render.Warnings(a.stdout, s.answer, o)
}

// ---- uninstall ----

func (a *app) cmdUninstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete stored tokens and the local data directory")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("uninstall takes no arguments")
	}
	s, err := a.open(ctx)
	if err != nil {
		return err
	}
	st, err := a.hooks.Uninstall(s.p)
	if err != nil {
		s.close()
		return fmt.Errorf("could not remove the hooks: %w", err)
	}
	a.out("Removed Muretto's hooks from %s (only what `muretto init` added).", st.SettingsPath)
	if !*purge {
		s.close()
		a.out("Your local data stays in %s and tokens stay in the keychain.", s.p.Home)
		a.out("Run `muretto uninstall --purge` to delete them too.")
		return nil
	}
	if !*yes && (!a.interactive() || !a.confirm("Delete all tokens and "+s.p.Home+"?", false)) {
		s.close()
		a.out("Kept tokens and local data.")
		return nil
	}
	for _, acct := range s.cfg.Accounts {
		if err := a.secrets.Delete(secrets.ProviderKey(acct.Provider, acct.ID)); err != nil {
			a.out("Could not delete the %s token: %v", acct.Provider, err)
		}
	}
	_ = a.secrets.Delete(secrets.SyncKey(hostOf(s.cfg.SyncServer())))
	s.close()
	if err := removeHome(s.p.Home); err != nil {
		return err
	}
	a.out("Deleted tokens and %s.", s.p.Home)
	return nil
}

// removeHome deletes the Muretto home, refusing paths that do not look like one.
func removeHome(home string) error {
	clean := filepath.Clean(home)
	if clean == "/" || clean == "." || filepath.Dir(clean) == clean {
		return fmt.Errorf("refusing to delete %q", home)
	}
	if _, err := os.Stat(filepath.Join(clean, "muretto.db")); err != nil {
		if _, err2 := os.Stat(filepath.Join(clean, "config.json")); err2 != nil {
			return fmt.Errorf("refusing to delete %q: it does not look like a Muretto home", home)
		}
	}
	return os.RemoveAll(clean)
}
