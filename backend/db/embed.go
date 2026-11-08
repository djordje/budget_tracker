// Package db embeds database migrations so every build uses the same set.
package db

import "embed"

// Migrations contains the numbered SQL migration files.
//
//go:embed migrations/*.sql
var Migrations embed.FS
