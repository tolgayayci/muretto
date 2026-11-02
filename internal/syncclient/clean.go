package syncclient

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/redact"
)

// Excerpt caps, tighter than the schema's: sync carries short evidence only.
const (
	maxExcerpt = 1000
	maxText    = 1000
)

var (
	// Home-directory paths (macOS, Linux, Windows) become "~".
	homePath = regexp.MustCompile(`(?i)(/Users|/home)/[^/\s"'` + "`" + `]+|[a-z]:\\Users\\[^\\\s"']+|/root\b`)
	// Muretto's own tokens, in case the redactor ever misses one.
	mrtToken = regexp.MustCompile(`\b(mrt|mrs|mri)_[a-z2-7]{16,}`)
	// Attribute keys whose values are never sent.
	secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api_?key|private_?key|authorization|credential|cookie|connection_?string|dsn)`)
)

// clean redacts secrets, removes home-directory paths and caps the length.
func clean(s string, n int) string {
	s = redact.String(s)
	s = mrtToken.ReplaceAllString(s, redact.Placeholder)
	s = homePath.ReplaceAllString(s, "~")
	return clip(s, n)
}

func cleanPtr(s *string, n int) *string {
	if s == nil {
		return nil
	}
	v := clean(*s, n)
	return &v
}

// clip truncates to n runes and drops invalid UTF-8.
func clip(s string, n int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, "file:") ||
		strings.HasPrefix(s, `\\`) || (len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/'))
}

// cleanURL drops local paths, credentials, query strings and fragments.
func cleanURL(s *string, n int) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" || isLocalPath(v) {
		return nil
	}
	if u, err := url.Parse(v); err == nil && u.Scheme != "" && u.Host != "" {
		u.User, u.RawQuery, u.Fragment, u.ForceQuery = nil, "", "", false
		v = u.String()
	} else if at := strings.Index(v, "@"); at >= 0 && strings.Contains(v[at:], ":") && !strings.Contains(v[:at], "/") {
		v = v[at+1:] // scp-style git@host:org/repo -> host:org/repo
	}
	out := clean(v, n)
	return &out
}

func cleanDeveloper(d contract.Developer) contract.Developer {
	d.DisplayName = clean(d.DisplayName, 100)
	if d.DisplayName == "" {
		d.DisplayName = "developer"
	}
	d.AccountLogin = cleanPtr(d.AccountLogin, 100)
	return d
}

func cleanSession(s contract.Session) contract.Session {
	s.TaskTitle = cleanPtr(s.TaskTitle, 80) // short title only, never the prompt
	s.GitBranch = cleanPtr(s.GitBranch, 250)
	s.Model = cleanPtr(s.Model, 100)
	s.AgentVersion = cleanPtr(s.AgentVersion, 50)
	s.EndReason = cleanPtr(s.EndReason, 50)
	if s.Repo != nil {
		r := *s.Repo
		r.RemoteUrl = cleanURL(r.RemoteUrl, 500)
		if r.DisplayName != nil && isLocalPath(*r.DisplayName) {
			base := path.Base(strings.ReplaceAll(*r.DisplayName, `\`, "/"))
			r.DisplayName = &base
		}
		r.DisplayName = cleanPtr(r.DisplayName, 200)
		s.Repo = &r
	}
	if s.Sources == nil {
		s.Sources = []contract.SessionSourcesElem{}
	}
	return s
}

func cleanResource(r contract.Resource) contract.Resource {
	r.Name = clean(r.Name, 300)
	r.AccountName = cleanPtr(r.AccountName, 200)
	r.Url = cleanURL(r.Url, 1000)
	r.Status = cleanPtr(r.Status, 50)
	attrs := contract.ResourceAttributes{}
	src := map[string]any(r.Attributes)
	if v, ok := redact.Value(src).(map[string]any); ok {
		src = v
	}
	for k, v := range src {
		switch {
		case secretKey.MatchString(k):
			attrs[k] = redact.Placeholder
		case v == nil:
			attrs[k] = nil
		default:
			switch x := v.(type) {
			case string:
				attrs[k] = clean(x, 1000)
			case bool, float64, float32, int, int64, int32, uint, uint64, uint32:
				attrs[k] = x
			default: // nested values are not allowed by the schema
				continue
			}
		}
	}
	r.Attributes = attrs
	return r
}

func cleanAttribution(a contract.Attribution) contract.Attribution {
	a.Reason = clean(a.Reason, 500)
	ev := make([]contract.Evidence, 0, len(a.Evidence))
	for _, e := range a.Evidence {
		e.ToolName = clean(e.ToolName, 200)
		e.MatchedValue = clean(e.MatchedValue, 300)
		e.Excerpt = clean(e.Excerpt, maxExcerpt)
		ev = append(ev, e)
	}
	a.Evidence = ev
	return a
}

func cleanFinding(f contract.ExposureFinding) contract.ExposureFinding {
	f.Title = clean(f.Title, 200)
	f.Explanation = clean(f.Explanation, maxText)
	f.Caveat = cleanPtr(f.Caveat, 500)
	return f
}

func cleanCost(c contract.CostRecord) contract.CostRecord {
	c.Basis = clean(c.Basis, 300)
	return c
}

func cleanReview(r contract.ReviewItem) contract.ReviewItem {
	r.Note = cleanPtr(r.Note, 500)
	return r
}

func cleanCheck(c contract.ProviderCheck) contract.ProviderCheck {
	c.Error = cleanPtr(c.Error, 500)
	c.Scope = cleanPtr(c.Scope, 300)
	return c
}
