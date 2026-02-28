package web

import "embed"

// Static holds the embedded web/static directory.
// Handlers access it via fs.Sub(Static, "static").
//
//go:embed static
var Static embed.FS
