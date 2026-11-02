package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/answer"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/render"
)

const usage = `muretto: see what your AI coding agents created outside the repo. Read-only.

Usage:
  muretto                      summary: agent-created, exposed, needs review, recent sessions
  muretto init                 add Claude Code hooks and connect Supabase / Vercel
  muretto sessions             recent agent sessions
  muretto session <id>         everything one session created
  muretto resources [--provider p] [--exposed] [--unknown]
  muretto show <resource>      one resource: timeline, creator, exposure, cost, review
  muretto why <resource>       the evidence behind its attribution label
  muretto exposed              exposure findings, worst first
  muretto fix <resource>       print the command or SQL to fix it (never runs it)
  muretto review [<resource> keep|not-needed]
  muretto providers [--coverage]
  muretto adapter new <openapi-file>   draft an adapter for any API from its OpenAPI spec
  muretto adapter check|install|list|remove
  muretto login | logout       optional dashboard login
  muretto sync on|off|status|now
  muretto doctor               check hooks, tokens, spool and last checks
  muretto uninstall [--purge]  remove exactly what muretto init added
  muretto version

Read commands accept --json, --refresh (ignore the 60 s scan cache) and --no-color.
Session and resource arguments accept unique prefixes and names.
`

// errUsage marks errors that should print a usage hint and exit 2.
var errUsage = errors.New("usage")

func usageErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

func (a *app) run(ctx context.Context, args []string) int {
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "", "summary":
		err = a.readCmd(ctx, "summary", args, 0, a.cmdSummary)
	case "sessions":
		err = a.readCmd(ctx, cmd, args, 0, a.cmdSessions)
	case "session":
		err = a.readCmd(ctx, cmd, args, 1, a.cmdSession)
	case "resources":
		err = a.cmdResources(ctx, args)
	case "show":
		err = a.readCmd(ctx, cmd, args, 1, a.cmdShow)
	case "why":
		err = a.readCmd(ctx, cmd, args, 1, a.cmdWhy)
	case "exposed":
		err = a.readCmd(ctx, cmd, args, 0, a.cmdExposed)
	case "fix":
		err = a.readCmd(ctx, cmd, args, 1, a.cmdFix)
	case "review":
		err = a.cmdReview(ctx, args)
	case "providers":
		err = a.cmdProviders(ctx, args)
	case "adapter", "adapters":
		err = a.cmdAdapter(ctx, args)
	case "init":
		err = a.cmdInit(ctx, args)
	case "login":
		err = a.cmdLogin(ctx, args)
	case "logout":
		err = a.cmdLogout(ctx, args)
	case "sync":
		err = a.cmdSync(ctx, args)
	case "doctor":
		err = a.cmdDoctor(ctx, args)
	case "uninstall":
		err = a.cmdUninstall(ctx, args)
	case "version", "--version":
		a.out("muretto %s (commit %s, built %s)", version, commit, date)
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
	default:
		err = usageErr("unknown command %q", cmd)
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(a.stdout, usage)
		return 0
	case errors.Is(err, errUsage):
		a.errf("%s", strings.TrimPrefix(err.Error(), "usage: "))
		fmt.Fprint(a.stderr, "Run `muretto help` for usage.\n")
		return 2
	case errors.Is(err, context.Canceled):
		return 130
	default:
		a.errf("%v", err)
		return 1
	}
}

// parse parses flags that may appear before, between or after positional args.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageErr("%v", err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func readFlags(name string, f *flags) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.BoolVar(&f.json, "json", false, "print the Answer as JSON")
	fs.BoolVar(&f.refresh, "refresh", false, "ignore the 60 s scan cache")
	fs.BoolVar(&f.noColor, "no-color", false, "disable color")
	return fs
}

type readFunc func(ctx context.Context, s *state, f flags, pos []string) error

func (a *app) readCmd(ctx context.Context, name string, args []string, nargs int, fn readFunc) error {
	var f flags
	pos, err := parse(readFlags(name, &f), args)
	if err != nil {
		return err
	}
	if len(pos) != nargs {
		if nargs == 0 {
			return usageErr("%s takes no arguments", name)
		}
		return usageErr("%s needs exactly one argument", name)
	}
	return a.withState(ctx, f, func(s *state) error { return fn(ctx, s, f, pos) })
}

