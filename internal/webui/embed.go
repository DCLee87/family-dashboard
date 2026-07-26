package webui

import "embed"

// Files contains the production web application built by Vite.
//
//go:embed dist
var Files embed.FS
