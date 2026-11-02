package adapters

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// secretPatterns flag anything secret-shaped. They are assembled at run time
// so this file itself never contains a secret-shaped literal.
var secretPatterns = func() map[string]*regexp.Regexp {
	j := "ey" + "J"
	return map[string]*regexp.Regexp{
		"JWT":                  regexp.MustCompile(j + `[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.`),
		"Supabase PAT":         regexp.MustCompile(`\b` + "sb" + `p_[A-Za-z0-9]`),
		"Supabase secret key":  regexp.MustCompile(`\b` + "sb_" + `secret_`),
		"Vercel token":         regexp.MustCompile(`\b` + "vc" + `[a-z]_[A-Za-z0-9]{16,}`),
		"GitHub token":         regexp.MustCompile(`\b(?:` + "gh" + `[pousr]_[A-Za-z0-9]{16,}|` + "github" + `_pat_)`),
		"URL with credentials": regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s/:@"'<>]+:[^\s/@"'<>]+@`),
		"private key":          regexp.MustCompile(`-----BEGIN [A-Z ]*` + "PRIVATE" + ` KEY-----`),
		"email":                regexp.MustCompile(`[A-Za-z0-9._%+-]+@(?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,}`),
	}
}()

// findSecrets returns a description of every secret-shaped match in data.
// Emails are allowed only at example.com.
func findSecrets(data string) []string {
	var out []string
	for name, re := range secretPatterns {
		for _, m := range re.FindAllString(data, -1) {
			if name == "email" && strings.HasSuffix(strings.ToLower(m), "@example.com") {
				continue
			}
			out = append(out, name+": "+redactMatch(m))
		}
	}
	return out
}

func redactMatch(m string) string {
	if len(m) > 8 {
		return m[:4] + "..."
	}
	return m
}

// TestFixturesHaveNoSecrets scans every adapter file and fixture. Markdown
// (research notes that name token prefixes) is left to the repo's secret
// scanner.
func TestFixturesHaveNoSecrets(t *testing.T) {
	root := filepath.Join("..", "..", "adapters")
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) == ".md" {
			return err
		}
		n++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, hit := range findSecrets(string(data)) {
			t.Errorf("%s: %s", path, hit)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("only %d files scanned", n)
	}
}

// The detector must catch every shape it claims to.
func TestSecretPatternsCatch(t *testing.T) {
	samples := []string{
		"ey" + "J" + "hbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"sb" + "p_" + strings.Repeat("0a", 20),
		"sb_" + "secret_" + strings.Repeat("x", 20),
		"vc" + "p_" + strings.Repeat("A1", 12),
		"gh" + "p_" + strings.Repeat("B2", 18),
		"github" + "_pat_" + strings.Repeat("C", 30),
		"postgres://" + "user" + ":" + "pw" + "@db.example.com:5432/x",
		"-----BEGIN " + "RSA " + "PRIVATE" + " KEY-----",
		"someone" + "@" + "gmail.com",
	}
	for _, s := range samples {
		if len(findSecrets(s)) == 0 {
			t.Errorf("not caught: %s", redactMatch(s))
		}
	}
	for _, ok := range []string{"dev@example.com", "vercel@claude-plugins-official", "https://supabase.com/dashboard/project/abcdefghijklmnopqrst"} {
		if hits := findSecrets(ok); len(hits) != 0 {
			t.Errorf("false positive on %q: %v", ok, hits)
		}
	}
}
