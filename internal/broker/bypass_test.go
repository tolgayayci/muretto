package broker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// bypassAllowed lists the only places that may open sockets or build HTTP
// clients. Neither of the last two talks to a provider:
//   - internal/syncclient is the CLI side of Muretto's own login and sync API;
//   - server/ is Muretto's own sync server.
//
// Adding a path here needs a decision record explaining why it can never reach
// a provider API.
var bypassAllowed = []string{
	"internal/broker/",
	"internal/syncclient/",
	"server/",
}

// allowedImports is the complete list of packages that non-test code outside
// bypassAllowed may import (besides this module's own packages, minus
// server/). Anything else, including os/exec, net, net/smtp, net/rpc,
// syscall, unsafe, plugin, crypto/tls, net/http/httputil, cgo and
// golang.org/x/net/..., fails the test. Add a package only if it cannot reach
// the network or run another program.
var allowedImports = map[string]string{
	"bufio": "", "bytes": "", "cmp": "", "context": "", "crypto/rand": "",
	"crypto/sha256": "", "embed": "", "encoding/base64": "", "encoding/hex": "",
	"encoding/json": "", "errors": "", "flag": "", "fmt": "", "io": "", "io/fs": "",
	"maps": "", "math": "", "net/url": "parsing only", "os/signal": "",
	"path": "", "path/filepath": "", "regexp": "", "runtime": "", "slices": "",
	"sort": "", "strconv": "", "strings": "", "sync": "", "text/tabwriter": "",
	"time": "", "unicode": "", "unicode/utf8": "",
	"os":                            "files only: os.StartProcess is forbidden below",
	"os/user":                       "local account lookup for paths",
	"database/sql":                  "only the embedded sqlite driver is importable",
	"net/http":                      "types and constants only: see restrictedSelectors",
	"go.yaml.in/yaml/v3":            "adapter files",
	"golang.org/x/term":             "terminal input for init",
	"github.com/zalando/go-keyring": "OS keychain (local IPC, not network)",
	"modernc.org/sqlite":            "embedded database",
	"github.com/santhosh-tekuri/jsonschema/v6": "schema checks; the default loader reads files only",
}

// selectorPolicy limits what may be used from an allowed import. allow, if
// set, is the complete list (an entry ending in "*" is a prefix); deny is
// always forbidden.
type selectorPolicy struct{ allow, deny []string }

var restrictedSelectors = map[string]selectorPolicy{
	// A package may use net/http for types in signatures and for status and
	// method constants, never to build a client, transport, server or request.
	"net/http": {allow: []string{"Request", "Response", "Header", "NoBody", "StatusText", "CanonicalHeaderKey", "Status*", "Method*"}},
	"os":       {deny: []string{"StartProcess"}},
}

// fileExceptions allow one file a specific import and selectors that are
// otherwise forbidden. Each entry says why it cannot reach a provider.
var fileExceptions = map[string]map[string][]string{
	// `muretto setup` checks whether the claude CLI is on PATH to tell the user
	// if Claude Code is installed. It never runs it.
	"cmd/muretto/setup.go": {"os/exec": {"LookPath"}},
}

const modulePath = "github.com/tolgayayci/muretto"

