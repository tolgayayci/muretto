// Package redact removes secrets from text and decoded JSON. It is used when
// data is recorded (hook, ingest) and again when it is shown or synced.
//
// It is tuned to keep identifiers that are not secrets (Supabase project refs,
// Vercel dpl_/prj_/team_ ids, UUIDs, tool_use ids) because attribution depends
// on them, so there is deliberately no generic "high-entropy string" rule.
//
// Speed matters because the hook runs this on every tool call. Patterns are
// compiled once, and every regex runs only anchored at the positions where one
// of its literal triggers occurs (found with strings.Index), never as a scan
// over the whole text. URL userinfo and KEY=value pairs use small hand-written
// scanners instead of regexes.
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Placeholder replaces every redacted value.
const Placeholder = "[REDACTED]"

// anchored is a regex rule tried only at the positions where one of its
// triggers (lowercase ASCII) occurs in the lowercased text. The regex must
// start with `^` and match at the trigger position.
type anchored struct {
	name     string
	triggers []string
	re       *regexp.Regexp
	window   int                     // max bytes the match may span from the trigger
	repl     func(m []string) string // nil: replace the whole match with Placeholder
}

// tokenRules replace whole token shapes.
var tokenRules = []anchored{
	{name: "pem", triggers: []string{"-----begin "}, window: 64 << 10,
		re: regexp.MustCompile(`^-----BEGIN [A-Z0-9 ]{1,64}-----[\s\S]*?(?:-----END [A-Z0-9 ]{1,64}-----|\z)`)},
	{name: "jwt", triggers: []string{"eyj"}, window: 16 << 10,
		re: regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*`)},
	{name: "supabase_pat", triggers: []string{"sbp_"}, window: 256,
		re: regexp.MustCompile(`^sbp_[A-Za-z0-9_]{16,}`)},
	{name: "supabase_secret", triggers: []string{"sb_secret_"}, window: 256,
		re: regexp.MustCompile(`^sb_secret_[A-Za-z0-9_-]{8,}`)},
	// Publishable keys are public by design; we still redact them (safer, and
	// they are never needed for attribution).
	{name: "supabase_publishable", triggers: []string{"sb_publishable_"}, window: 256,
		re: regexp.MustCompile(`^sb_publishable_[A-Za-z0-9_-]{8,}`)},
	{name: "github", triggers: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, window: 512,
		re: regexp.MustCompile(`^(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)},
	{name: "vercel", triggers: []string{"vcp_"}, window: 256,
		re: regexp.MustCompile(`^vcp_[A-Za-z0-9_-]{16,}`)},
	{name: "stripe", triggers: []string{"sk_live_", "sk_test_", "rk_live_", "rk_test_", "whsec_"}, window: 512,
		re: regexp.MustCompile(`^(?:[sr]k_(?:live|test)_[A-Za-z0-9]{8,}|whsec_[A-Za-z0-9]{16,})`)},
	{name: "aws_access_key", triggers: []string{"akia", "asia", "agpa", "aida", "aroa", "aipa", "anpa", "anva"}, window: 20,
		re: regexp.MustCompile(`^(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA)[A-Z0-9]{16}`)},
	{name: "anthropic_openai", triggers: []string{"sk-"}, window: 512,
		re: regexp.MustCompile(`^sk-(?:ant-|proj-)?[A-Za-z0-9_-]{24,}`)},
	{name: "slack", triggers: []string{"xox"}, window: 512,
		re: regexp.MustCompile(`^xox[abposr]-[A-Za-z0-9-]{10,}`)},
	{name: "npm", triggers: []string{"npm_"}, window: 256,
		re: regexp.MustCompile(`^npm_[A-Za-z0-9]{30,}`)},
	{name: "google_api_key", triggers: []string{"aiza"}, window: 256,
		re: regexp.MustCompile(`^AIza[0-9A-Za-z_-]{30,}`)},
	{name: "muretto", triggers: []string{"mrt_"}, window: 256,
		re: regexp.MustCompile(`^mrt_[A-Za-z0-9_-]{16,}`)},
}

