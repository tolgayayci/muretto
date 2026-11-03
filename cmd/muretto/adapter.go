package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	adapterfiles "github.com/tolgayayci/muretto/adapters"
	"github.com/tolgayayci/muretto/internal/adaptercheck"
	"github.com/tolgayayci/muretto/internal/adaptergen"
	"github.com/tolgayayci/muretto/internal/adapters"
	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/openapi"
	"github.com/tolgayayci/muretto/internal/paths"
)

const adapterUsage = `Usage:
  muretto adapter new <openapi.json|yaml> [flags]   draft an adapter from an OpenAPI description
  muretto adapter check <adapter.yaml>              validate an adapter and show what it may call
  muretto adapter test <adapter.yaml> [flags]       run its fixtures (and a dry run) offline
  muretto adapter install <adapter.yaml> [--force]  use it on this machine (~/.muretto/adapters)
  muretto adapter list                              built-in and installed adapters
  muretto adapter remove <provider>                 uninstall an installed adapter

Flags for new:
  --provider slug        provider name (default: from the spec title)
  --name "Display"       display name
  --base-url URL         https base URL, when the spec has none or several
  --list                 only list the candidate kinds, write nothing
  --kinds a,b,c          generate only these kinds (and their parents)
  --max-depth n          nesting levels for child kinds (default 2)
  --accounts path|none   accounts endpoint, or none for one account per token
  --mcp-server name      add MCP signatures, assuming tools are named after operationIds
  --no-rules             do not suggest exposure rules
  --include-deprecated   also use deprecated operations
  -o, --out file         where to write the adapter (default <provider>.yaml; - for stdout)
  --fixtures dir         also write starter test fixtures there (adapters/<provider>/testdata)
  --install              install it right away
  --force                overwrite existing files
  --json                 print the generation report as JSON

Flags for test:
  --fixtures dir         fixtures to run (default: testdata/ next to adapter.yaml,
                         or testdata/<provider> next to any other file)
  --openapi file         also dry-run a scan against fake responses built from this spec
  --strict               apply the rules every adapter shipped with Muretto must meet (CI does)

Tests never touch the network: requests go through the broker's checks and
are answered locally.

The spec is read from disk only: Muretto never fetches it or its $refs.
`

func (a *app) cmdAdapter(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(a.stdout, adapterUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "new":
		return a.adapterNew(rest)
	case "check":
		return a.adapterCheck(rest)
	case "install":
		return a.adapterInstall(rest)
	case "test":
		return a.adapterTest(rest)
	case "list":
		return a.adapterList(rest)
	case "remove", "uninstall":
		return a.adapterRemove(rest)
	}
	return usageErr("unknown adapter command %q (new, check, test, install, list, remove)", sub)
}

