package redact

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fake builds an obviously fake secret body at runtime, so no secret-shaped
// literal ever lands in the repository (and secret scanners stay quiet).
func fake(prefix string, n int) string {
	const alphabet = "FAKEfake0123456789XYZxyz"
	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[(i*7+3)%len(alphabet)])
	}
	return b.String()
}

func fakeJWT() string {
	return "eyJ" + fake("", 20) + ".eyJ" + fake("", 30) + "." + fake("", 43)
}

func TestStringRedacts(t *testing.T) {
	sbp := fake("sbp_", 40)
	jwt := fakeJWT()
	pw := fake("pw", 12)
	tests := []struct {
		name    string
		in      string
		secrets []string // must not appear in the output
		keep    []string // must still appear
	}{
		{"supabase pat", "export SUPABASE_ACCESS_TOKEN " + sbp, []string{sbp}, []string{"SUPABASE_ACCESS_TOKEN"}},
		{"supabase pat in sentence", "using token " + sbp + " now", []string{sbp}, []string{"using token", "now"}},
		{"supabase secret key", "key=" + fake("sb_secret_", 32), []string{fake("sb_secret_", 32)}, nil},
		{"supabase publishable key", "pub " + fake("sb_publishable_", 32), []string{fake("sb_publishable_", 32)}, []string{"pub "}},
		{"github classic", "token " + fake("ghp_", 36), []string{fake("ghp_", 36)}, nil},
		{"github oauth", fake("gho_", 36), []string{fake("gho_", 36)}, nil},
		{"github user-to-server", fake("ghu_", 36), []string{fake("ghu_", 36)}, nil},
		{"github server-to-server", fake("ghs_", 36), []string{fake("ghs_", 36)}, nil},
		{"github fine-grained", fake("github_pat_", 82), []string{fake("github_pat_", 82)}, nil},
		{"vercel token", "vercel --token " + fake("vcp_", 24) + " deploy", []string{fake("vcp_", 24)}, []string{"vercel --token", "deploy"}},
		{"vercel token bare", "tok " + fake("vcp_", 24), []string{fake("vcp_", 24)}, nil},
		{"jwt", "anon key: " + jwt, []string{jwt}, nil},
		{"jwt alone", jwt, []string{jwt}, nil},
		{"stripe live", fake("sk_live_", 24), []string{fake("sk_live_", 24)}, nil},
		{"stripe test", "STRIPE " + fake("sk_test_", 24), []string{fake("sk_test_", 24)}, nil},
		{"stripe restricted", fake("rk_live_", 24), []string{fake("rk_live_", 24)}, nil},
		{"aws access key", "id AKIA" + strings.ToUpper(fake("", 16)), []string{"AKIA" + strings.ToUpper(fake("", 16))}, []string{"id "}},
		{"aws secret env", "AWS_SECRET_ACCESS_KEY=" + fake("", 40), []string{fake("", 40)}, []string{"AWS_SECRET_ACCESS_KEY="}},
		{"postgres url", "postgres://postgres:" + pw + "@db.abcdefghijklmnopqrst.supabase.co:5432/postgres",
			[]string{pw, "postgres:" + pw}, []string{"postgres://", "@db.abcdefghijklmnopqrst.supabase.co:5432/postgres"}},
		{"postgresql url with slash in password", "postgresql://u:" + pw + "/x@host/db", []string{pw}, []string{"postgresql://", "@host/db"}},
		{"pooler url", "postgresql://postgres.abcdefghijklmnopqrst:" + pw + "@aws-0-eu-west-1.pooler.supabase.com:6543/postgres",
			[]string{pw}, []string{"@aws-0-eu-west-1.pooler.supabase.com:6543/postgres"}},
		{"https userinfo", "git clone https://x-access-token:" + pw + "@github.com/acme/web.git", []string{pw}, []string{"https://", "@github.com/acme/web.git"}},
		{"https token user", "https://" + pw + "@github.com/acme/web", []string{pw}, []string{"@github.com/acme/web"}},
		{"authorization bearer", `curl -H "Authorization: Bearer ` + pw + `" https://api.example.com`, []string{pw}, []string{"Authorization: Bearer", "https://api.example.com"}},
		{"authorization json", `{"Authorization": "Basic ` + pw + `"}`, []string{pw}, []string{"Authorization"}},
		{"apikey header", `-H 'apikey: ` + pw + `'`, []string{pw}, []string{"apikey"}},
		{"x-api-key header", `x-api-key=` + pw, []string{pw}, []string{"x-api-key"}},
		{"env line", "DB_PASSWORD=" + pw + "\nNEXT_PUBLIC_URL=https://acme.dev", []string{pw}, []string{"DB_PASSWORD=", "NEXT_PUBLIC_URL=https://acme.dev"}},
		{"env line export quoted", `export SERVICE_ROLE_KEY="` + pw + `"`, []string{pw}, []string{"SERVICE_ROLE_KEY"}},
		{"env inline before command", "SUPABASE_DB_PASSWORD=" + pw + " supabase db push", []string{pw}, []string{"supabase db push"}},
		{"json secret key in text", `{"service_role_key":"` + pw + `","ref":"abcdefghijklmnopqrst"}`, []string{pw}, []string{"abcdefghijklmnopqrst"}},
		{"escaped json", `curl -d "{\"password\":\"` + pw + `\"}"`, []string{pw}, []string{"password"}},
		{"yaml", "client_secret: " + pw, []string{pw}, []string{"client_secret:"}},
		{"query string", "https://x.example/cb?access_token=" + pw + "&state=ok", []string{pw}, []string{"state=ok", "https://x.example/cb?"}},
		{"query after url key", "url: https://x.example/?token=" + pw, []string{pw}, nil},
		{"pem", "-----BEGIN RSA PRIVATE " + "KEY-----\n" + fake("", 64) + "\n" + fake("", 64) + "\n-----END RSA PRIVATE " + "KEY-----\nafter",
			[]string{fake("", 64)}, []string{"after"}},
		{"pem truncated", "key:\n-----BEGIN OPENSSH PRIVATE " + "KEY-----\n" + fake("", 64), []string{fake("", 64)}, nil},
		{"flag db-password", "supabase link --project-ref abcdefghijklmnopqrst --db-password " + pw, []string{pw}, []string{"--project-ref abcdefghijklmnopqrst", "--db-password"}},
		{"flag token equals", "vercel deploy --token=" + pw, []string{pw}, []string{"--token="}},
		{"flag value", "vercel env add SECRET_X production --value " + pw + " --yes", []string{pw}, []string{"--value", "--yes", "production"}},
		{"flag value quoted", `--value "` + pw + ` two"`, []string{pw}, nil},
		{"muretto token", fake("mrt_", 32), []string{fake("mrt_", 32)}, nil},
		{"anthropic key", fake("sk-ant-api03-", 40), []string{fake("sk-ant-api03-", 40)}, nil},
		{"vercel claim url key", `"claimUrl": "https://vercel.com/claim/` + pw + `"`, []string{pw}, nil},
		{"camelCase json", `{"accessToken":"` + pw + `"}`, []string{pw}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := String(tc.in)
			for _, s := range tc.secrets {
				if strings.Contains(got, s) {
					t.Errorf("secret %q survived:\n in: %s\nout: %s", s, tc.in, got)
				}
			}
			for _, k := range tc.keep {
				if !strings.Contains(got, k) {
					t.Errorf("lost %q:\n in: %s\nout: %s", k, tc.in, got)
				}
			}
			if !strings.Contains(got, Placeholder) {
				t.Errorf("no placeholder in %q", got)
			}
			if again := String(got); again != got {
				t.Errorf("not idempotent:\n1: %s\n2: %s", got, again)
			}
		})
	}
}

