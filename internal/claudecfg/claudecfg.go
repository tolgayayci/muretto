// Package claudecfg adds Muretto's hooks to Claude Code settings additively,
// and removes exactly what it added.
//
// The user's settings.json is edited through an order-preserving JSON tree:
// unknown keys, their order, number literals and string escapes are kept.
// Muretto only appends one matcher group per event, backs the file up to
// ~/.muretto/backups/ first, validates the result as strict JSON, replaces the
// file atomically and records every handler it added in install-manifest.json.
package claudecfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/tolgayayci/muretto/internal/paths"
)

// Status describes Muretto's hooks in the Claude Code settings.
type Status struct {
	SettingsPath  string
	Installed     bool
	Events        []string
	HooksDisabled bool // disableAllHooks is set
	ManagedOnly   bool // allowManagedHooksOnly is set
}

// Hook events Muretto registers, in order, and the matcher each group gets
// ("" = no matcher key: the event has none or it means all).
var Events = []struct{ Name, Matcher string }{
	{"SessionStart", ""},
	{"UserPromptSubmit", ""},
	{"PostToolUse", "*"},
	{"PostToolUseFailure", "*"},
	{"SubagentStart", ""},
	{"SessionEnd", ""},
}

// Timeout is each handler's timeout in seconds (Claude Code's default is 600).
const Timeout = 5

// managedDirs are where file-based managed settings live (docs: "Deploy
// managed settings"). A var so tests can point it elsewhere.
var managedDirs = func() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Library/Application Support/ClaudeCode"}
	case "linux":
		return []string{"/etc/claude-code"}
	case "windows":
		return []string{`C:\Program Files\ClaudeCode`}
	}
	return nil
}()

// Handler is one hook handler Muretto added, as recorded in the manifest.
type Handler struct {
	Event   string   `json:"event"`
	Matcher string   `json:"matcher,omitempty"`
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"`
}

// Manifest is install-manifest.json: exactly what Install added.
type Manifest struct {
	Version      int       `json:"version"`
	SettingsPath string    `json:"settings_path"`
	InstalledAt  time.Time `json:"installed_at"`
	Backup       string    `json:"backup,omitempty"`
	Handlers     []Handler `json:"handlers"`
}

func handlerFor(event, matcher, bin string) Handler {
	return Handler{Event: event, Matcher: matcher, Type: "command", Command: bin, Args: []string{"hook", event}, Timeout: Timeout}
}

func (h Handler) node() *node {
	return fromValue(orderedObj{{"type", h.Type}, {"command", h.Command}, {"args", h.Args}, {"timeout", h.Timeout}})
}

// matches reports whether a settings handler node is this handler. Timeout is
// ignored so a user who tuned it still gets a clean uninstall.
func (h Handler) matches(n *node) bool {
	if n == nil || n.kind != 'o' || n.get("type").strVal() != h.Type || n.get("command").strVal() != h.Command {
		return false
	}
	args := n.get("args")
	if args == nil || args.kind != 'a' || len(args.vals) != len(h.Args) {
		return false
	}
	for i, a := range args.vals {
		if a.strVal() != h.Args[i] {
			return false
		}
	}
	return true
}

// SettingsPath is <CLAUDE_CONFIG_DIR or ~/.claude>/settings.json.
func SettingsPath() (string, error) {
	d, err := paths.ClaudeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "settings.json"), nil
}

// settingsFile is a loaded settings.json.
type settingsFile struct {
	path    string
	exists  bool
	orig    []byte
	mode    os.FileMode
	root    *node
	indent  string
	trailNL bool
}

func load(path string) (*settingsFile, error) {
	f := &settingsFile{path: path, mode: 0o600, indent: "  ", trailNL: true}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		f.root = &node{kind: 'o'}
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f.exists, f.orig = true, b
	if fi, err := os.Stat(path); err == nil {
		f.mode = fi.Mode().Perm()
	}
	if len(bytes.TrimSpace(b)) == 0 {
		f.root = &node{kind: 'o'}
		return f, nil
	}
	root, err := parseJSON(b)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON; fix it first, Muretto won't touch it: %w", path, err)
	}
	if root.kind != 'o' {
		return nil, fmt.Errorf("%s is not a JSON object; Muretto won't touch it", path)
	}
	f.root, f.indent, f.trailNL = root, detectIndent(b), bytes.HasSuffix(b, []byte("\n"))
	return f, nil
}

// backup copies the original file to <backups>/settings.json.<ts>.bak.
func (f *settingsFile) backup(p paths.Paths, now time.Time) (string, error) {
	if !f.exists {
		return "", nil
	}
	if err := os.MkdirAll(p.Backups, paths.DirPerm); err != nil {
		return "", fmt.Errorf("create backups dir: %w", err)
	}
	name := fmt.Sprintf("settings.json.%s.bak", now.UTC().Format("20060102T150405.000000000Z"))
	dst := filepath.Join(p.Backups, name)
	if err := writeAtomic(dst, f.orig, paths.FilePerm); err != nil {
		return "", fmt.Errorf("back up settings: %w", err)
	}
	return dst, nil
}

