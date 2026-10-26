// Package adapters interprets the declarative provider adapters in /adapters:
// read-only scans, agent-creation signatures, exposure rules and fix hints.
package adapters

import (
	"context"
	"errors"
	"io/fs"

	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/observe"
)

// Credentials for one provider account. Token is never stored by this package.
type Credentials struct {
	Token     string
	AccountID string
}

// Account is a provider org or team the token can see.
type Account struct {
	ID   string
	Name string
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

// ProviderInfo describes one adapter.
type ProviderInfo struct {
	Name        string
	DisplayName string
	TokenHelp   string
	Coverage    []CoverageRow
}

// Registry holds the loaded adapters.
type Registry struct{}

// Load reads every adapter file in fsys.
func Load(fsys fs.FS) (*Registry, error) {
	return nil, errors.New("not implemented") // TODO(stream B)
}

// Providers lists the loaded adapters.
func (r *Registry) Providers() []ProviderInfo { return nil }

// Accounts lists the orgs/teams a token can see.
func (r *Registry) Accounts(ctx context.Context, b *broker.Broker, provider, token string) ([]Account, error) {
	return nil, errors.New("not implemented") // TODO(stream B)
}

// Scan lists resources, exposure findings, costs and check results for one account.
func (r *Registry) Scan(ctx context.Context, b *broker.Broker, provider string, c Credentials) (ScanResult, error) {
	return ScanResult{}, errors.New("not implemented") // TODO(stream B)
}

// Extract finds resource identifiers in an action using the adapters' signatures. Pure.
func (r *Registry) Extract(a observe.RawAction) []contract.Identifier { return nil }

// RemoveHint returns the command a human could run to delete res. Never executed.
func (r *Registry) RemoveHint(res contract.Resource) *contract.Fix { return nil }

// Allowlist returns the broker rules every adapter needs.
func (r *Registry) Allowlist() []broker.Rule { return nil }