func (a *app) withState(ctx context.Context, f flags, fn func(*state) error) error {
	s, err := a.load(ctx, f)
	if err != nil {
		return err
	}
	defer s.close()
	if err := fn(s); err != nil {
		return err
	}
	a.syncAfter(ctx, s)
	return nil
}

func (a *app) emit(f flags, an contract.Answer, text func(io.Writer, render.Options) error) error {
	if f.json {
		return render.JSON(a.stdout, an)
	}
	return text(a.stdout, a.renderOpts(f, a.stdout))
}

func all(contract.Resource) bool           { return true }
func noSessions(contract.SessionView) bool { return false }

// sessionSubset is the session plus what it created or claimed.
func sessionSubset(an contract.Answer, id string) contract.Answer {
	keep := map[string]bool{}
	for _, at := range an.Attributions {
		if at.SessionID != nil && *at.SessionID == id {
			keep[at.ResourceID] = true
			continue
		}
		if at.Label == contract.AttributionLabelUnknown && len(at.Evidence) > 0 {
			only := true
			for _, e := range at.Evidence {
				only = only && e.SessionID == id
			}
			if only {
				keep[at.ResourceID] = true
			}
		}
	}
	// Parents come along so headers can name the project they belong to.
	for _, r := range an.Resources {
		if keep[r.ID] && !(r.SeenVia == contract.ResourceSeenViaEvidence || exactFor(an, r.ID, id)) {
			delete(keep, r.ID)
		}
	}
	withParents := map[string]bool{}
	for _, r := range an.Resources {
		if keep[r.ID] {
			withParents[r.ID] = true
			if r.ParentID != nil {
				withParents[*r.ParentID] = true
			}
		}
	}
	return answer.Subset(an,
		func(r contract.Resource) bool { return withParents[r.ID] },
		func(v contract.SessionView) bool { return v.Session.ID == id })
}

func exactFor(an contract.Answer, resID, sessID string) bool {
	for _, at := range an.Attributions {
		if at.ResourceID == resID {
			return at.Label == contract.AttributionLabelExact && at.SessionID != nil && *at.SessionID == sessID
		}
	}
	return false
}

// resourceSubset is one resource plus its creating session (and parent, for names).
func resourceSubset(an contract.Answer, r contract.Resource) contract.Answer {
	sess := map[string]bool{}
	for _, at := range an.Attributions {
		if at.ResourceID == r.ID {
			for _, e := range at.Evidence {
				sess[e.SessionID] = true
			}
		}
	}
	return answer.Subset(an,
		func(x contract.Resource) bool { return x.ID == r.ID || (r.ParentID != nil && x.ID == *r.ParentID) },
		func(v contract.SessionView) bool { return sess[v.Session.ID] })
}

func (a *app) cmdSummary(_ context.Context, s *state, f flags, _ []string) error {
	return a.emit(f, s.answer, func(w io.Writer, o render.Options) error { return render.Summary(w, s.answer, o) })
}

func (a *app) cmdSessions(_ context.Context, s *state, f flags, _ []string) error {
	return a.emit(f, s.answer, func(w io.Writer, o render.Options) error { return render.Sessions(w, s.answer, o) })
}

func (a *app) cmdSession(_ context.Context, s *state, f flags, pos []string) error {
	v, err := resolveSession(s.answer, pos[0])
	if err != nil {
		return err
	}
	sub := sessionSubset(s.answer, v.Session.ID)
	return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Session(w, sub, v.Session.ID, o) })
}

