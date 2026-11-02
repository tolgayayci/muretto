package adaptergen

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// snake turns camelCase, kebab-case and other spellings into snake_case.
func snake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) ||
				(i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('_')
		}
	}
	out := regexp.MustCompile(`_+`).ReplaceAllString(b.String(), "_")
	return strings.Trim(out, "_")
}

// ident makes a name valid for kinds, rules and attributes: ^[a-z][a-z0-9_]*$.
func ident(s string) string {
	s = snake(s)
	if s == "" {
		return "item"
	}
	if s[0] < 'a' || s[0] > 'z' {
		s = "k_" + s
	}
	return s
}

var irregular = map[string]string{
	"aliases": "alias", "statuses": "status", "buses": "bus", "indices": "index", "indexes": "index",
	"people": "person", "children": "child", "analyses": "analysis", "data": "data", "metadata": "metadata",
	"series": "series", "species": "species", "news": "news", "settings": "settings", "access": "access",
	"addresses": "address", "processes": "process", "caches": "cache", "databases": "database",
	"responses": "response", "releases": "release", "leases": "lease", "licenses": "license",
	"vertices": "vertex", "matrices": "matrix", "policies": "policy", "branches": "branch",
	"postgres": "postgres", "redis": "redis", "dns": "dns", "kubernetes": "kubernetes", "analytics": "analytics",
	"metrics": "metrics", "credentials": "credential", "aws": "aws", "gcs": "gcs", "s3": "s3",
}

// singular returns the singular of the last word of a snake_case name.
func singular(s string) string {
	words := strings.Split(s, "_")
	w := words[len(words)-1]
	switch {
	case irregular[w] != "":
		w = irregular[w]
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		w = w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "sses"), strings.HasSuffix(w, "shes"), strings.HasSuffix(w, "ches"),
		strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "zzes"):
		w = w[:len(w)-2]
	case strings.HasSuffix(w, "ss"), strings.HasSuffix(w, "us"), strings.HasSuffix(w, "is"):
	case strings.HasSuffix(w, "s") && len(w) > 2:
		w = w[:len(w)-1]
	}
	words[len(words)-1] = w
	return strings.Join(words, "_")
}

var reVersionSeg = regexp.MustCompile(`^v\d+(?:(?:alpha|beta|preview)\d*)?$`)

func isVersionSeg(s string) bool { return reVersionSeg.MatchString(strings.ToLower(s)) }

func versionOf(s string) int {
	s = strings.ToLower(s)
	if !isVersionSeg(s) {
		return 0
	}
	n := 0
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func isParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") && len(seg) > 2
}

func paramName(seg string) string { return seg[1 : len(seg)-1] }

func segments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normKey is a path with version segments dropped and params as {}, so
// /v9/projects/{idOrName}/domains and /v10/projects/{id}/domains compare equal.
func normKey(segs []string) string {
	var b strings.Builder
	for _, s := range segs {
		if isVersionSeg(s) {
			continue
		}
		b.WriteByte('/')
		if isParam(s) {
			b.WriteString("{}")
		} else {
			b.WriteString(strings.ToLower(s))
		}
	}
	return b.String()
}

// versionScore orders path versions: v10 > v9 > v9beta1.
func versionScore(segs []string) int {
	best := 0
	for _, s := range segs {
		if !isVersionSeg(s) {
			continue
		}
		v := versionOf(s) * 10
		if regexp.MustCompile(`^v\d+$`).MatchString(strings.ToLower(s)) {
			v += 5
		}
		if v > best {
			best = v
		}
	}
	return best
}

var reSlugBad = regexp.MustCompile(`[^a-z0-9]+`)

var slugStop = map[string]bool{
	"api": true, "apis": true, "rest": true, "openapi": true, "swagger": true, "public": true,
	"http": true, "spec": true, "specification": true, "service": true, "the": true, "reference": true,
	"management": true, "platform": true, "documentation": true, "docs": true, "server": true, "official": true,
}

// providerSlug derives ^[a-z][a-z0-9-]{1,31}$ from a title, or else a host.
func providerSlug(title, host string) string {
	var words []string
	for _, w := range strings.Fields(reSlugBad.ReplaceAllString(strings.ToLower(title), " ")) {
		if slugStop[w] || isVersionSeg(w) || isNumber(w) {
			continue
		}
		words = append(words, w)
	}
	s := strings.Join(words, "-")
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		// The registered domain's label: api.acme.dev -> acme,
		// us.sentry.io -> sentry, api.acme.co.uk -> acme.
		labels := strings.Split(host, ".")
		s = ""
		for i := len(labels) - 2; i >= 0; i-- {
			l := labels[i]
			if (l == "co" || l == "com" || l == "org" || l == "net" || l == "gov" || l == "ac") && i > 0 {
				continue
			}
			s = reSlugBad.ReplaceAllString(l, "-")
			break
		}
	}
	s = strings.Trim(s, "-")
	if len(s) > 32 {
		s = strings.Trim(s[:32], "-")
	}
	if len(s) < 2 || s[0] < 'a' || s[0] > 'z' {
		return ""
	}
	return s
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// displayName cleans a spec title ("Acme REST API v2" -> "Acme").
func displayName(title, slug string) string {
	t := strings.TrimSpace(title)
	re := regexp.MustCompile(`(?i)\s*(\(.*\)|\bv\d+(\.\d+)*\b|\b(rest|public|management|platform|http)?\s*api\b|\bopenapi\b|\bspec(ification)?\b)\s*`)
	for i := 0; i < 3; i++ {
		t = strings.TrimSpace(re.ReplaceAllString(t, " "))
	}
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		t = strings.ToUpper(slug[:1]) + slug[1:]
	}
	if len(t) > 60 {
		t = t[:60]
	}
	return t
}

// envName is the shell variable a printed curl command reads the token from.
func envName(slug string) string {
	return strings.ToUpper(strings.ReplaceAll(slug, "-", "_")) + "_TOKEN"
}
