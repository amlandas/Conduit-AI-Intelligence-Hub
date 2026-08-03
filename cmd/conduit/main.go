// Command conduit is Conduit's single binary.
//
// It is deliberately almost empty. Every command lives in internal/cli, and
// every capability behind those commands is a library call (internal/kbservice,
// internal/kb, internal/mcpserver, internal/embed). There is no daemon to
// start and no second binary to install: this process opens the knowledge
// base, does the work, and exits.
package main

import (
	"os"

	"github.com/simpleflo/conduit/internal/cli"
)

// Build information, injected at link time:
//
//	-ldflags "-X main.Version=... -X main.BuildTime=..."
var (
	// Version is set at build time.
	Version = "dev"
	// BuildTime is set at build time.
	BuildTime = "unknown"
)

func main() {
	os.Exit(cli.Execute(Version, BuildTime))
}
