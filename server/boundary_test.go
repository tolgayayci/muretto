package server_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImportBoundary enforces docs/DECISIONS.md: server/ may import only
// internal/contract and schema from the rest of this repository, so the hosted
// parts can take a different license later.
func TestImportBoundary(t *testing.T) {
	const mod = "github.com/tolgayayci/muretto/"
	allowed := []string{mod + "internal/contract", mod + "schema", mod + "server/"}
	n := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		n++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(p, mod) && p != strings.TrimSuffix(mod, "/") {
				continue
			}
			ok := false
			for _, a := range allowed {
				if p == a || (strings.HasSuffix(a, "/") && strings.HasPrefix(p, a)) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s imports %s: server/ may only import internal/contract and schema", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("only %d Go files scanned", n)
	}
}
