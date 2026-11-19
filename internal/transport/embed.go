package transport

import "embed"

// Files holds the HTML templates and static assets, embedded into the binary.
//
//go:embed templates/*.html static
var Files embed.FS
