// Package web embeds the static browser client so the server ships as a single
// self-contained binary with no dependency on the working directory.
package web

import "embed"

// Static holds the embedded web client. The files live under a "static"
// subdirectory; use fs.Sub(web.Static, "static") to get a filesystem rooted at
// the client.
//
//go:embed static
var Static embed.FS
