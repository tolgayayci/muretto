// Package validate checks request bodies against Muretto's JSON Schemas
// (schema/*.schema.json, draft 2020-12, formats asserted).
package validate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/tolgayayci/muretto/schema"
)

const baseID = "https://github.com/tolgayayci/muretto/schema/"

// Validator holds compiled schemas.
type Validator struct {
	syncBatch *jsonschema.Schema
	answer    *jsonschema.Schema
}

// New compiles the schemas embedded in the schema package.
func New() (*Validator, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	names, err := fs.Glob(schema.Files, "*.schema.json")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		raw, err := schema.Files.ReadFile(n)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if err := c.AddResource(baseID+n, doc); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
	}
	v := &Validator{}
	if v.syncBatch, err = c.Compile(baseID + "sync-batch.schema.json"); err != nil {
		return nil, err
	}
	if v.answer, err = c.Compile(baseID + "answer.schema.json"); err != nil {
		return nil, err
	}
	return v, nil
}

// Error is a validation failure with a short list of problems.
type Error struct{ Problems []string }

func (e *Error) Error() string { return "schema validation failed: " + strings.Join(e.Problems, "; ") }

// SyncBatch validates a raw SyncBatch body.
func (v *Validator) SyncBatch(raw []byte) error { return check(v.syncBatch, raw) }

// Answer validates a raw Answer document.
func (v *Validator) Answer(raw []byte) error { return check(v.answer, raw) }

// maxProblems caps how many problems are reported back.
const maxProblems = 10

func check(s *jsonschema.Schema, raw []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &Error{Problems: []string{"body is not valid JSON"}}
	}
	err = s.Validate(doc)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return &Error{Problems: []string{err.Error()}}
	}
	var out []string
	collect(*ve.DetailedOutput(), &out)
	if len(out) == 0 {
		out = []string{"invalid document"}
	}
	return &Error{Problems: out}
}

func collect(u jsonschema.OutputUnit, out *[]string) {
	if len(*out) >= maxProblems {
		return
	}
	if u.Error != nil && len(u.Errors) == 0 {
		loc := u.InstanceLocation
		if loc == "" {
			loc = "/"
		}
		msg := u.Error.String()
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		*out = append(*out, loc+": "+msg)
	}
	for _, c := range u.Errors {
		collect(c, out)
	}
}
