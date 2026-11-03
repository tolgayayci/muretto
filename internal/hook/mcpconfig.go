package hook

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tolgayayci/muretto/internal/paths"
	"github.com/tolgayayci/muretto/internal/redact"
)

// A project-pinned MCP server (e.g. Supabase's
// https://mcp.supabase.com/mcp?project_ref=<ref>) takes no project id in its
// tool input, so the hook records which project the server was pinned to.
// Claude Code doesn't send the server URL, only its name and scope, so the
// hook reads the server's entry from Claude Code's MCP config and keeps only
// allowlisted identifier query params. Never the URL, its path, headers,
// other params or env values.

// mcpParamKeys are the query parameter names that may be recorded.
var mcpParamKeys = map[string]bool{
	"project_ref": true, "project_id": true, "projectId": true,
	"team_id": true, "teamId": true, "org_id": true, "organization_id": true, "account_id": true,
}

var mcpParamValue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// maxMCPConfig caps the size of a config file the hook will parse.
const maxMCPConfig = 16 << 20

// maxMCPParams caps how many params are kept.
const maxMCPParams = 8

// MCPParamAllowed reports whether an MCP server URL param may be recorded:
// an allowlisted key and a short id-shaped value the redactor leaves alone.
func MCPParamAllowed(key, value string) bool {
	return mcpParamKeys[key] && mcpParamValue.MatchString(value) && redact.String(value) == value
}

// mcpResolvable reports whether the hook can find a server of this scope in
// Claude Code's config files.
func mcpResolvable(scope string) bool {
	return scope == "project" || scope == "local" || scope == "user"
}

// mcpServerParams returns the allowlisted URL params of MCP server name,
// loaded by Claude Code from scope, for a session running in cwd. Any error
// gives nil.
func mcpServerParams(name, scope, cwd string, getenv func(string) string) map[string]string {
	if name == "" || cwd == "" || !filepath.IsAbs(cwd) || !mcpResolvable(scope) {
		return nil
	}
	var entry json.RawMessage
	if scope == "project" {
		entry = projectServer(name, filepath.Clean(cwd))
	} else {
		path, err := paths.ClaudeGlobalConfig()
		if err != nil {
			return nil
		}
		entry = globalServer(path, name, scope, filepath.Clean(cwd))
	}
	if entry == nil {
		return nil
	}
	var srv struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(entry, &srv) != nil || srv.URL == "" {
		return nil
	}
	return urlParams(srv.URL, getenv)
}

type mcpServers struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// projectServer finds name in the nearest .mcp.json, walking up from cwd,
// that defines it.
func projectServer(name, cwd string) json.RawMessage {
	dir := cwd
	for range 64 {
		if b := readConfig(filepath.Join(dir, ".mcp.json")); b != nil {
			var c mcpServers
			if json.Unmarshal(b, &c) == nil {
				if e, ok := c.MCPServers[name]; ok {
					return e
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

// globalServer finds name in Claude Code's global config: top-level
// mcpServers (user scope), or projects[<cwd or a parent>].mcpServers (local).
func globalServer(path, name, scope, cwd string) json.RawMessage {
	b := readConfig(path)
	if b == nil {
		return nil
	}
	var c struct {
		mcpServers
		Projects map[string]mcpServers `json:"projects"`
	}
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	if scope == "user" {
		return c.MCPServers[name]
	}
	dir := cwd
	for range 64 {
		if p, ok := c.Projects[dir]; ok {
			if e, ok := p.MCPServers[name]; ok {
				return e
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

// readConfig returns the contents of a regular file of at most maxMCPConfig
// bytes, or nil.
func readConfig(path string) []byte {
	f, err := os.Open(path) // #nosec G304 -- Claude Code's own config files
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxMCPConfig {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, maxMCPConfig+1))
	if err != nil || len(b) > maxMCPConfig {
		return nil
	}
	return b
}

// urlParams returns the allowlisted query params of raw. ${VAR} and
// ${VAR:-default} are expanded (as Claude Code does in .mcp.json) only in the
// values of allowlisted keys. A key given twice with different values is
// dropped.
func urlParams(raw string, getenv func(string) string) map[string]string {
	_, q, ok := strings.Cut(raw, "?")
	if !ok {
		return nil
	}
	q, _, _ = strings.Cut(q, "#")
	out := map[string]string{}
	conflict := map[string]bool{}
	for _, part := range strings.Split(q, "&") {
		k, v, _ := strings.Cut(part, "=")
		k, err := url.QueryUnescape(k)
		if err != nil || !mcpParamKeys[k] {
			continue
		}
		v, ok := expandEnv(v, getenv)
		if !ok {
			conflict[k] = true
			continue
		}
		v, err = url.QueryUnescape(v)
		if err != nil || !MCPParamAllowed(k, v) {
			conflict[k] = true
			continue
		}
		if old, seen := out[k]; seen && old != v {
			conflict[k] = true
		}
		out[k] = v
	}
	for k := range conflict {
		delete(out, k)
	}
	if len(out) == 0 || len(out) > maxMCPParams {
		return nil
	}
	return out
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// expandEnv expands ${VAR} and ${VAR:-default}. An unset variable without a
// default, or a malformed reference, fails.
func expandEnv(s string, getenv func(string) string) (string, bool) {
	var b strings.Builder
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			b.WriteString(s)
			return b.String(), true
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return "", false
		}
		name, def, hasDef := strings.Cut(s[i+2:i+j], ":-")
		if !envName.MatchString(name) {
			return "", false
		}
		v := ""
		if getenv != nil {
			v = getenv(name)
		}
		if v == "" {
			if !hasDef {
				return "", false
			}
			v = def
		}
		b.WriteString(s[:i])
		b.WriteString(v)
		s = s[i+j+1:]
	}
}