// TestStringReviewGaps covers the inputs an independent review found passing
// through unchanged (or partly unchanged).
func TestStringReviewGaps(t *testing.T) {
	pw := fake("pw", 12)
	tok := fake("tk", 24)
	host := "@db.abcdefghijklmnopqrst.supabase.co:5432/postgres"
	esc2 := `\\\"` // a quote escaped twice: JSON text inside a JSON string inside a string
	tests := []struct {
		name    string
		in      string
		secrets []string
		keep    []string
	}{
		{"DB_PASS", "DB_PASS=" + pw, []string{pw}, []string{"DB_PASS="}},
		{"export SMTP_PASS quoted", "export SMTP_PASS='" + pw + "'", []string{pw}, []string{"export SMTP_PASS="}},
		{"REDIS_PASS", "REDIS_PASS=" + pw + " redis-cli ping", []string{pw}, []string{"redis-cli ping"}},
		{"camelCase dbPass", `{"dbPass":"` + pw + `"}`, []string{pw}, []string{"dbPass"}},
		{"passwd key", "passwd=" + pw, []string{pw}, nil},
		{"url password with comma", "postgresql://postgres:" + pw + ",x" + host, []string{pw, ",x"}, []string{"postgresql://", host}},
		{"url password with parens", "postgresql://postgres:a(" + pw + ")b" + host, []string{pw, ")b"}, []string{host}},
		{"url password with braces", "postgresql://postgres:a{" + pw + "}b" + host, []string{pw, "}b"}, []string{host}},
		{"url password with brackets", "postgresql://postgres:a[" + pw + "]b" + host, []string{pw, "]b"}, []string{host}},
		{"url password with at slash hash", "postgresql://postgres:p@" + pw + "/w#rd" + host, []string{pw, "w#rd"}, []string{host}},
		{"url password percent-encoded", "postgresql://postgres:p%40" + pw + "@db/postgres", []string{pw}, []string{"@db/postgres"}},
		{"https password with comma", "https://u:" + pw + ",x@example.com/x", []string{pw}, []string{"@example.com/x"}},
		{"db-url flag", "supabase db push --db-url postgresql://postgres:" + pw + ",x@h/db", []string{pw}, []string{"--db-url postgresql://", "@h/db"}},
		{"curl -u", "curl -u admin:" + pw + " https://x.example", []string{pw}, []string{"-u admin:", "https://x.example"}},
		{"curl -u attached", "curl -uadmin:" + pw + " https://x.example", []string{pw}, []string{"-uadmin:"}},
		{"curl --user", "curl --user admin:" + pw + " https://x.example", []string{pw}, []string{"--user admin:"}},
		{"curl --user=", "curl --user='admin:" + pw + "' https://x.example", []string{pw}, []string{"--user='admin:"}},
		{"mysql -p attached", "mysql -uroot -p" + pw + " db", []string{pw}, []string{"mysql -uroot -p", " db"}},
		{"mysqldump -p quoted", "mysqldump -u root -p'" + pw + "' db", []string{pw}, []string{"mysqldump -u root -p"}},
		{"vercel -t", "vercel deploy -t " + tok, []string{tok}, []string{"vercel deploy -t "}},
		{"vercel -t prod", "vercel deploy --prod -t " + tok + " --yes", []string{tok}, []string{"--yes"}},
		{"double-escaped json", `{` + esc2 + `password` + esc2 + `:` + esc2 + pw + esc2 + `}`, []string{pw}, []string{"password"}},
		{"double-escaped db_pass", `[{` + esc2 + `db_pass` + esc2 + `:` + esc2 + pw + esc2 + `}]`, []string{pw}, []string{"db_pass"}},
		{"escaped quote inside value", `{"password":"a\"` + pw + `"}`, []string{pw}, []string{"password"}},
		{"python dict list value", "{'Authorization': ['Bearer " + tok + "']}", []string{tok}, []string{"'Authorization'"}},
		{"python dict secret list", "{'password': ['" + pw + "', 'x" + pw + "']}", []string{pw}, []string{"'password'"}},
		{"python dict single-quoted", "{'client_secret': '" + pw + "'}", []string{pw}, []string{"'client_secret'"}},
		{"yaml value with spaces", "password: correct " + pw + " battery", []string{pw, "battery"}, []string{"password: "}},
		{"yaml value with spaces keeps next line", "password: a " + pw + "\nuser: bob", []string{pw}, []string{"\nuser: bob"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := String(tc.in)
			for _, s := range tc.secrets {
				if strings.Contains(got, s) {
					t.Errorf("secret %q survived:\n in: %s\nout: %s", s, tc.in, got)
				}
			}
			for _, k := range tc.keep {
				if !strings.Contains(got, k) {
					t.Errorf("lost %q:\n in: %s\nout: %s", k, tc.in, got)
				}
			}
			if again := String(got); again != got {
				t.Errorf("not idempotent:\n1: %s\n2: %s", got, again)
			}
		})
	}
	// The same inputs inside decoded JSON (the hook's tool_input).
	for _, tc := range tests {
		enc, _ := json.Marshal(Value(map[string]any{"command": tc.in}))
		for _, s := range tc.secrets {
			if strings.Contains(string(enc), s) {
				t.Errorf("%s: secret %q survived Value: %s", tc.name, s, enc)
			}
		}
	}
}