func (a *app) cmdResources(ctx context.Context, args []string) error {
	var f flags
	var provider string
	var exposedOnly, unknownOnly bool
	fs := readFlags("resources", &f)
	fs.StringVar(&provider, "provider", "", "only this provider")
	fs.BoolVar(&exposedOnly, "exposed", false, "only exposed resources")
	fs.BoolVar(&unknownOnly, "unknown", false, "only resources with creator unknown")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("resources takes no arguments")
	}
	return a.withState(ctx, f, func(s *state) error {
		exposed := map[string]bool{}
		for _, fd := range s.answer.Findings {
			if fd.State == contract.ExposureFindingStateExposed {
				exposed[fd.ResourceID] = true
			}
		}
		label := map[string]contract.AttributionLabel{}
		sessOf := map[string]bool{}
		for _, at := range s.answer.Attributions {
			label[at.ResourceID] = at.Label
		}
		keep := func(r contract.Resource) bool {
			if provider != "" && r.Provider != provider {
				return false
			}
			if exposedOnly && !exposed[r.ID] {
				return false
			}
			if unknownOnly && label[r.ID] != contract.AttributionLabelUnknown {
				return false
			}
			return true
		}
		for _, at := range s.answer.Attributions {
			for _, r := range s.answer.Resources {
				if r.ID == at.ResourceID && keep(r) && at.SessionID != nil {
					sessOf[*at.SessionID] = true
				}
			}
			for _, e := range at.Evidence {
				for _, r := range s.answer.Resources {
					if r.ID == at.ResourceID && keep(r) {
						sessOf[e.SessionID] = true
					}
				}
			}
		}
		sub := answer.Subset(s.answer, keep, func(v contract.SessionView) bool { return sessOf[v.Session.ID] })
		return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Resources(w, sub, o) })
	})
}

func (a *app) cmdShow(_ context.Context, s *state, f flags, pos []string) error {
	r, err := resolveResource(s.answer, pos[0])
	if err != nil {
		return err
	}
	sub := resourceSubset(s.answer, r)
	return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Show(w, sub, r.ID, o) })
}

func (a *app) cmdWhy(_ context.Context, s *state, f flags, pos []string) error {
	r, err := resolveResource(s.answer, pos[0])
	if err != nil {
		return err
	}
	sub := resourceSubset(s.answer, r)
	return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Why(w, sub, r.ID, o) })
}

func (a *app) cmdExposed(_ context.Context, s *state, f flags, _ []string) error {
	ex := map[string]bool{}
	for _, fd := range s.answer.Findings {
		if fd.State != contract.ExposureFindingStateNotExposed {
			ex[fd.ResourceID] = true
		}
	}
	sess := map[string]bool{}
	for _, at := range s.answer.Attributions {
		if ex[at.ResourceID] && at.SessionID != nil {
			sess[*at.SessionID] = true
		}
	}
	sub := answer.Subset(s.answer, func(r contract.Resource) bool { return ex[r.ID] }, func(v contract.SessionView) bool { return sess[v.Session.ID] })
	// Keep every check so "nothing exposed" can be told apart from "not checked".
	sub.Checks = s.answer.Checks
	sub.Summary = answer.Summarize(sub, s.answer.GeneratedAt)
	return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Exposed(w, sub, o) })
}

func (a *app) cmdFix(_ context.Context, s *state, f flags, pos []string) error {
	r, err := resolveResource(s.answer, pos[0])
	if err != nil {
		return err
	}
	sub := resourceSubset(s.answer, r)
	var remove *contract.Fix
	if s.reg != nil && r.SeenVia != contract.ResourceSeenViaEvidence && r.Existence != contract.ResourceExistenceGone {
		remove = s.reg.RemoveHint(r)
	}
	return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Fix(w, sub, r.ID, remove, o) })
}