// TestNoBrokerBypass parses every non-test Go file in the module and fails if
// code outside the allowed packages could reach the network or run another
// program without the broker. It works on the syntax tree, so import aliases
// are caught too; dot imports are refused outright. Test files are skipped:
// they never ship and use fake transports.
func TestNoBrokerBypass(t *testing.T) {
	root := moduleRoot(t)
	var violations []string
	scanned := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			// Another module (e.g. tools/, which pins dev tools) is not
			// part of the shipped binary.
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, a := range bypassAllowed {
			if strings.HasPrefix(rel, a) {
				return nil
			}
		}
		scanned[rel] = true
		violations = append(violations, scanFile(t, path, rel)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Guard against a walk that silently skips the code it should check.
	for _, must := range []string{"cmd/muretto/setup.go", "internal/adapters/scan.go"} {
		if !scanned[must] {
			t.Errorf("%s was not scanned", must)
		}
	}
	for _, v := range violations {
		t.Error(v)
	}
}

func scanFile(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return checkFile(fset, f, rel)
}

// checkFile returns every violation in one parsed file, which is treated as if
// it lived at rel.
func checkFile(fset *token.FileSet, f *ast.File, rel string) []string {
	var out []string
	bad := func(pos token.Pos, msg string) {
		out = append(out, fset.Position(pos).String()+": "+msg)
	}
	exceptions := fileExceptions[rel]
	type policy struct {
		path        string
		allow, deny []string
	}
	aliases := map[string]policy{} // local name -> selector policy
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			bad(imp.Pos(), "dot import of "+p+" hides calls from this check")
			continue
		}
		if sels, ok := exceptions[p]; ok {
			aliases[name] = policy{path: p, allow: sels}
			continue
		}
		if p == modulePath+"/server" || strings.HasPrefix(p, modulePath+"/server/") {
			bad(imp.Pos(), "import of "+p+": Muretto's server may only be used by itself")
			continue
		}
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			continue
		}
		if _, ok := allowedImports[p]; !ok {
			bad(imp.Pos(), "import of "+p+" is not on the allowlist (allowedImports in internal/broker/bypass_test.go); provider calls go through broker.Do")
			continue
		}
		if sp, ok := restrictedSelectors[p]; ok {
			aliases[name] = policy{path: p, allow: sp.allow, deny: sp.deny}
		}
	}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:linkname") {
				bad(c.Pos(), "go:linkname can reach unexported network code")
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Name.Name == "RoundTrip" {
				bad(n.Pos(), "RoundTrip method: implementing an http.RoundTripper bypasses the broker")
			}
		case *ast.SelectorExpr:
			if n.Sel.Name == "RoundTrip" {
				bad(n.Pos(), "RoundTrip call bypasses the broker")
				return true
			}
			id, ok := n.X.(*ast.Ident)
			if !ok {
				return true
			}
			pol, ok := aliases[id.Name]
			if !ok {
				return true
			}
			sel := n.Sel.Name
			if (pol.allow != nil && !selectorIn(sel, pol.allow)) || selectorIn(sel, pol.deny) {
				bad(n.Pos(), pol.path+"."+sel+" is not allowed here (provider calls go through broker.Do / broker.NewRequest)")
			}
		}
		return true
	})
	return out
}

func selectorIn(sel string, list []string) bool {
	for _, a := range list {
		if p, ok := strings.CutSuffix(a, "*"); ok {
			if strings.HasPrefix(sel, p) {
				return true
			}
		} else if sel == a {
			return true
		}
	}
	return false
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// The checker itself must catch the patterns it claims to. Each fixture in
// testdata/bypass is parsed (never compiled) as if it lived at the path in
// its "// path:" line (default cmd/fixture/x.go). A fixture named ok_*.go
// must pass; any other must fail with every "// want:" substring.
func TestBypassCheckerCatches(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "bypass", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	sort.Strings(files)
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			rel := "cmd/fixture/x.go"
			var wants []string
			for _, line := range strings.Split(string(src), "\n") {
				if v, ok := strings.CutPrefix(line, "// path: "); ok {
					rel = strings.TrimSpace(v)
				}
				if v, ok := strings.CutPrefix(line, "// want: "); ok {
					wants = append(wants, strings.TrimSpace(v))
				}
			}
			got := scanFile(t, file, rel)
			if strings.HasPrefix(name, "ok_") {
				if len(got) != 0 {
					t.Fatalf("clean fixture flagged: %v", got)
				}
				return
			}
			if len(wants) == 0 {
				t.Fatal("fixture has no // want: line")
			}
			if len(got) == 0 {
				t.Fatal("checker missed this bypass")
			}
			all := strings.Join(got, "\n")
			for _, w := range wants {
				if !strings.Contains(all, w) {
					t.Errorf("want a violation mentioning %q, got:\n%s", w, all)
				}
			}
		})
	}
}

// Every fileExceptions entry must point at a real file, so a stale exception
// cannot silently cover a new file with the same name.
func TestBypassExceptionsExist(t *testing.T) {
	root := moduleRoot(t)
	for rel := range fileExceptions {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("fileExceptions: %s: %v", rel, err)
		}
	}
}
