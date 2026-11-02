// Package adapterfiles embeds the declarative provider adapters: one folder
// per provider, holding adapter.yaml, its research and its test fixtures.
package adapterfiles

import "embed"

// Files holds every <provider>/adapter.yaml in this directory. Folders whose
// name starts with "_" (the template) are skipped by the loader.
//
//go:embed */adapter.yaml
var Files embed.FS