// The new flag and URL rules must not touch look-alikes that carry no secret.
func TestStringKeepsLookalikes(t *testing.T) {
	keep := []string{
		"docker run -u 1000:1000 alpine",
		"ssh -p22 host",
		"mkdir -p/tmp/x",
		"mysql -p dbname",
		"docker build -t web:latest .",
		"tar -tf archive.tar",
		"https://host.example:8443/path@v2",
		"https://registry.npmjs.org/@scope%2fpkg",
		"psql postgres://localhost:5432/app?sslmode=disable",
		"bypass: true",
	}
	for _, s := range keep {
		if got := String(s); got != s {
			t.Errorf("changed a non-secret:\n in: %s\nout: %s", s, got)
		}
	}
}

// Identifiers attribution depends on must survive untouched.
func TestStringKeepsIdentifiers(t *testing.T) {
	keep := []string{
		"Created a new project at https://supabase.com/dashboard/project/abcdefghijklmnopqrst",
		"project ref: abcdefghijklmnopqrst",
		`{"ref": "zzfakeref000000000000", "name": "demo-waitlist", "status": "ACTIVE_HEALTHY"}`,
		"dpl_FakeDeployment1234567890ab ready",
		`{"id":"prj_FakeProject1234567890abcd","name":"web","accountId":"team_FakeTeam123456789012"}`,
		"session 0b6e3c1a-7f2d-4c55-9a10-3e8d2f4b6a01",
		"toolu_01FAKEbashAAAAAAAAAAAAAAA",
		"https://example-waitlist-abc123def-example-team.vercel.app",
		"supabase functions deploy process-import --project-ref abcdefghijklmnopqrst",
		"CREATE TABLE public.customers (id uuid primary key, name text);",
		"git@github.com:acme/web.git",
		"ssh://git@github.com/acme/web.git",
		"https://github.com/acme/web.git",
		"https://www.npmjs.com/package/@scope/pkg",
		"psql postgres://localhost:5432/app",
		`{"input_tokens":2,"output_tokens":120,"cache_read_input_tokens":2000}`,
		`{"token_type":"bearer","expires_in":3600}`,
		"Authorization required", // no separator: prose
		"password reset email sent",
		"NEXT_PUBLIC_SUPABASE_URL=https://abcdefghijklmnopqrst.supabase.co",
		"sk_ is a prefix", "short",
		"pk_test_ is publishable",
		"echo hello world",
		"--verbose --project-ref abcdefghijklmnopqrst",
		"PWD=/home/dev/acme-app",
	}
	for _, s := range keep {
		if got := String(s); got != s {
			t.Errorf("changed a non-secret:\n in: %s\nout: %s", s, got)
		}
	}
}

