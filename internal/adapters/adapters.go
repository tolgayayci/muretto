// Package adapters interprets the declarative provider adapters in /adapters:
// read-only scans, agent-creation signatures, and exposure rules.
//
// The engine is generic: every provider-specific fact (endpoints, field
// mappings, signatures, rules) lives in the adapter YAML. Adding a provider is
// one YAML file plus fixtures; see adapters/WRITING.md.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

// Credentials for one provider account. Token is never stored by this package.
type Credentials struct {
	Token     string
	AccountID string
	// AccountName is optional display name, copied onto resources.
	AccountName string
	// AccountSlug is optional (Account.Slug from Accounts). It is stamped on
	// every resource as attributes.account_slug, which name-based identifiers
	// (e.g. the Vercel team slug the CLI prints) are matched against. When
	// empty, the adapter's account_slug template is used (Supabase: the org
	// slug, which is the account id).
	AccountSlug string
	// Options turns on opt-in adapter features by name (see ProviderInfo.Options).
	// Keys in use: "catalog_rls" (Supabase: list every table and its RLS state
	// with the pinned read-only catalog query; needs Database Read).
	Options map[string]bool
}

// Account is a provider org or team the token can see.
type Account struct {
	ID   string
	Name string
	Slug string // URL/CLI slug when the provider has one (Vercel team slug, Supabase org slug)
}

// ScanResult is everything one read-only scan of one account found.
type ScanResult struct {
	Resources []contract.Resource
	Findings  []contract.ExposureFinding
	Costs     []contract.CostRecord
	Checks    []contract.ProviderCheck
}

// CoverageRow is one line of `muretto providers --coverage`.
type CoverageRow struct {
	Kind   string
	Status string // full, partial or planned
	Note   string
}

// Option is an opt-in adapter feature.
type Option struct {
	Name        string
	Description string
}

// ProviderInfo describes one adapter.
type ProviderInfo struct {
	Name        string
	DisplayName string
	TokenHelp   string
	Coverage    []CoverageRow
	Options     []Option
	Source      string // file of a local adapter; "" for a built-in one
	// AccountPrompt, when set, is what the user must type as the account id
	// (the adapter has no endpoint that lists accounts).
	AccountPrompt string
}

// ErrAccountInput is returned by Accounts for adapters whose account id the
// user types (ProviderInfo.AccountPrompt).
var ErrAccountInput = errors.New("this provider's account id is entered by the user")

// Registry holds the loaded adapters.
type Registry struct {
	adapters []*adapterSpec
	byName   map[string]*adapterSpec
	now      func() time.Time
}

// Load reads every adapter in fsys: <provider>/adapter.yaml folders (the
// layout of adapters/) and plain *.yaml files. Names starting with "_" or
// "template" are documentation and are not loaded.
func Load(fsys fs.FS) (*Registry, error) {
	flat, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	nested, err := fs.Glob(fsys, "*/adapter.yaml")
	if err != nil {
		return nil, err
	}
	names := append(flat, nested...)
	r := &Registry{byName: map[string]*adapterSpec{}, now: func() time.Time { return time.Now().UTC() }}
	var errs []error
	for _, name := range names {
		if strings.HasPrefix(name, "template") || strings.HasPrefix(name, "_") {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		a, err := parseAdapter(data, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if r.byName[a.Provider] != nil {
			errs = append(errs, fmt.Errorf("%s: provider %q defined twice", name, a.Provider))
			continue
		}
		r.byName[a.Provider] = a
		r.adapters = append(r.adapters, a)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sort.Slice(r.adapters, func(i, j int) bool { return r.adapters[i].Provider < r.adapters[j].Provider })
	return r, nil
}

// LoadFile returns a registry holding just one adapter file (for checks).
func LoadFile(file string, data []byte) (*Registry, error) {
	a, err := parseAdapter(data, file)
	if err != nil {
		return nil, err
	}
	a.source = file
	return &Registry{adapters: []*adapterSpec{a}, byName: map[string]*adapterSpec{a.Provider: a}, now: func() time.Time { return time.Now().UTC() }}, nil
}

// SignatureIDs lists a provider's signature ids in file order.
func (r *Registry) SignatureIDs(provider string) []string {
	a := r.byName[provider]
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.Signatures))
	for _, s := range a.Signatures {
		out = append(out, s.ID)
	}
	return out
}

// tokenMode names a token by the first matching accounts.token_modes pattern.
func tokenMode(a *adapterSpec, token string) string {
	names := make([]string, 0, len(a.Accounts.TokenModes))
	for n := range a.Accounts.TokenModes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if a.Accounts.TokenModes[n].MatchString(token) {
			return n
		}
	}
	return ""
}

