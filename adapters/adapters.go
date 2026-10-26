// Package adapterfiles embeds the declarative provider adapters.
package adapterfiles

import "embed"

// Files holds every adapter YAML file in this directory.
//
//go:embed *.yaml
var Files embed.FS
