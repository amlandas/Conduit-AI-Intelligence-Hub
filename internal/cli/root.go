// Package cli is Conduit's command surface.
//
// Every command here is a thin frontend over a library call. There is no
// daemon, no socket and no HTTP client: a command opens the knowledge base
// (internal/kbservice), does the work in-process and exits. That is the whole
// architecture, and it is why `conduit kb search` works on a machine where
// nothing else is running.
//
// # Output is a contract
//
// Human-readable output and every `--json` shape are consumed by scripts and
// by the frozen desktop GUI. Treat them as API. When a command's backend was
// removed outright, it is a documented removal (see removed.go) rather than a
// silently changed format.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kbservice"
)

// Build information, injected by main.
var (
	version   = "dev"
	buildTime = "unknown"
)

// globalFlags is the root command's persistent flag set. It is consulted by
// loadConfig so that --db and friends outrank the config file, which is the
// documented precedence.
var globalFlags *pflag.FlagSet

// exitError carries a process exit code out of a command without adding a
// message: the command has already printed everything the user needs.
//
// It exists so no command has to call os.Exit half way through its own RunE.
// That mattered for correctness (an os.Exit skips every deferred Close, so the
// knowledge base handle was never released) and it is what makes the documented
// exit codes -- `kb sync` returning 2 on partial success, `doctor` returning 1
// on a failed check -- testable in-process.
type exitError struct{ code int }

func (e exitError) Error() string { return "" }

// exitWith returns an error that ends the process with the given code.
func exitWith(code int) error { return exitError{code: code} }

// Execute builds the root command and runs it, returning a process exit code.
func Execute(v, bt string) int {
	version, buildTime = v, bt

	root := NewRootCommand()
	err := root.Execute()
	if err == nil {
		return 0
	}

	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}

	fmt.Fprintln(os.Stderr, "Error:", err)
	return 1
}

// NewRootCommand builds the full command tree.
//
// It is exported so tests can execute commands in-process against a temporary
// knowledge base rather than shelling out to a built binary.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "conduit",
		Short: "Conduit - local-first private knowledge base for AI tools",
		Long: `Conduit is a local-first knowledge base that AI clients query over MCP.

It is a single binary. There is no daemon and no background service: every
command opens the knowledge base file, does its work, and exits. Point an AI
client at 'conduit mcp kb' and it can search your documents.

Examples:
  conduit kb add ./docs --name "Project Docs"
  conduit kb sync
  conduit kb search "how does authentication work"
  conduit mcp configure`,
		Version:      fmt.Sprintf("%s (built %s)", version, buildTime),
		SilenceUsage: true,
		// Execute prints errors itself so that an exitError, which carries only
		// a code, does not surface as a blank "Error:" line.
		SilenceErrors: true,
	}

	// Global flags. --db is the workspace-isolation seam: it points the whole
	// CLI at a different knowledge base file, so a project can keep its own.
	root.PersistentFlags().String("db", "",
		"Path to the knowledge base SQLite file (default <data-dir>/conduit.db)")
	root.PersistentFlags().String("data-dir", "",
		"Conduit data directory (default ~/.conduit)")
	root.PersistentFlags().String("log-level", "",
		"Log level: debug, info, warn, error")

	globalFlags = root.PersistentFlags()

	root.AddCommand(kbCmd())
	root.AddCommand(mcpCmd())
	root.AddCommand(configCmd())
	root.AddCommand(doctorCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(setupCmd())
	root.AddCommand(modelCmd())
	root.AddCommand(backupCmd())
	root.AddCommand(uninstallCmd())
	root.AddCommand(ollamaCmd())
	root.AddCommand(versionCmd())

	addRemovedCommands(root)

	return root
}

// versionCmd prints version information.
func versionCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				fmt.Printf("{\"version\":%q,\"build_time\":%q}\n", version, buildTime)
				return nil
			}
			fmt.Printf("conduit %s (built %s)\n", version, buildTime)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

// loadConfig loads configuration honouring the global flags.
//
// Precedence: flags > environment > file > defaults.
func loadConfig() (*config.Config, error) {
	res, err := config.LoadWithFlags(globalFlags)
	if err != nil {
		return nil, err
	}
	// Unknown keys are reported once, on stderr, so that stdout stays parseable
	// for --json consumers.
	if msg := config.FormatUnknownKeys(res.File, res.UnknownKeys); msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	return res.Config, nil
}

// kbPath returns the knowledge base file path, honouring --db, and makes sure
// the directory it names exists.
//
// It exists so that no command hardcodes ~/.conduit/conduit.db; that
// duplication is what made workspace isolation impossible before. Creating the
// directory is part of the job: every caller is about to open a database there,
// and --db may well point somewhere that has never been used.
func kbPath() string {
	cfg, err := loadConfig()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".conduit", "conduit.db")
	}
	// Best effort: if this fails, opening the database fails next with a
	// better message than anything reportable from here.
	_ = cfg.EnsureDirectories()
	return cfg.DatabasePath()
}

// openKB opens the knowledge base for a command.
//
// Callers must Close the returned service.
func openKB() (*kbservice.Service, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return kbservice.Open(cfg)
}
