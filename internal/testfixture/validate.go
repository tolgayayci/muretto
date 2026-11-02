package testfixture

import (
	"bytes"
	"fmt"
	"io/fs"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/tolgayayci/muretto/schema"
)

const schemaBase = "https://github.com/tolgayayci/muretto/schema/"

var (
	compileOnce sync.Once
	answerSch   *jsonschema.Schema
	errCompile  error
)

// ValidateAnswer checks raw JSON against schema/answer.schema.json (with
// formats asserted), so tests prove --json output matches the contract.
func ValidateAnswer(raw []byte) error {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.DefaultDraft(jsonschema.Draft2020)
		c.AssertFormat()
		names, err := fs.Glob(schema.Files, "*.schema.json")
		if err != nil {
			errCompile = err
			return
		}
		for _, n := range names {
			b, err := schema.Files.ReadFile(n)
			if err != nil {
				errCompile = err
				return
			}
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
			if err != nil {
				errCompile = fmt.Errorf("%s: %w", n, err)
				return
			}
			if err := c.AddResource(schemaBase+n, doc); err != nil {
				errCompile = err
				return
			}
		}
		answerSch, errCompile = c.Compile(schemaBase + "answer.schema.json")
	})
	if errCompile != nil {
		return errCompile
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return answerSch.Validate(v)
}