// SignatureRole is a signature's role (created or referenced), or "".
func (r *Registry) SignatureRole(provider, id string) string {
	if a := r.byName[provider]; a != nil {
		for _, s := range a.Signatures {
			if s.ID == id {
				return s.Role
			}
		}
	}
	return ""
}

// Validate parses and checks one adapter file without loading it.
func Validate(data []byte, file string) error {
	_, err := parseAdapter(data, file)
	return err
}

// AddLocal loads an adapter the user installed (~/.muretto/adapters), on
// top of the built-in ones. Local adapters cannot replace a built-in
// provider and cannot use pinned POST bodies, which are reviewed per
// built-in adapter.
func (r *Registry) AddLocal(file string, data []byte) error {
	a, err := parseAdapter(data, file)
	if err != nil {
		return err
	}
	if r.byName[a.Provider] != nil {
		return fmt.Errorf("%s: provider %q is already defined (by %s)", file, a.Provider, r.byName[a.Provider].source)
	}
	for name, ep := range a.Endpoints {
		if ep.Method != http.MethodGet {
			return fmt.Errorf("%s: endpoint %s: local adapters may only use GET", file, name)
		}
	}
	a.source = file
	r.byName[a.Provider] = a
	r.adapters = append(r.adapters, a)
	sort.Slice(r.adapters, func(i, j int) bool { return r.adapters[i].Provider < r.adapters[j].Provider })
	return nil
}

// Providers lists the loaded adapters.
func (r *Registry) Providers() []ProviderInfo {
	var out []ProviderInfo
	for _, a := range r.adapters {
		p := ProviderInfo{Name: a.Provider, DisplayName: a.DisplayName, TokenHelp: strings.TrimSpace(a.TokenHelp), Source: a.source, AccountPrompt: a.Accounts.Ask}
		for _, c := range a.Coverage {
			p.Coverage = append(p.Coverage, CoverageRow(c))
		}
		for _, o := range a.Options {
			p.Options = append(p.Options, Option(o))
		}
		out = append(out, p)
	}
	return out
}

// Accounts lists the orgs/teams a token can see.
func (r *Registry) Accounts(ctx context.Context, b *broker.Broker, provider, token string) ([]Account, error) {
	a, err := r.adapter(provider, b)
	if err != nil {
		return nil, err
	}
	if a.Accounts.Ask != "" {
		return nil, ErrAccountInput
	}
	mode := tokenMode(a, token)
	if a.Accounts.Endpoint == "" {
		// One implicit account per token (an API with no org, team or
		// current-user endpoint).
		mctx := map[string]any{"token_mode": mode}
		id, _ := a.Accounts.ID.render(mctx)
		name, ok := a.Accounts.Name.render(mctx)
		if !ok || name == "" {
			name = a.DisplayName
		}
		slug, _ := a.Accounts.Slug.render(mctx)
		return []Account{{ID: id, Name: name, Slug: slug}}, nil
	}
	s := &scanner{a: a, b: b, ctx: ctx, creds: Credentials{Token: token}}
	pages, err := s.fetch(a.Endpoints[a.Accounts.Endpoint], map[string]any{})
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, page := range pages {
		for _, raw := range itemsOf(page, a.Accounts.Items) {
			item := make(map[string]any, len(raw)+1)
			for k, v := range raw {
				item[k] = v
			}
			item["token_mode"] = mode
			id, ok := a.Accounts.ID.render(item)
			if !ok {
				continue
			}
			name, ok := a.Accounts.Name.render(item)
			if !ok {
				name = id
			}
			slug, _ := a.Accounts.Slug.render(item)
			out = append(out, Account{ID: id, Name: name, Slug: slug})
		}
	}
	return out, nil
}