// contextRules keep the surrounding structure (header name, auth scheme,
// flag) and redact only the secret part.
var contextRules = []anchored{
	// Authorization / apikey style headers, in curl commands, logs or JSON text.
	{name: "header", triggers: []string{"authorization", "apikey", "api-key", "x-auth-token", "x-api-token"}, window: 4096,
		re: regexp.MustCompile(`^(?i)(authorization|api-?key|x-auth-token|x-api-token)(\\?["']?[ \t]*[:=][ \t]*\\?["']?)(?:(bearer|basic|token|digest)[ \t]+)?(\[REDACTED\]|[^\s"',;\\]+)`),
		repl: func(m []string) string {
			out := m[1] + m[2]
			if m[3] != "" {
				out += m[3] + " "
			}
			return out + Placeholder
		}},
	// CLI flags that carry a secret value: --db-password x, --token=x, --value x.
	{name: "cli_flag", triggers: []string{"--"}, window: 4096,
		re: regexp.MustCompile(`^(--(?i:[a-z0-9-]*(?:password|passwd|secret|token|api-key|apikey|access-key|private-key|service-role-key|credentials?)|value|anon-key|service-key))([ \t]+|=)("[^"\n]*"|'[^'\n]*'|[^\s"']+)`),
		repl: func(m []string) string {
			if strings.HasPrefix(m[3], "-") || m[3] == Placeholder {
				return m[0] // the next token is another flag, or already redacted
			}
			return m[1] + m[2] + Placeholder
		}},
}

// String redacts secret-shaped substrings in s. It is idempotent.
func String(s string) string {
	if len(s) < 8 {
		return s
	}
	lower := asciiLower(s)
	for i := range tokenRules {
		if out := tokenRules[i].apply(s, lower); out != s {
			s, lower = out, asciiLower(out)
		}
	}
	if strings.Contains(s, "://") {
		if out := redactURLUserinfo(s); out != s {
			s, lower = out, asciiLower(out)
		}
	}
	for i := range contextRules {
		if out := contextRules[i].apply(s, lower); out != s {
			s, lower = out, asciiLower(out)
		}
	}
	if hasAny(lower, secretKeyTriggers) {
		s = redactKeyValues(s)
	}
	return s
}

