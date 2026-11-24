package web

import "embed"

// DistFS embeds the built Ambra Web client static assets.
//
//go:embed all:dist
var DistFS embed.FS