func (a *app) adapterNew(args []string) error {
	fs := flag.NewFlagSet("adapter new", flag.ContinueOnError)
	var opt adaptergen.Options
	var kinds, out, fixtures string
	var list, install, force, asJSON bool
	fs.StringVar(&opt.Provider, "provider", "", "")
	fs.StringVar(&opt.DisplayName, "name", "", "")
	fs.StringVar(&opt.BaseURL, "base-url", "", "")
	fs.StringVar(&kinds, "kinds", "", "")
	fs.IntVar(&opt.MaxDepth, "max-depth", 2, "")
	fs.StringVar(&opt.Accounts, "accounts", "", "")
	fs.StringVar(&opt.MCPServer, "mcp-server", "", "")
	fs.BoolVar(&opt.NoRules, "no-rules", false, "")
	fs.BoolVar(&opt.IncludeDeprecated, "include-deprecated", false, "")
	fs.StringVar(&out, "out", "", "")
	fs.StringVar(&out, "o", "", "")
	fs.StringVar(&fixtures, "fixtures", "", "")
	fs.BoolVar(&list, "list", false, "")
	fs.BoolVar(&install, "install", false, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&asJSON, "json", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("adapter new takes one OpenAPI file (download it first; Muretto never fetches it)")
	}
	if strings.Contains(pos[0], "://") {
		return usageErr("adapter new reads a local file; download the spec first (e.g. curl -o openapi.json %s)", pos[0])
	}
	if kinds != "" {
		opt.Kinds = strings.Split(kinds, ",")
	}
	specPath, err := filepath.Abs(pos[0])
	if err != nil {
		return err
	}
	st, err := os.Stat(specPath) // #nosec G703 -- a file the user named
	if err != nil {
		return err
	}
	if st.Size() > openapi.MaxFileSize {
		return fmt.Errorf("%s is larger than %d MB", pos[0], openapi.MaxFileSize>>20)
	}
	data, err := os.ReadFile(specPath) // #nosec G304 G703 -- a file the user named
	if err != nil {
		return err
	}
	doc, err := openapi.Parse(data, openapi.Options{FS: os.DirFS(filepath.Dir(specPath)), Path: filepath.Base(specPath)})
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	opt.SpecName = filepath.Base(specPath)
	res, genErr := adaptergen.Generate(doc, opt)
	if asJSON && res != nil {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res.Report); err != nil {
			return err
		}
	}
	if list {
		if res == nil {
			return genErr
		}
		if !asJSON {
			a.printCandidates(res.Report)
		}
		return nil
	}
	if genErr != nil {
		if res != nil && res.Report != nil && !asJSON {
			a.printSkipped(res.Report, 15)
		}
		return genErr
	}
	if out == "" {
		out = res.Provider + ".yaml"
	}
	if out == "-" {
		_, err := a.stdout.Write(res.YAML)
		return err
	}
	if err := writeNew(out, res.YAML, force); err != nil {
		return err
	}
	if !asJSON {
		a.printReport(res.Report, out)
	}
	if fixtures != "" {
		if err := a.writeFixtures(res, fixtures, force); err != nil {
			return err
		}
	}
	if install {
		return a.installAdapter(out, res.YAML, force)
	}
	if !asJSON {
		a.out("\nNext: review the lines marked REVIEW in %s, then", out)
		a.out("  muretto adapter check %s", out)
		if fixtures != "" {
			a.out("  muretto adapter test %s --fixtures %s --strict", out, fixtures)
		} else {
			a.out("  muretto adapter test %s --openapi %s", out, pos[0])
		}
		a.out("  muretto adapter install %s", out)
		a.out("  muretto init                     # connect your %s token", p0(res))
	}
	return nil
}

func writeNew(path string, data []byte, force bool) error {
	if _, err := os.Stat(path); err == nil && !force { // #nosec G703 -- an output path the user named
		return fmt.Errorf("%s exists; pass --force to overwrite it", path)
	}
	// -o adapters/<provider>/adapter.yaml creates the provider's folder.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G703 -- an output path the user named
		return err
	}
	return os.WriteFile(path, data, 0o600) // #nosec G703 -- an output path the user named
}

func (a *app) printReport(r *adaptergen.Report, out string) {
	sel := 0
	for _, k := range r.Kinds {
		if k.Selected {
			sel++
		}
	}
	a.out("Wrote %s (%s)", out, r.Spec)
	a.out("  provider   %s", r.Provider)
	a.out("  base URL   %s", r.BaseURL)
	a.out("  auth       %s", r.Auth)
	a.out("  accounts   %s", r.Accounts)
	a.out("\n%d kinds:", sel)
	tw := tabwriter.NewWriter(a.stdout, 2, 4, 2, ' ', 0)
	for _, k := range r.Kinds {
		if !k.Selected {
			continue
		}
		flags := []string{}
		if k.Create {
			flags = append(flags, "create")
		}
		parent := ""
		if k.Parent != "" {
			parent = "per " + k.Parent
		}
		fmt.Fprintf(tw, "  %s\tGET %s\t%s\t%s\t%s\n", k.Kind, k.Path, parent, k.Paging, strings.Join(flags, ","))
	}
	_ = tw.Flush()
	if len(r.Signatures) > 0 {
		a.out("\n%d creation signatures (curl calls to the API).", len(r.Signatures))
	}
	if len(r.Rules) > 0 {
		a.out("%d suggested exposure rules: %s", len(r.Rules), strings.Join(r.Rules, ", "))
	}
	not := len(r.Kinds) - sel
	if not > 0 || len(r.Skipped) > 0 {
		a.out("\nNot generated: %d more candidate kinds (see --list, pick with --kinds) and %d skipped endpoints.", not, len(r.Skipped))
	}
	if len(r.Review) > 0 {
		a.out("\nReview:")
		for _, l := range r.Review {
			a.out("  - %s", l)
		}
	}
	if len(r.Warnings) > 0 {
		a.out("\n%d spec warnings, e.g.: %s", len(r.Warnings), r.Warnings[0])
	}
	a.out("\nNot in an OpenAPI file, so not generated: CLI output signatures and MCP tool names")
	a.out("(see adapters/WRITING.md section 7 to add them).")
}