// save validates the new content as strict JSON and replaces the file atomically.
func (f *settingsFile) save(check func(*node) error) error {
	out := f.root.encode(f.indent)
	if f.trailNL {
		out = append(out, '\n')
	}
	if !json.Valid(out) {
		return errors.New("internal error: generated settings are not valid JSON; file left unchanged")
	}
	reparsed, err := parseJSON(out)
	if err != nil {
		return fmt.Errorf("internal error: generated settings don't parse: %w", err)
	}
	if err := check(reparsed); err != nil {
		return fmt.Errorf("internal error: %w; file left unchanged", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.path), paths.DirPerm); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(f.path), err)
	}
	return writeAtomic(f.path, out, f.mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".muretto-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// hooksObj returns settings.hooks, creating it (at the end) when create is set.
func hooksObj(root *node, create bool) (*node, error) {
	h := root.get("hooks")
	if h == nil {
		if !create {
			return nil, nil
		}
		h = &node{kind: 'o'}
		root.set("hooks", h)
	}
	if h.kind != 'o' {
		return nil, errors.New(`settings "hooks" is not an object; Muretto won't touch it`)
	}
	return h, nil
}

// present reports whether handler h is in settings.hooks[h.Event].
func present(hooks *node, h Handler) bool {
	arr := hooks.get(h.Event)
	if arr == nil || arr.kind != 'a' {
		return false
	}
	for _, g := range arr.vals {
		if hs := g.get("hooks"); hs != nil && hs.kind == 'a' {
			for _, hn := range hs.vals {
				if h.matches(hn) {
					return true
				}
			}
		}
	}
	return false
}

func readManifest(p paths.Paths) (*Manifest, error) {
	b, err := os.ReadFile(p.Manifest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest %s is corrupt: %w", p.Manifest, err)
	}
	return &m, nil
}

func writeManifest(p paths.Paths, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Manifest), paths.DirPerm); err != nil {
		return err
	}
	return writeAtomic(p.Manifest, append(b, '\n'), paths.FilePerm)
}

// Install backs up the settings file, adds Muretto's hooks and records them in
// the manifest. It is idempotent: handlers already present are not added
// again. Handlers from an earlier install with a different binary path (as
// listed in the manifest) are replaced.
func Install(p paths.Paths, binaryPath string) (Status, error) {
	if binaryPath == "" || !filepath.IsAbs(binaryPath) {
		return Status{}, fmt.Errorf("binary path must be absolute, got %q", binaryPath)
	}
	sp, err := SettingsPath()
	if err != nil {
		return Status{}, err
	}
	f, err := load(sp)
	if err != nil {
		return Status{}, err
	}
	man, err := readManifest(p)
	if err != nil {
		return Status{}, err
	}
	if man != nil && man.SettingsPath != "" && man.SettingsPath != sp {
		return Status{}, fmt.Errorf("muretto is already installed in %s; run uninstall first", man.SettingsPath)
	}
	if man == nil {
		man = &Manifest{Version: 1}
	}
	hooks, err := hooksObj(f.root, true)
	if err != nil {
		return Status{}, err
	}
	for _, e := range Events {
		if arr := hooks.get(e.Name); arr != nil && arr.kind != 'a' {
			return Status{}, fmt.Errorf(`settings "hooks.%s" is not an array; Muretto won't touch it`, e.Name)
		}
	}

	changed := false
	// Drop handlers of an earlier install that used another binary path.
	var keep []Handler
	for _, h := range man.Handlers {
		if h.Command == binaryPath {
			keep = append(keep, h)
			continue
		}
		if removeHandler(hooks, h) {
			changed = true
		}
	}
	man.Handlers = keep

	var added []Handler
	for _, e := range Events {
		h := handlerFor(e.Name, e.Matcher, binaryPath)
		if present(hooks, h) {
			continue
		}
		group := orderedObj{}
		if e.Matcher != "" {
			group = append(group, kv{"matcher", e.Matcher})
		}
		g := fromValue(group)
		g.set("hooks", &node{kind: 'a', vals: []*node{h.node()}})
		arr := hooks.get(e.Name)
		if arr == nil {
			arr = &node{kind: 'a'}
			hooks.set(e.Name, arr)
		}
		arr.vals = append(arr.vals, g) // append only: the user's groups stay first
		added = append(added, h)
		changed = true
	}
	for _, h := range added {
		if !slices.ContainsFunc(man.Handlers, func(x Handler) bool { return x.Event == h.Event && x.Command == h.Command }) {
			man.Handlers = append(man.Handlers, h)
		}
	}
	now := time.Now()
	if changed {
		bk, err := f.backup(p, now)
		if err != nil {
			return Status{}, err
		}
		if err := f.save(func(n *node) error {
			h, err := hooksObj(n, false)
			if err != nil || h == nil {
				return errors.New("hooks missing after install")
			}
			for _, e := range Events {
				if !present(h, handlerFor(e.Name, e.Matcher, binaryPath)) {
					return fmt.Errorf("handler for %s missing after install", e.Name)
				}
			}
			return nil
		}); err != nil {
			return Status{}, err
		}
		if bk != "" {
			man.Backup = bk
		}
		man.InstalledAt = now.UTC()
	}
	man.SettingsPath = sp
	if man.InstalledAt.IsZero() {
		man.InstalledAt = now.UTC()
	}
	if err := writeManifest(p, man); err != nil {
		return Status{}, fmt.Errorf("write manifest (settings were updated; run install again): %w", err)
	}
	return Inspect(p, binaryPath)
}