func TestIsSecretKey(t *testing.T) {
	yes := []string{"password", "DB_PASSWORD", "db-password", "client_secret", "clientSecret", "token", "access_token",
		"accessToken", "api_key", "apiKey", "x-api-key", "private_key", "privateKey", "service_role", "SUPABASE_SERVICE_ROLE_KEY",
		"authorization", "Authorization", "jwt_secret", "STRIPE_KEY", "anon_key", "credentials", "aws_secret_access_key",
		"claimUrl", "SENTRY_DSN", "passphrase", "db_pwd", "pass", "DB_PASS", "smtp-pass", "dbPass", "passwd"}
	no := []string{"", "name", "ref", "project_ref", "id", "key", "input_tokens", "outputTokens", "totalTokens", "token_type",
		"max_tokens", "primary_key", "public_key", "cwd", "PWD", "author", "status", "region", "session_id", "tool_use_id",
		"description", "command", "bypass", "compass"}
	for _, k := range yes {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false, want true", k)
		}
	}
	for _, k := range no {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true, want false", k)
		}
	}
}

func TestValue(t *testing.T) {
	pw := fake("pw", 12)
	sbp := fake("sbp_", 40)
	var in any
	raw := `{
		"command": "supabase login --token ` + sbp + `",
		"password": "` + pw + `",
		"nested": {"apiKey": "` + pw + `", "ref": "abcdefghijklmnopqrst", "enabled": true},
		"secrets": [{"name": "X", "value": "` + pw + `"}],
		"env": [{"key": "SUPABASE_SERVICE_ROLE_KEY", "value": "` + pw + `"}, {"key": "NEXT_PUBLIC_URL", "value": "https://acme.dev"}],
		"usage": {"input_tokens": 12, "output_tokens": 3},
		"token": null,
		"has_secret": false,
		"list": ["ok", "Bearer token ` + sbp + `", 3]
	}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	orig, _ := json.Marshal(in)
	out := Value(in)
	enc, _ := json.Marshal(out)
	s := string(enc)
	for _, secret := range []string{pw, sbp} {
		if strings.Contains(s, secret) {
			t.Errorf("secret %q survived: %s", secret, s)
		}
	}
	for _, k := range []string{"abcdefghijklmnopqrst", `"enabled":true`, `"input_tokens":12`, `"token":null`, `"has_secret":false`,
		"https://acme.dev", `"key":"SUPABASE_SERVICE_ROLE_KEY"`, "supabase login --token"} {
		if !strings.Contains(s, k) {
			t.Errorf("lost %q: %s", k, s)
		}
	}
	// Deep copy: the input is unchanged.
	after, _ := json.Marshal(in)
	if string(orig) != string(after) {
		t.Error("Value mutated its input")
	}
}

func TestValueScalars(t *testing.T) {
	for _, v := range []any{nil, true, 1.5, json.Number("7")} {
		if got := Value(v); !reflect.DeepEqual(got, v) {
			t.Errorf("Value(%v) = %v", v, got)
		}
	}
	if got := Value([]string{fake("ghp_", 36)}); !reflect.DeepEqual(got, []any{Placeholder}) {
		t.Errorf("[]string not redacted: %v", got)
	}
	if got := Value(map[string]string{"token": "abc", "x": "y"}); !reflect.DeepEqual(got, map[string]any{"token": Placeholder, "x": "y"}) {
		t.Errorf("map[string]string: %v", got)
	}
}

func FuzzString(f *testing.F) {
	f.Add("postgres://u:p@h/db")
	f.Add(`{"password":"x"}`)
	f.Add("-----BEGIN PRIVATE " + "KEY-----")
	f.Add("--token")
	f.Add("curl -u a:b mysql -px vercel -t y")
	f.Add(`{\\\"password\\\":\\\"x\\\"}`)
	f.Add("{'Authorization': ['Bearer x']} password: a b")
	f.Add("postgres://u:a,(b)@h/db")
	f.Fuzz(func(t *testing.T, s string) {
		out := String(s)
		if again := String(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, out, again)
		}
	})
}