func (a *app) printCandidates(r *adaptergen.Report) {
	a.out("%s: %s, accounts %s", r.Spec, r.BaseURL, r.Accounts)
	tw := tabwriter.NewWriter(a.stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  KIND\tPATH\tPARENT\tDEFAULT")
	for _, k := range r.Kinds {
		def := "yes"
		if !k.Selected {
			def = "no: " + k.Note
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", k.Kind, k.Path, k.Parent, def)
	}
	_ = tw.Flush()
	a.printSkipped(r, 20)
}

func (a *app) printSkipped(r *adaptergen.Report, max int) {
	if len(r.Skipped) == 0 {
		return
	}
	a.out("\nSkipped list endpoints (%d):", len(r.Skipped))
	for i, s := range r.Skipped {
		if i == max {
			a.out("  ... and %d more (--json shows all)", len(r.Skipped)-max)
			break
		}
		a.out("  %s: %s", s.Path, s.Reason)
	}
}

func (a *app) adapterCheck(args []string) error {
	pos, err := parse(flag.NewFlagSet("adapter check", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("adapter check takes one adapter file")
	}
	data, err := readAdapterFile(pos[0])
	if err != nil {
		return err
	}
	reg, err := adapters.LoadFile(filepath.Base(pos[0]), data)
	if err != nil {
		return err
	}
	p := reg.Providers()[0]
	a.out("%s (%s): valid", p.DisplayName, p.Name)
	if builtIn(p.Name) {
		a.out("  note: %q is a built-in provider; an installed copy would be refused", p.Name)
	}
	a.out("\nIt may only send these requests (the broker refuses anything else):")
	tw := tabwriter.NewWriter(a.stdout, 2, 4, 2, ' ', 0)
	for _, r := range reg.Allowlist() {
		q := ""
		if len(r.Query) > 0 {
			q = "?" + strings.Join(r.Query, ",")
		}
		fmt.Fprintf(tw, "  %s\thttps://%s%s%s\n", r.Method, r.Host, r.PathPattern, q)
	}
	_ = tw.Flush()
	a.out("\nKinds:")
	for _, c := range p.Coverage {
		a.out("  %-24s %-8s %s", c.Kind, c.Status, c.Note)
	}
	if p.AccountPrompt != "" {
		a.out("\nAccounts: muretto init asks for %q.", p.AccountPrompt)
	}
	if n := strings.Count(string(data), "REVIEW"); n > 0 {
		a.out("\n%d lines are marked REVIEW.", n)
	}
	return nil
}

func readAdapterFile(path string) ([]byte, error) {
	st, err := os.Stat(path) // #nosec G703 -- a file the user named
	if err != nil {
		return nil, err
	}
	if st.Size() > maxAdapterFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxAdapterFile)
	}
	return os.ReadFile(path) // #nosec G304 G703 -- a file the user named
}

func builtIn(provider string) bool {
	reg, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		return false
	}
	for _, p := range reg.Providers() {
		if p.Name == provider {
			return true
		}
	}
	return false
}

func (a *app) adapterInstall(args []string) error {
	fs := flag.NewFlagSet("adapter install", flag.ContinueOnError)
	var force bool
	fs.BoolVar(&force, "force", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("adapter install takes one adapter file")
	}
	data, err := readAdapterFile(pos[0])
	if err != nil {
		return err
	}
	return a.installAdapter(pos[0], data, force)
}

func (a *app) installAdapter(from string, data []byte, force bool) error {
	reg, err := adapters.LoadFile(filepath.Base(from), data)
	if err != nil {
		return err
	}
	p := reg.Providers()[0]
	if builtIn(p.Name) {
		return fmt.Errorf("%q is a built-in provider; rename it (provider: ...) to install a copy", p.Name)
	}
	for _, r := range reg.Allowlist() {
		if r.Method != http.MethodGet {
			return fmt.Errorf("installed adapters may only use GET (endpoint %s %s)", r.Method, r.PathPattern)
		}
	}
	ps, err := a.resolvePaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ps.Adapters, paths.DirPerm); err != nil {
		return err
	}
	dest := filepath.Join(ps.Adapters, p.Name+".yaml")
	if old, err := os.ReadFile(dest); err == nil && !force && string(old) != string(data) { // #nosec G304 -- our own directory
		return fmt.Errorf("%s is already installed (%s); pass --force to replace it", p.Name, dest)
	}
	if err := os.WriteFile(dest, data, paths.FilePerm); err != nil { // #nosec G703 -- our own directory; the provider name is a validated slug
		return err
	}
	a.out("\nInstalled %s to %s.", p.DisplayName, dest)
	a.out("Run `muretto init` to connect a %s token; `muretto providers` lists it.", p.DisplayName)
	return nil
}

