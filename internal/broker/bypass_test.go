package broker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bypassAllowed lists the only places outside internal/broker that may build
// their own HTTP clients or dial sockets. Neither talks to a provider:
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

// forbidden selectors per import path. A selector matches if it equals the
// name or starts with it (so NewRequest also catches NewRequestWithContext and
// Dial catches Dialer and DialTimeout).
var forbidden = map[string][]string{
	"net/http":          {"Client", "Get", "Post", "Head", "NewRequest", "DefaultClient", "DefaultTransport", "Transport"},
	"net":               {"Dial"},
	"crypto/tls":        {"Dial"},
	"net/http/httputil": {"NewSingleHostReverseProxy", "ReverseProxy"},
}

// TestNoBrokerBypass parses every non-test Go file in the module and fails if
// code outside the allowed packages could issue a network request without the
// broker. It works on the syntax tree, so import aliases and dot imports are
// caught too. Test files are skipped: they never ship and use fake transports.
func TestNoBrokerBypass(t *testing.T) {
	root := moduleRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" || name == "vendor" || name == "tools") {
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
		violations = append(violations, scanFile(t, path, rel)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

func scanFile(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	aliases := map[string]string{} // local name -> import path
	var out []string
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if _, ok := forbidden[p]; !ok {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			out = append(out, rel+": dot import of "+p+" hides network calls from this check")
			continue
		}
		aliases[name] = p
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		p, ok := aliases[id.Name]
		if !ok {
			return true
		}
		for _, bad := range forbidden[p] {
			if strings.HasPrefix(sel.Sel.Name, bad) {
				out = append(out, fset.Position(sel.Pos()).String()+": "+p+"."+sel.Sel.Name+" bypasses the broker (use broker.Do / broker.NewRequest)")
			}
		}
		return true
	})
	return out
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

// The checker itself must catch the patterns it claims to.
func TestBypassCheckerCatches(t *testing.T) {
	src := `package x
import (
	h "net/http"
	"net"
)
func f() {
	_ = &h.Client{}
	_, _ = h.NewRequestWithContext(nil, "GET", "", nil)
	_, _ = net.Dial("tcp", "x")
	var d net.Dialer
	_ = d
}`
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := scanFile(t, p, "x.go")
	if len(got) != 4 {
		t.Fatalf("want 4 violations, got %d: %v", len(got), got)
	}
}