func benchInput() string {
	var b strings.Builder
	for b.Len() < 64<<10 {
		b.WriteString(`{"id":"prj_FakeProject1234567890abcd","name":"web","ref":"abcdefghijklmnopqrst","status":"READY","url":"https://web-abc.vercel.app","created":1793016000000}` + "\n")
		b.WriteString("Created a new project at https://supabase.com/dashboard/project/abcdefghijklmnopqrst\n")
	}
	return b.String()
}

func BenchmarkString64KiBClean(b *testing.B) {
	s := benchInput()
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		_ = String(s)
	}
}

func BenchmarkString64KiBSecrets(b *testing.B) {
	s := benchInput() + "DB_PASSWORD=" + fake("", 20) + " " + fakeJWT() + " postgres://u:" + fake("", 10) + "@h/db " + fake("sbp_", 40)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		_ = String(s)
	}
}

func BenchmarkStringShort(b *testing.B) {
	s := "supabase projects create demo --org-id fakeorg --db-password " + fake("", 16) + " --region us-east-1"
	for b.Loop() {
		_ = String(s)
	}
}

func TestStringPercentEncodedTokens(t *testing.T) {
	hex40 := strings.Repeat("a1", 20)
	cases := []string{
		"https://api.example.com/x?token=sbp" + "%5F" + hex40,
		"curl 'https://x.example/?key=sb" + "%5fsecret%5f" + strings.Repeat("Z", 24) + "'",
		"next=ghp" + "%5F" + strings.Repeat("A", 36) + "&page=2",
		// No secret-looking key in front: only the encoded-token rule can catch these.
		"/callback/sbp" + "%5F" + hex40 + "/done",
		"ref=sb" + "%5Fsecret%5F" + strings.Repeat("Z", 24),
	}
	for _, in := range cases {
		out := String(in)
		if strings.Contains(out, hex40) || strings.Contains(out, strings.Repeat("Z", 24)) || strings.Contains(out, strings.Repeat("A", 36)) {
			t.Errorf("String(%q) = %q, token survived", in, out)
		}
		if !strings.Contains(out, Placeholder) {
			t.Errorf("String(%q) = %q, want a placeholder", in, out)
		}
	}
	// Ordinary percent-encoding is left alone.
	keep := "https://example.com/search?q=snake%5Fcase%20names"
	if got := String(keep); got != keep {
		t.Errorf("String(%q) = %q, want unchanged", keep, got)
	}
}