func (a *app) adapterList(args []string) error {
	pos, err := parse(flag.NewFlagSet("adapter list", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageErr("adapter list takes no arguments")
	}
	reg, err := adapters.Load(adapterfiles.Files)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(a.stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tNAME\tSOURCE")
	for _, p := range reg.Providers() {
		fmt.Fprintf(tw, "%s\t%s\tbuilt in\n", p.Name, p.DisplayName)
	}
	ps, err := a.resolvePaths()
	if err == nil {
		names, data, errs := localAdapters(ps.Adapters)
		for i, n := range names {
			if err := reg.AddLocal(n, data[i]); err != nil {
				errs = append(errs, err)
			}
		}
		var local []adapters.ProviderInfo
		for _, p := range reg.Providers() {
			if p.Source != "" {
				local = append(local, p)
			}
		}
		sort.Slice(local, func(i, j int) bool { return local[i].Name < local[j].Name })
		for _, p := range local {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.DisplayName, p.Source)
		}
		_ = tw.Flush()
		for _, e := range errs {
			a.out("broken: %v", e)
		}
		return nil
	}
	return tw.Flush()
}

func (a *app) adapterRemove(args []string) error {
	pos, err := parse(flag.NewFlagSet("adapter remove", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("adapter remove takes a provider name")
	}
	name := pos[0]
	if builtIn(name) {
		return fmt.Errorf("%q is built in and cannot be removed", name)
	}
	ps, err := a.resolvePaths()
	if err != nil {
		return err
	}
	dest := filepath.Join(ps.Adapters, filepath.Base(name)+".yaml")
	if err := os.Remove(dest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no installed adapter %q in %s", name, ps.Adapters)
		}
		return err
	}
	a.out("Removed %s. Its accounts stay in config until `muretto init` or `muretto uninstall`.", dest)
	return nil
}

// writeFixtures writes starter fixtures for a generated adapter.
func (a *app) writeFixtures(res *adaptergen.Result, dir string, force bool) error {
	files, err := adaptergen.Fixtures(res)
	if err != nil {
		return fmt.Errorf("fixtures: %w", err)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !force {
		return fmt.Errorf("%s is not empty; pass --force to write fixtures into it", dir)
	}
	names := make([]string, 0, len(files))
	total := 0
	for n, b := range files {
		names = append(names, n)
		total += len(b)
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { // #nosec G301 -- fixtures are committed test data
			return err
		}
		if err := os.WriteFile(p, files[n], 0o644); err != nil { // #nosec G306 G703 -- fixtures are committed test data, under a directory the user named
			return err
		}
	}
	a.out("\nWrote %d fixture files to %s: scan/basic, scan/failures (one endpoint answers 403), extract/cases.yaml.", len(files), dir)
	a.out("Their bodies are fake values built from the spec. Replace them with recorded, sanitized")
	a.out("responses (adapters/FIXTURES.md) before you rely on them.")
	if total > 2<<20 {
		a.out("They are large (%d MB): trim the bodies before committing them.", total>>20)
	}
	return nil
}

func (a *app) adapterTest(args []string) error {
	fs := flag.NewFlagSet("adapter test", flag.ContinueOnError)
	var fixtures, spec string
	var strict bool
	fs.StringVar(&fixtures, "fixtures", "", "")
	fs.StringVar(&spec, "openapi", "", "")
	fs.BoolVar(&strict, "strict", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("adapter test takes one adapter file")
	}
	data, err := readAdapterFile(pos[0])
	if err != nil {
		return err
	}
	reg, err := adapters.LoadFile(filepath.Base(pos[0]), data)
	if err != nil {
		return err
	}
	p := reg.Providers()[0]
	// Where fixtures live: testdata/ next to adapter.yaml, as in
	// adapters/<provider>/, or testdata/<provider> next to a single file.
	defaultFixtures := filepath.Join(filepath.Dir(pos[0]), "testdata")
	if filepath.Base(pos[0]) != "adapter.yaml" {
		defaultFixtures = filepath.Join(defaultFixtures, p.Name)
	}
	if fixtures == "" && (dirExists(defaultFixtures) || strict) {
		fixtures = defaultFixtures
	}
	if fixtures == "" && spec == "" && !strict {
		return usageErr("nothing to test: no %s; pass --fixtures, or --openapi for a dry run (adapter new --fixtures writes starter fixtures)",
			defaultFixtures)
	}
	failed := false
	a.out("%s (%s)", p.DisplayName, p.Name)
	if fixtures != "" || strict {
		rep, err := adaptercheck.Run(reg, p.Name, fixtures, strict)
		if err != nil {
			return err
		}
		for _, s := range rep.Scenarios {
			status := "ok"
			if len(s.Problems) > 0 {
				status = "FAIL"
			}
			a.out("  scan/%-12s %-4s %d requests, %d resources, %d findings", s.Name, status, s.Requests, s.Resources, s.Findings)
		}
		if rep.Cases > 0 {
			status := "ok"
			if len(rep.CaseProblems) > 0 {
				status = "FAIL"
			}
			a.out("  extract           %-4s %d cases", status, rep.Cases)
		}
		if strict {
			status := "ok"
			if len(rep.Rules) > 0 {
				status = "FAIL"
			}
			a.out("  shipping rules    %s", status)
		}
		for _, prob := range rep.Problems() {
			a.out("    - %s", strings.ReplaceAll(prob, "\n", "\n      "))
		}
		failed = failed || !rep.OK()
	}
	if spec != "" {
		ok, err := a.dryRun(reg, p.Name, spec)
		if err != nil {
			return err
		}
		failed = failed || !ok
	}
	if failed {
		return errors.New("adapter test failed")
	}
	a.out("PASS")
	return nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p) // #nosec G703 -- a path the user named
	return err == nil && st.IsDir()
}

// dryRun scans with the adapter against fake responses built from the spec,
// through an offline broker.
func (a *app) dryRun(reg *adapters.Registry, provider, specPath string) (bool, error) {
	data, err := os.ReadFile(specPath) // #nosec G304 -- a file the user named
	if err != nil {
		return false, err
	}
	abs, _ := filepath.Abs(specPath)
	doc, err := openapi.Parse(data, openapi.Options{FS: os.DirFS(filepath.Dir(abs)), Path: filepath.Base(abs)})
	if err != nil {
		return false, err
	}
	base := ""
	for _, r := range reg.Allowlist() {
		if r.Provider == provider {
			for _, s := range doc.Servers {
				if u, err := url.Parse(s.URL); err == nil && strings.EqualFold(u.Hostname(), r.Host) {
					base = strings.TrimSuffix(u.Path, "/")
				}
			}
			break
		}
	}
	fake := adaptergen.NewFakeAPI(doc, base)
	b := broker.NewOffline(reg.Allowlist(), fake.Respond)
	ctx := context.Background()
	acct := adapters.Account{ID: "example-account", Name: "Example account"}
	if accts, err := reg.Accounts(ctx, b, provider, "fake-test-token"); err == nil && len(accts) > 0 {
		acct = accts[0]
	} else if err != nil && !errors.Is(err, adapters.ErrAccountInput) {
		a.out("  dry run: accounts failed: %v", err)
		return false, nil
	}
	res, err := reg.Scan(ctx, b, provider, adapters.Credentials{Token: "fake-test-token", AccountID: acct.ID, AccountName: acct.Name, AccountSlug: acct.Slug})
	if err != nil {
		return false, err
	}
	perKind := map[string]int{}
	for _, r := range res.Resources {
		perKind[r.Kind]++
	}
	failedChecks := 0
	var problems []string
	for _, c := range res.Checks {
		if c.Status != "ok" {
			failedChecks++
			msg := ""
			if c.Error != nil {
				msg = *c.Error
			}
			problems = append(problems, fmt.Sprintf("check %s failed: %s", c.Kind, msg))
		}
	}
	for _, u := range fake.Unmatched {
		problems = append(problems, "requested an endpoint the spec does not have: "+u)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), adaptercheck.FakeSecret) {
		problems = append(problems, "a secret-looking field reached the scan result (strip it, or drop it from the attributes)")
	}
	status := "ok"
	if len(problems) > 0 {
		status = "FAIL"
	}
	a.out("  dry run (spec)    %-4s %d requests, %d resources, %d findings, %d failed checks", status, len(fake.Calls), len(res.Resources), len(res.Findings), failedChecks)
	kinds := make([]string, 0, len(perKind))
	for k := range perKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		a.out("    %-30s %d", k, perKind[k])
	}
	for _, p := range problems {
		a.out("    - %s", p)
	}
	return len(problems) == 0, nil
}

// p0 is a generated adapter's display name.
func p0(res *adaptergen.Result) string {
	if reg, err := adapters.LoadFile("x.yaml", res.YAML); err == nil {
		return reg.Providers()[0].DisplayName
	}
	return res.Provider
}
