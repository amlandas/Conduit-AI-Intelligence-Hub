// Package main is the entry point for the Conduit daemon.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/daemon"
	"github.com/simpleflo/conduit/internal/observability"
)

var (
	// Version is set at build time
	Version = "dev"
	// BuildTime is set at build time
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "conduit-daemon",
		Short: "Conduit daemon - AI Intelligence Hub background service",
		Long: `Conduit daemon manages connector lifecycles, client bindings,
and the knowledge base. It runs as a background service and
communicates with the CLI via Unix socket.`,
		Version: fmt.Sprintf("%s (built %s)", Version, BuildTime),
		RunE:    runDaemon,
	}

	// Flags
	rootCmd.Flags().String("data-dir", "", "Data directory (default: ~/.conduit)")
	rootCmd.Flags().String("socket", "", "Unix socket path (default: ~/.conduit/conduit.sock)")
	rootCmd.Flags().String("log-level", "info", "Log level: debug, info, warn, error")
	rootCmd.Flags().String("log-format", "json", "Log format: json, console")
	rootCmd.Flags().Bool("foreground", false, "Run in foreground (don't daemonize)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Override with command line flags
	if dataDir, _ := cmd.Flags().GetString("data-dir"); dataDir != "" {
		cfg.DataDir = dataDir
	}
	if socket, _ := cmd.Flags().GetString("socket"); socket != "" {
		cfg.SocketPath = socket
	}
	if logLevel, _ := cmd.Flags().GetString("log-level"); logLevel != "" {
		cfg.LogLevel = logLevel
	}
	if logFormat, _ := cmd.Flags().GetString("log-format"); logFormat != "" {
		cfg.LogFormat = logFormat
	}

	// Setup logging
	observability.SetupLogging(cfg.LogLevel, cfg.LogFormat, os.Stderr)

	// Set version info for daemon handlers
	daemon.Version = Version
	daemon.BuildTime = BuildTime

	// Create and run daemon
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("create daemon: %w", err)
	}

	return d.Run()
}