func (a *app) cmdReview(ctx context.Context, args []string) error {
	var f flags
	var note string
	fs := readFlags("review", &f)
	fs.StringVar(&note, "note", "", "optional note stored with the decision")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 && len(pos) != 2 {
		return usageErr("review takes no arguments, or <resource> keep|not-needed")
	}
	return a.withState(ctx, f, func(s *state) error {
		if len(pos) == 0 {
			ids := map[string]bool{}
			for _, rv := range s.answer.Reviews {
				ids[rv.ResourceID] = true
			}
			sub := answer.Subset(s.answer, func(r contract.Resource) bool { return ids[r.ID] }, noSessions)
			return a.emit(f, sub, func(w io.Writer, o render.Options) error { return render.Review(w, sub, o) })
		}
		var state contract.ReviewItemState
		switch pos[1] {
		case "keep":
			state = contract.ReviewItemStateKeep
		case "not-needed", "not_needed":
			state = contract.ReviewItemStateNotNeeded
		default:
			return usageErr("decision must be keep or not-needed, not %q", pos[1])
		}
		r, err := resolveResource(s.answer, pos[0])
		if err != nil {
			return err
		}
		var item *contract.ReviewItem
		for i := range s.answer.Reviews {
			if s.answer.Reviews[i].ResourceID == r.ID {
				item = &s.answer.Reviews[i]
			}
		}
		if item == nil {
			return fmt.Errorf("%s has no review: only existing, agent-created resources are reviewed", render.DisplayName(r))
		}
		now := a.now()
		by := s.cfg.DeveloperID()
		rec := contract.ReviewItem{ResourceID: r.ID, State: state, DueAt: item.DueAt, OwnerDeveloperID: item.OwnerDeveloperID, DecidedAt: &now, DecidedBy: &by}
		if note != "" {
			n := clip(note, 500)
			rec.Note = &n
		}
		if err := s.st.SetReview(ctx, rec); err != nil {
			return err
		}
		if err := a.compute(ctx, s); err != nil {
			return err
		}
		if f.json {
			return render.JSON(a.stdout, resourceSubset(s.answer, r))
		}
		a.out("Recorded: %s %s → %s. Muretto only records this; it never deletes anything.", render.ShortKind(r.Kind), render.DisplayName(r), pos[1])
		if state == contract.ReviewItemStateNotNeeded {
			a.out("To remove it yourself: muretto fix %s", render.DisplayName(r))
		}
		return nil
	})
}

type providersJSON struct {
	Providers []providerJSON           `json:"providers"`
	Checks    []contract.ProviderCheck `json:"checks"`
}

type providerJSON struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Accounts    []accountJSON  `json:"accounts"`
	Coverage    []coverageJSON `json:"coverage,omitempty"`
}

type accountJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	TokenSource string `json:"token_source"`
}

type coverageJSON struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

func (a *app) cmdProviders(ctx context.Context, args []string) error {
	var f flags
	var coverage bool
	fs := readFlags("providers", &f)
	fs.BoolVar(&coverage, "coverage", false, "show the coverage matrix")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("providers takes no arguments")
	}
	return a.withState(ctx, f, func(s *state) error {
		if s.reg == nil {
			return errors.New("provider adapters are not available in this build")
		}
		var rows []render.ProviderRow
		var js providersJSON
		js.Checks = s.answer.Checks
		for _, p := range s.reg.Providers() {
			row := render.ProviderRow{Name: p.Name, DisplayName: p.DisplayName}
			pj := providerJSON{Name: p.Name, DisplayName: p.DisplayName, Accounts: []accountJSON{}}
			for _, acct := range s.cfg.Accounts {
				if acct.Provider != p.Name {
					continue
				}
				_, src, _ := a.secrets.Provider(acct.Provider, acct.ID)
				row.Accounts = append(row.Accounts, fmt.Sprintf("%s · token from %s", acctLabel(acct), src))
				pj.Accounts = append(pj.Accounts, accountJSON{ID: acct.ID, Name: acct.Name, TokenSource: string(src)})
			}
			for _, c := range p.Coverage {
				row.Coverage = append(row.Coverage, [3]string{c.Kind, c.Status, c.Note})
				if coverage {
					pj.Coverage = append(pj.Coverage, coverageJSON{c.Kind, c.Status, c.Note})
				}
			}
			rows = append(rows, row)
			js.Providers = append(js.Providers, pj)
		}
		if f.json {
			return render.JSON(a.stdout, js)
		}
		if err := render.Providers(a.stdout, s.answer, rows, coverage, a.renderOpts(f, a.stdout)); err != nil {
			return err
		}
		if s.cfg.Options.SupabaseCatalogRead {
			a.out("\nSupabase catalog read: on (lists every table; that permission can read every row).")
		}
		return nil
	})
}

// since formats the age of t relative to now for status lines.
func since(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return answer.Age(now.Sub(t)) + " ago"
}
