package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T, config string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "web")
	must(t, os.MkdirAll(filepath.Join(root, ".git"), 0o700))
	must(t, os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o600))
	return root
}

func TestRepoForRemote(t *testing.T) {
	pw := "p" + strings.Repeat("w9", 6)
	root := gitRepo(t, `[core]
	repositoryformatversion = 0
; comment
[remote "upstream"]
	url = https://github.com/other/fork.git
[remote "origin"]
	url = https://alice:`+pw+`@github.com/acme/web.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`)
	sub := filepath.Join(root, "apps", "site")
	must(t, os.MkdirAll(sub, 0o700))
	r := RepoFor(sub, dev)
	if r.RemoteUrl == nil || *r.RemoteUrl != "https://github.com/acme/web.git" || *r.DisplayName != "acme/web" {
		t.Errorf("%+v", r)
	}
	if strings.Contains(*r.RemoteUrl, pw) || strings.Contains(*r.RemoteUrl, "alice") {
		t.Error("credentials kept")
	}
	if r.PathHash != PathHash(dev, sub) || r.PathHash == PathHash("other-dev", sub) || len(r.PathHash) != 64 {
		t.Error("path hash not salted per developer")
	}
}

func TestRepoForWorktreeFile(t *testing.T) {
	main := gitRepo(t, "[remote \"origin\"]\n\turl = git@github.com:acme/api.git\n")
	wtGit := filepath.Join(main, ".git", "worktrees", "feat")
	must(t, os.MkdirAll(wtGit, 0o700))
	must(t, os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o600))
	wt := filepath.Join(t.TempDir(), "feat")
	must(t, os.MkdirAll(wt, 0o700))
	must(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o600))
	r := RepoFor(wt, dev)
	if r.RemoteUrl == nil || *r.RemoteUrl != "git@github.com:acme/api.git" || *r.DisplayName != "acme/api" {
		t.Errorf("%+v", r)
	}
}

func TestRepoForNoGit(t *testing.T) {
	d := filepath.Join(t.TempDir(), "scratch-app")
	must(t, os.MkdirAll(d, 0o700))
	r := RepoFor(d, dev)
	if r.RemoteUrl != nil || *r.DisplayName != "scratch-app" {
		t.Errorf("%+v", r)
	}
	// A cwd that no longer exists still gets a hash and a name.
	r = RepoFor("/nonexistent/path/to/proj", dev)
	if *r.DisplayName != "proj" || r.PathHash == "" {
		t.Errorf("%+v", r)
	}
}

func TestStripCredentials(t *testing.T) {
	tok := "ghp_" + strings.Repeat("Ab", 18)
	for in, want := range map[string]string{
		"https://x-access-token:" + tok + "@github.com/acme/web.git": "https://github.com/acme/web.git",
		"https://" + tok + "@github.com/acme/web":                    "https://github.com/acme/web",
		"https://github.com/acme/web.git?token=abc#frag":             "https://github.com/acme/web.git",
		"ssh://git@github.com/acme/web.git":                          "ssh://git@github.com/acme/web.git",
		"ssh://deploy:secret1@host.example/acme/web.git":             "ssh://host.example/acme/web.git",
		"git@github.com:acme/web.git":                                "git@github.com:acme/web.git",
		"deploy@host.example:acme/web.git":                           "host.example:acme/web.git",
		"/srv/git/web.git":                                           "/srv/git/web.git",
	} {
		if got := StripCredentials(in); got != want {
			t.Errorf("StripCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOwnerRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/web.git":     "acme/web",
		"https://gitlab.com/group/sub/proj":   "sub/proj",
		"git@github.com:acme/web.git":         "acme/web",
		"ssh://git@github.com/acme/web.git/":  "acme/web",
		"https://example.com/":                "",
		"/srv/git/web.git":                    "",
		"https://dev.azure.com/org/proj/_git": "proj/_git",
	} {
		if got := ownerRepo(in); got != want {
			t.Errorf("ownerRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
