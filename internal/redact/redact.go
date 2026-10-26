// Package redact removes secrets from text and decoded JSON. It is used when
// data is recorded (hook, ingest) and again when it is shown or synced.
package redact

// Placeholder replaces every redacted value.
const Placeholder = "[REDACTED]"

// String redacts secret-shaped substrings in s.
func String(s string) string {
	return s // TODO(stream A): implement
}

// Value returns a deep copy of decoded JSON with every string redacted and the
// values of secret-looking keys replaced.
func Value(v any) any {
	return v // TODO(stream A): implement
}