// removeHandler deletes every copy of h from settings.hooks[h.Event], then any
// matcher group that removal left empty, then the event key if it became
// empty. Groups and keys that were already empty are left alone.
func removeHandler(hooks *node, h Handler) bool {
	arr := hooks.get(h.Event)
	if arr == nil || arr.kind != 'a' {
		return false
	}
	removed := false
	var groups []*node
	for _, g := range arr.vals {
		hs := g.get("hooks")
		if hs == nil || hs.kind != 'a' {
			groups = append(groups, g)
			continue
		}
		before := len(hs.vals)
		hs.vals = slices.DeleteFunc(hs.vals, h.matches)
		if len(hs.vals) != before {
			removed = true
			if len(hs.vals) == 0 {
				continue // a group we emptied
			}
		}
		groups = append(groups, g)
	}
	arr.vals = groups
	if removed && len(arr.vals) == 0 {
		hooks.del(h.Event)
	}
	return removed
}

// Uninstall removes exactly the hooks listed in the manifest.
func Uninstall(p paths.Paths) (Status, error) {
	man, err := readManifest(p)
	if err != nil {
		return Status{}, err
	}
	sp, err := SettingsPath()
	if err != nil {
		return Status{}, err
	}
	if man == nil {
		st, err := inspect(sp, "")
		return st, err
	}
	if man.SettingsPath != "" {
		sp = man.SettingsPath
	}
	f, err := load(sp)
	if err != nil {
		return Status{}, err
	}
	bin := ""
	if hooks, err := hooksObj(f.root, false); err != nil {
		return Status{}, err
	} else if hooks != nil {
		changed := false
		for _, h := range man.Handlers {
			bin = h.Command
			if removeHandler(hooks, h) {
				changed = true
			}
		}
		if changed && len(hooks.keys) == 0 {
			f.root.del("hooks")
		}
		if changed {
			if _, err := f.backup(p, time.Now()); err != nil {
				return Status{}, err
			}
			if err := f.save(func(n *node) error {
				h, _ := hooksObj(n, false)
				for _, x := range man.Handlers {
					if h != nil && present(h, x) {
						return fmt.Errorf("handler for %s still present", x.Event)
					}
				}
				return nil
			}); err != nil {
				return Status{}, err
			}
		}
	}
	if err := os.Remove(p.Manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Status{}, fmt.Errorf("remove manifest: %w", err)
	}
	return inspect(sp, bin)
}

// Inspect reports the current state without changing anything.
func Inspect(p paths.Paths, binaryPath string) (Status, error) {
	sp, err := SettingsPath()
	if err != nil {
		return Status{}, err
	}
	if man, _ := readManifest(p); man != nil && man.SettingsPath != "" {
		sp = man.SettingsPath
	}
	return inspect(sp, binaryPath)
}

func inspect(settingsPath, binaryPath string) (Status, error) {
	st := Status{SettingsPath: settingsPath}
	f, err := load(settingsPath)
	if err != nil {
		return st, err
	}
	st.HooksDisabled = f.root.get("disableAllHooks").isTrue()
	st.ManagedOnly = f.root.get("allowManagedHooksOnly").isTrue()
	for _, m := range managedSettings() {
		st.HooksDisabled = st.HooksDisabled || m.get("disableAllHooks").isTrue()
		st.ManagedOnly = st.ManagedOnly || m.get("allowManagedHooksOnly").isTrue()
	}
	if binaryPath != "" {
		if hooks, err := hooksObj(f.root, false); err == nil && hooks != nil {
			for _, e := range Events {
				if present(hooks, handlerFor(e.Name, e.Matcher, binaryPath)) {
					st.Events = append(st.Events, e.Name)
				}
			}
		}
	}
	st.Installed = len(st.Events) == len(Events)
	return st, nil
}

// managedSettings reads file-based managed settings: managed-settings.json
// and managed-settings.d/*.json. Unreadable or invalid files are skipped (MDM
// and server-managed policies can't be read from disk).
func managedSettings() []*node {
	var out []*node
	for _, d := range managedDirs {
		files := []string{filepath.Join(d, "managed-settings.json")}
		drop, _ := filepath.Glob(filepath.Join(d, "managed-settings.d", "*.json"))
		slices.Sort(drop)
		files = append(files, drop...)
		for _, f := range files {
			if filepath.Base(f)[0] == '.' {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if n, err := parseJSON(b); err == nil && n.kind == 'o' {
				out = append(out, n)
			}
		}
	}
	return out
}