// Scan lists resources, exposure findings, costs and check results for one
// account. Failed calls never fail the scan: they become ProviderChecks with
// status error, and the rules that depend on them stay unknown. The error is
// only for programming mistakes (unknown provider, nil broker).
func (r *Registry) Scan(ctx context.Context, b *broker.Broker, provider string, c Credentials) (ScanResult, error) {
	a, err := r.adapter(provider, b)
	if err != nil {
		return ScanResult{}, err
	}
	s := &scanner{a: a, b: b, ctx: ctx, creds: c, now: r.now(), byID: map[string]*scanned{}}
	for _, k := range a.Kinds {
		s.scanKind(k)
	}
	s.evaluateRules()
	return s.result(), nil
}

// Extract finds resource identifiers in an action using the adapters' signatures. Pure.
func (r *Registry) Extract(a observe.RawAction) []contract.Identifier {
	var out []contract.Identifier
	for _, ad := range r.adapters {
		out = append(out, extract(ad, a)...)
	}
	return dedupeIdentifiers(out)
}

// ProviderGuess returns the provider whose signatures or MCP servers match the
// action's tool call, or "" if none does. It does not need an id in the output:
// e.g. `stripe projects add supabase/project` guesses supabase. Pure.
func (r *Registry) ProviderGuess(a observe.RawAction) string {
	for _, ad := range r.adapters {
		if guess(ad, a) {
			return ad.Provider
		}
	}
	return ""
}

// Matches reports whether identifier id refers to resource res, using the
// signature's (or kind's) match rule from the adapter file. Pure. An
// identifier whose match rule needs a container but has none never matches.
func (r *Registry) Matches(id contract.Identifier, res contract.Resource) bool {
	if id.Provider != res.Provider || id.Kind != res.Kind {
		return false
	}
	a := r.byName[id.Provider]
	if a == nil {
		return false
	}
	ctx := resourceCtx(res, nil)
	for _, m := range matchesFor(a, id) {
		want, ok := m.Value.render(ctx)
		if !ok || want != id.Value {
			continue
		}
		if m.Container.empty() {
			return true
		}
		if id.Container == nil {
			continue
		}
		if c, ok := m.Container.render(ctx); ok && c == *id.Container {
			return true
		}
	}
	return false
}

// Allowlist returns the broker rules every adapter needs: exactly the
// endpoints declared in the adapter files, with their query parameter names.
func (r *Registry) Allowlist() []broker.Rule {
	var out []broker.Rule
	for _, a := range r.adapters {
		names := make([]string, 0, len(a.Endpoints))
		for n := range a.Endpoints {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, endpointRule(a, a.Endpoints[n]))
		}
	}
	return out
}

func endpointRule(a *adapterSpec, ep *endpointSpec) broker.Rule {
	var q []string
	for k := range ep.Query {
		q = append(q, k)
	}
	if ep.Pagination != nil {
		q = append(q, ep.Pagination.Param)
	}
	sort.Strings(q)
	rule := broker.Rule{
		Provider:    a.Provider,
		Host:        a.API.host,
		Method:      ep.Method,
		PathPattern: ep.Path,
		Query:       q,
		Strip:       append([]string(nil), ep.Strip...),
	}
	if ep.PinnedBody != "" {
		_, rule.BodySHA256, _ = broker.PinnedBody(ep.PinnedBody)
	}
	return rule
}

func (r *Registry) adapter(provider string, b *broker.Broker) (*adapterSpec, error) {
	a := r.byName[provider]
	if a == nil {
		return nil, fmt.Errorf("adapters: unknown provider %q", provider)
	}
	if b == nil {
		return nil, errors.New("adapters: nil broker")
	}
	return a, nil
}

func fullKind(a *adapterSpec, kind string) string { return a.Provider + "." + kind }

func kindByFull(a *adapterSpec, full string) *kindSpec {
	for _, k := range a.Kinds {
		if fullKind(a, k.Kind) == full {
			return k
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - 3
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}
