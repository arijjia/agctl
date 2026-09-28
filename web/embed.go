// Package web — встроенный UI agctl (React SPA, сборка в dist/, go:embed).
package web

import "embed"

// Dist — собранная статика SPA (префикс "dist").
//
//go:embed all:dist
var Dist embed.FS
