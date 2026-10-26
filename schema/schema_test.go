package schema

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

// The code generators (go-jsonschema, json-schema-to-typescript) only handle a
// subset of JSON Schema. Keywords outside it are silently ignored, which would
// make the generated types disagree with the schema. Fail loudly instead.
var unsupported = []string{"anyOf", "oneOf", "allOf", "not", "if", "then", "else", "const", "patternProperties", "dependentSchemas"}

func TestSchemasUseSupportedSubset(t *testing.T) {
	files, err := fs.Glob(Files, "*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no schema files embedded")
	}
	for _, name := range files {
		raw, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: invalid JSON: %v", name, err)
		}
		walk(t, name, "", doc)
	}
}

func walk(t *testing.T, file, path string, node any) {
	t.Helper()
	switch n := node.(type) {
	case map[string]any:
		// Property names inside "properties" are data, not keywords.
		inProperties := strings.HasSuffix(path, "/properties") || strings.HasSuffix(path, "/$defs")
		for k, v := range n {
			if !inProperties {
				for _, bad := range unsupported {
					if k == bad {
						t.Errorf("%s: %s/%s uses unsupported keyword %q", file, path, k, bad)
					}
				}
				if k == "type" {
					if types, ok := v.([]any); ok {
						for _, ty := range types {
							if ty == "null" && !strings.Contains(path, "additionalProperties") {
								t.Errorf("%s: %s uses a nullable type; make the field optional instead", file, path)
							}
						}
					}
				}
			}
			walk(t, file, path+"/"+k, v)
		}
	case []any:
		for _, v := range n {
			walk(t, file, path, v)
		}
	}
}

func TestSchemaIDsMatchFileNames(t *testing.T) {
	files, _ := fs.Glob(Files, "*.schema.json")
	for _, name := range files {
		raw, _ := Files.ReadFile(name)
		var doc struct {
			ID     string `json:"$id"`
			Schema string `json:"$schema"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(doc.ID, "/"+name) {
			t.Errorf("%s: $id %q does not end in the file name", name, doc.ID)
		}
		if doc.Schema != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s: $schema must be draft 2020-12, got %q", name, doc.Schema)
		}
	}
}
