package store

import (
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// Entity keys: the identity a synced record is upserted by, per user. They
// are also the ids carried by SSE events.
//
//	session      <session id>
//	resource     <resource id>
//	attribution  <resource id>
//	finding      <finding id>
//	cost         <resource id>|<source>|<interval>|<period_start>
//	review       <resource id>
//	check        <provider>|<account id>|<kind>|<scope>|<rule id>

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// CostKey is the key of a cost record.
func CostKey(c contract.CostRecord) string {
	var interval, start string
	if c.Interval != nil {
		interval = string(*c.Interval)
	}
	if c.PeriodStart != nil {
		start = c.PeriodStart.UTC().Format(time.RFC3339)
	}
	return strings.Join([]string{c.ResourceID, string(c.Source), interval, start}, "|")
}

// CheckKey is the key of a provider check.
func CheckKey(c contract.ProviderCheck) string {
	return strings.Join([]string{c.Provider, c.AccountID, c.Kind, deref(c.Scope), deref(c.RuleID)}, "|")
}
