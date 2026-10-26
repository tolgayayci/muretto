// Package schema embeds Muretto's JSON Schema files, the single source of
// truth for every entity the CLI stores, prints and syncs. Go types are
// generated from these files into internal/contract; TypeScript types into
// web/src/contract.
package schema

import "embed"

// Files holds every *.schema.json file in this directory.
//
//go:embed *.schema.json
var Files embed.FS
