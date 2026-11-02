package adapters

import "strings"

// HumanRule is the short, resource-free name of an exposure rule. It titles
// findings that are not exposed: an adapter's own title is written for the
// exposed state and would be false otherwise.
func HumanRule(rule string) string {
	switch rule {
	case "rls_disabled":
		return "Row level security"
	case "public_bucket":
		return "Public bucket"
	case "public_preview":
		return "Public preview"
	case "client_exposed_env":
		return "Client-exposed env var"
	}
	return strings.ReplaceAll(rule, "_", " ")
}

// Texts for findings that are not exposed. The answer package uses them too,
// so a scanned and a synthesized not_exposed finding read the same.
const (
	NotExposedExplanation = "The last successful check did not flag this resource."
	UnknownExplanation    = "The check could not decide whether this resource is exposed: the data it needs was missing or inconclusive."
)

// NotExposedTitle titles a finding the last check did not flag.
func NotExposedTitle(rule string) string { return HumanRule(rule) + ": not exposed" }

// UnknownTitle titles a finding the check could not decide.
func UnknownTitle(rule string) string { return HumanRule(rule) + ": exposure unknown" }