// apply runs the rule at every trigger position, left to right.
func (r *anchored) apply(s, lower string) string {
	if !hasAny(lower, r.triggers) {
		return s
	}
	var b strings.Builder
	last, pos := 0, 0
	for {
		// The next trigger occurrence at or after pos.
		next := -1
		for _, t := range r.triggers {
			if j := strings.Index(lower[pos:], t); j >= 0 && (next < 0 || pos+j < next) {
				next = pos + j
			}
		}
		if next < 0 {
			break
		}
		end := min(len(s), next+r.window)
		loc := r.re.FindStringSubmatchIndex(s[next:end])
		if loc == nil {
			pos = next + 1
			continue
		}
		var repl string
		if r.repl == nil {
			repl = Placeholder
		} else {
			m := make([]string, len(loc)/2)
			for g := range m {
				if loc[2*g] >= 0 {
					m[g] = s[next+loc[2*g] : next+loc[2*g+1]]
				}
			}
			repl = r.repl(m)
		}
		if b.Cap() == 0 {
			b.Grow(len(s))
		}
		b.WriteString(s[last:next])
		b.WriteString(repl)
		last = next + loc[1]
		pos = max(last, next+1)
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// dbSchemes may carry a raw '/' in the password, so their userinfo runs to the
// last '@' of the authority rather than stopping at the first '/'.
var dbSchemes = map[string]bool{
	"postgres": true, "postgresql": true, "mysql": true, "mariadb": true, "mongodb": true,
	"mongodb+srv": true, "redis": true, "rediss": true, "amqp": true, "amqps": true,
	"mssql": true, "sqlserver": true, "cockroachdb": true, "clickhouse": true,
}

// redactURLUserinfo turns scheme://user:pass@host into scheme://[REDACTED]@host.
func redactURLUserinfo(s string) string {
	var b strings.Builder
	last, pos := 0, 0
	for {
		j := strings.Index(s[pos:], "://")
		if j < 0 {
			break
		}
		sep := pos + j
		pos = sep + 3
		// The scheme: letters, digits, + . - before "://", starting with a letter.
		k := sep
		for k > 0 && sep-k < 32 && isSchemeByte(s[k-1]) {
			k--
		}
		for k < sep && !isLetter(s[k]) {
			k++
		}
		if k == sep {
			continue
		}
		scheme := asciiLower(s[k:sep])
		start := sep + 3
		// The candidate region ends at whitespace, a quote or a bracket.
		end := start
		for end < len(s) && !isURLStop(s[end]) {
			end++
		}
		region := s[start:end]
		firstAt := strings.IndexByte(region, '@')
		if firstAt <= 0 {
			continue
		}
		if !dbSchemes[scheme] && strings.ContainsAny(region[:firstAt], "/?#") {
			continue // the '@' is in the path, not in the userinfo
		}
		authEnd := len(region)
		if x := strings.IndexAny(region[firstAt:], "/?#"); x >= 0 {
			authEnd = firstAt + x
		}
		at := strings.LastIndexByte(region[:authEnd], '@')
		if region[:at] == Placeholder {
			continue
		}
		if b.Cap() == 0 {
			b.Grow(len(s))
		}
		b.WriteString(s[last:start])
		b.WriteString(Placeholder)
		last = start + at
		pos = last
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func isSchemeByte(c byte) bool {
	return isLetter(c) || (c >= '0' && c <= '9') || c == '+' || c == '.' || c == '-'
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isURLStop(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', '`', '<', '>', '(', ')', '[', ']', '{', '}', '\\', ',':
		return true
	}
	return false
}

// secretKeyTriggers are lowercase substrings that must appear somewhere in a
// text before the KEY=value scanner is worth running.
var secretKeyTriggers = []string{
	"pass", "secret", "token", "key", "credential", "authorization", "jwt", "dsn",
	"claim", "pwd", "connection",
}

// redactKeyValues replaces the values that follow secret-looking keys in
// KEY=value, key: value and "key": "value" forms (.env lines, YAML, JSON text
// inside strings, query strings). It finds each ':' or '=' and looks back for
// the key, so a non-secret key never hides a later secret one.
func redactKeyValues(s string) string {
	var b strings.Builder
	last, pos := 0, 0
	for pos < len(s) {
		j := strings.IndexAny(s[pos:], ":=")
		if j < 0 {
			break
		}
		sep := pos + j
		pos = sep + 1
		key, ok := keyBefore(s, sep)
		if !ok || !IsSecretKey(key) {
			continue
		}
		// Skip spaces and an optional (escaped) opening quote after the separator.
		v := sep + 1
		for v < len(s) && (s[v] == ' ' || s[v] == '\t') {
			v++
		}
		vs, ve := valueSpan(s, v)
		if ve <= vs || vs < last {
			continue
		}
		val := s[vs:ve]
		if val == Placeholder || val[0] == '$' || val[0] == '=' || val[0] == '/' || isAuthScheme(val) {
			continue
		}
		if b.Cap() == 0 {
			b.Grow(len(s))
		}
		b.WriteString(s[last:vs])
		b.WriteString(Placeholder)
		last = ve
		pos = ve
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// keyBefore returns the key name that ends just before the separator at sep,
// skipping spaces, one closing quote and an escaping backslash.
func keyBefore(s string, sep int) (string, bool) {
	e := sep
	for e > 0 && (s[e-1] == ' ' || s[e-1] == '\t') {
		e--
	}
	if e > 0 && (s[e-1] == '"' || s[e-1] == '\'') {
		e--
		if e > 0 && s[e-1] == '\\' {
			e--
		}
	}
	k := e
	for k > 0 && e-k < 100 && isKeyByte(s[k-1]) {
		k--
	}
	for k < e && !(isLetter(s[k]) || s[k] == '_') {
		k++
	}
	if k == e {
		return "", false
	}
	return s[k:e], true
}

func isKeyByte(c byte) bool {
	return isLetter(c) || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-'
}

// valueSpan returns the bounds of the value starting at i, without quotes.
func valueSpan(s string, i int) (start, end int) {
	if i >= len(s) {
		return i, i
	}
	switch {
	case strings.HasPrefix(s[i:], Placeholder):
		return i, i + len(Placeholder)
	case s[i] == '\\' && i+1 < len(s) && s[i+1] == '"': // escaped JSON: \"value\"
		if j := strings.Index(s[i+2:], `\"`); j >= 0 && !strings.Contains(s[i+2:i+2+j], "\n") {
			return i + 2, i + 2 + j
		}
		return i, i
	case s[i] == '"' || s[i] == '\'':
		if j := strings.IndexByte(s[i+1:], s[i]); j >= 0 && !strings.Contains(s[i+1:i+1+j], "\n") {
			return i + 1, i + 1 + j
		}
		return i, i
	}
	j := i
	for j < len(s) && !isValueStop(s[j]) {
		j++
	}
	return i, j
}

func isValueStop(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '"', '\'', ',', ';', '&', '|', '}', ')', ']', '\\':
		return true
	}
	return false
}

func isAuthScheme(v string) bool {
	switch asciiLower(v) {
	case "bearer", "basic", "token", "digest":
		return true
	}
	return false
}

// secretKeyParts are substrings of a normalized key name (lowercase, '-', '.'
// and ' ' turned into '_') that mark its value as secret.
var secretKeyParts = []string{
	"password", "passwd", "passphrase", "secret", "token", "apikey", "api_key",
	"accesskey", "access_key", "privatekey", "private_key", "service_role",
	"servicerole", "credential", "authorization", "jwt", "signing_key",
	"encryption_key", "session_key", "claim_url", "claimurl", "connection_string",
	"connectionstring", "dsn",
}

// notSecretKeys contain a secret part (or end in _key) but hold counts,
// types or non-secret names.
var notSecretKeys = map[string]bool{
	"token_type": true, "tokentype": true, "token_count": true, "tokencount": true,
	"max_tokens": true, "maxtokens": true, "jwt_expiry": true, "jwt_exp": true,
	"primary_key": true, "foreign_key": true, "sort_key": true, "partition_key": true,
	"cache_key": true, "idempotency_key": true, "public_key": true, "dedupe_key": true,
	"unique_key": true,
}

// IsSecretKey reports whether a JSON key, env var name or config key names a
// secret value (password, token, api_key, private_key, service_role, ...).
func IsSecretKey(k string) bool {
	if k == "" || len(k) > 100 {
		return false
	}
	n := normalizeKey(k)
	if notSecretKeys[n] {
		return false
	}
	// Usage counters: input_tokens, totalTokens, cache_read_input_tokens, ...
	if strings.HasSuffix(n, "tokens") || strings.Contains(n, "tokens_") {
		return false
	}
	for _, p := range secretKeyParts {
		if strings.Contains(n, p) {
			return true
		}
	}
	// FOO_KEY, anon_key, stripe_key and *_pwd.
	return strings.HasSuffix(n, "_key") || strings.HasSuffix(n, "_pwd")
}

func normalizeKey(k string) string {
	var buf [100]byte
	b := buf[:0]
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		case c == '-' || c == '.' || c == ' ':
			c = '_'
		}
		b = append(b, c)
	}
	return string(b)
}

