// Package web embeds the HTML templates and static assets used by the
// server-rendered application.
package web

import "embed"

// Templates contains the HTML page templates.
//
//go:embed templates/*.html
var Templates embed.FS

// Static contains the browser assets served under /static.
//
//go:embed static
var Static embed.FS
