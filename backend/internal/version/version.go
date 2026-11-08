// Package version exposes build metadata for logs and diagnostics.
package version

// Version is overridden at build time with:
//
//	-ldflags "-X budgettracker/internal/version.Version=<version>"
var Version = "dev"