// asciiLower lowercases ASCII letters only, so byte offsets stay aligned with
// the original string (strings.ToLower can change lengths for some runes).
func asciiLower(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func hasAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Value returns a deep copy of decoded JSON with every string redacted and the
// values of secret-looking keys replaced. Booleans and nulls under secret keys
// are kept (they carry no secret); everything else becomes Placeholder.
func Value(v any) any {
	switch t := v.(type) {
	case string:
		return String(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		// {"name": "SUPABASE_SERVICE_ROLE_KEY", "value": "..."}: env var listings.
		pairSecret := false
		for _, nk := range []string{"key", "name"} {
			if s, ok := t[nk].(string); ok && IsSecretKey(s) {
				pairSecret = true
			}
		}
		for k, val := range t {
			if IsSecretKey(k) || (pairSecret && (k == "value" || k == "decrypted_value")) {
				out[k] = redactWhole(val)
				continue
			}
			out[k] = Value(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = Value(e)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsSecretKey(k) {
				out[k] = redactWhole(val)
			} else {
				out[k] = String(val)
			}
		}
		return out
	case []string:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = String(e)
		}
		return out
	case json.RawMessage:
		var d any
		if err := json.Unmarshal(t, &d); err != nil {
			return String(string(t))
		}
		return Value(d)
	default:
		return v // numbers, bools, nil
	}
}

func redactWhole(v any) any {
	switch t := v.(type) {
	case nil, bool:
		return v
	case string:
		if t == "" || t == Placeholder {
			return t
		}
		return Placeholder
	default:
		return Placeholder
	}
}
