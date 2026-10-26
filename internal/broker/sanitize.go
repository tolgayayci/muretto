package broker

import (
	"bytes"
	"encoding/json"
	"strings"
)

// alwaysStrip are JSON keys removed from every provider response at any depth,
// whatever the adapter says. Each one is a secret (or a secret-bearing object)
// some provider returns next to the metadata Muretto needs:
// Vercel env "value"/"vsmValue"/"legacyValue", "protectionBypass" (keys are
// bypass secrets) and "deployHooks" (secret trigger URLs); Supabase
// "jwt_secret" and "db_pass". No adapter may rely on a field with these names.
var alwaysStrip = map[string]bool{
	"value":            true,
	"vsmValue":         true,
	"legacyValue":      true,
	"protectionBypass": true,
	"deployHooks":      true,
	"jwt_secret":       true,
	"db_pass":          true,
	"service_role_key": true,
	"smtp_pass":        true,
	"claimUrl":         true,
}

// AlwaysStripped reports whether key is removed from every response.
func AlwaysStripped(key string) bool { return alwaysStrip[key] }

// sanitizeBody removes secret fields from a JSON body. Bodies that are not JSON
// are returned unchanged (adapters only parse JSON, so they fail closed).
func sanitizeBody(body []byte, contentType string, paths []string) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		if !strings.Contains(contentType, "json") {
			return body
		}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return body
	}
	for _, p := range paths {
		stripPath(v, strings.Split(p, "."))
	}
	v = stripKeys(v)
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}

func stripKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if alwaysStrip[k] {
				delete(t, k)
				continue
			}
			t[k] = stripKeys(child)
		}
	case []any:
		for i, child := range t {
			t[i] = stripKeys(child)
		}
	}
	return v
}

// stripPath deletes one dotted path. "name[]" walks every element of the array
// at name; a leading "[]" walks a root array.
func stripPath(v any, parts []string) {
	if len(parts) == 0 {
		return
	}
	part := parts[0]
	if part == "[]" {
		if arr, ok := v.([]any); ok {
			for _, el := range arr {
				stripPath(el, parts[1:])
			}
		}
		return
	}
	if name, isArr := strings.CutSuffix(part, "[]"); isArr {
		if name == "" {
			stripPath(v, append([]string{"[]"}, parts[1:]...))
			return
		}
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		stripPath(m[name], append([]string{"[]"}, parts[1:]...))
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	if len(parts) == 1 {
		delete(m, part)
		return
	}
	stripPath(m[part], parts[1:])
}
