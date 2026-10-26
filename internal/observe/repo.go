package observe

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/internal/redact"
)

// PathHash is the salted sha256 of a working directory. The salt is the
// developer id, so the same path hashes the same for one developer but can't
// be looked up in a table of common paths across developers. The path itself
// never leaves the machine.
func PathHash(developerID, cwd string) string {
	h := sha256.New()
	h.Write([]byte("muretto-repo-v1\x00"))
	h.Write([]byte(developerID))
	h.Write([]byte{0})
	h.Write([]byte(filepath.Clean(cwd)))
	return hex.EncodeToString(h.Sum(nil))
}

type repoCache struct {
	m map[string]*contract.SessionRepo
}

func newRepoCache() *repoCache { return &repoCache{m: map[string]*contract.SessionRepo{}} }

func (c *repoCache) get(cwd, developerID string) *contract.SessionRepo {
	if r, ok := c.m[cwd]; ok {
		cp := *r
		return &cp
	}
	r := RepoFor(cwd, developerID)
	c.m[cwd] = r
	cp := *r
	return &cp
}

// RepoFor describes the repository a session ran in. The remote URL comes from
// reading .git/config directly (git is never executed), with any credentials
// removed. display_name is owner/repo from the remote, else the directory name.
func RepoFor(cwd, developerID string) *contract.SessionRepo {
	r := &contract.SessionRepo{PathHash: PathHash(developerID, cwd)}
	root, cfg := findGitConfig(cwd)
	if cfg != "" {
		if remote := remoteURL(cfg); remote != "" {
			clean := StripCredentials(remote)
			r.RemoteUrl = optional(truncateRunes(clean, 500))
			if dn := ownerRepo(clean); dn != "" {
				r.DisplayName = &dn
			}
		}
	}
	if r.DisplayName == nil {
		base := root
		if base == "" {
			base = cwd
		}
		if name := filepath.Base(filepath.Clean(base)); name != "." && name != string(filepath.Separator) && name != "" {
			name = truncateRunes(name, 200)
			r.DisplayName = &name
		}
	}
	return r
}

// findGitConfig walks up from dir to the repository root and returns the root
// and the path of its config file. It handles worktrees and submodules, where
// .git is a file holding "gitdir: <path>" (and the config lives in commondir).
func findGitConfig(dir string) (root, config string) {
	d := filepath.Clean(dir)
	for i := 0; i < 64; i++ {
		g := filepath.Join(d, ".git")
		if fi, err := os.Stat(g); err == nil {
			if fi.IsDir() {
				return d, filepath.Join(g, "config")
			}
			return d, configFromGitFile(g)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", ""
}

func configFromGitFile(gitFile string) string {
	b, err := readSmall(gitFile)
	if err != nil {
		return ""
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(gitFile), gitdir)
	}
	if c, err := readSmall(filepath.Join(gitdir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
		return filepath.Join(common, "config")
	}
	return filepath.Join(gitdir, "config")
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// remoteURL returns the url of remote "origin", or of the first remote.
func remoteURL(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	var section, first, origin string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		name, ok := strings.CutPrefix(section, "remote ")
		if !ok {
			continue
		}
		name = strings.Trim(strings.TrimSpace(name), `"`)
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "url") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if first == "" {
			first = v
		}
		if name == "origin" && origin == "" {
			origin = v
		}
	}
	if origin != "" {
		return origin
	}
	return first
}

// StripCredentials removes userinfo from a remote URL (keeping the "git" user
// of ssh remotes, which is not a credential) and drops query and fragment.
func StripCredentials(remote string) string {
	if !strings.Contains(remote, "://") {
		// scp-like: [user@]host:owner/repo. Keep only a "git" user.
		if at := strings.Index(remote, "@"); at >= 0 {
			if colon := strings.Index(remote, ":"); colon < 0 || at < colon {
				if remote[:at] != "git" {
					remote = remote[at+1:]
				}
			}
		}
		return redact.String(remote)
	}
	u, err := url.Parse(remote)
	if err != nil {
		return redact.String(remote)
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass || u.User.Username() != "git" || u.Scheme != "ssh" {
			u.User = nil
		}
	}
	u.RawQuery, u.Fragment = "", ""
	return redact.String(u.String())
}

// ownerRepo extracts owner/repo from a remote URL.
func ownerRepo(remote string) string {
	p := remote
	if i := strings.Index(p, "://"); i >= 0 {
		p = p[i+3:]
		if j := strings.Index(p, "/"); j >= 0 {
			p = p[j+1:]
		} else {
			return ""
		}
	} else if j := strings.Index(p, ":"); j >= 0 {
		p = p[j+1:]
	} else {
		return ""
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	parts := strings.Split(p, "/")
	if len(parts) < 2 || parts[len(parts)-1] == "" || parts[len(parts)-2] == "" {
		return ""
	}
	return truncateRunes(parts[len(parts)-2]+"/"+parts[len(parts)-1], 200)
}
