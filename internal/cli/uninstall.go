package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	setuppkg "github.com/simpleflo/conduit/internal/setup"
)

// exitCodeUserCancelled is returned when the user declines a confirmation.
//
// It has to be distinguishable from both success and failure. A wrapper script
// that cannot tell "the user typed something other than UNINSTALL" from "the
// uninstall completed" will carry on and remove the binary the user just
// declined to remove -- which is precisely what happened before this existed.
const exitCodeUserCancelled = 3

// uninstallCmd removes Conduit
func uninstallCmd() *cobra.Command {
	var (
		keepData   bool
		all        bool
		force      bool
		dryRun     bool
		jsonOutput bool
		showInfo   bool
		prefix     string
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Conduit",
		Long: `Remove the Conduit binary, its MCP client entries, its PATH lines, and
optionally its data.

UNINSTALL OPTIONS:
  --keep-data    Remove the binary and PATH entries, keep indexed data
  --all          Remove everything including the data directory

SAFETY FLAGS:
  --force        Skip all confirmations
  --dry-run      Show what would be removed without removing
  --json         Output results as JSON

Data is kept unless you ask for it to go. --all is the only thing that deletes
the knowledge base, and it prompts unless --force is given.

NOTE: Conduit runs no service and no containers, so there is nothing of that
      kind to tear down. Tools you may share with other projects are never
      removed. To remove them yourself:
      - Ollama:  see https://ollama.com/download
      - poppler (pdftotext): brew uninstall poppler

      A machine that ran a Conduit 1.x installer also has a daemon service and
      container leftovers that this command knows nothing about. Remove those
      with scripts/remove-v1.sh, which defaults to --dry-run.

Examples:
  conduit uninstall                    # Interactive mode
  conduit uninstall --keep-data        # Keep data for reinstall
  conduit uninstall --all --force      # Remove data without prompts
  conduit uninstall --dry-run          # Preview what would be removed
  conduit uninstall --info             # Show what's installed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// --prefix scopes removal to one install directory; --all deletes
			// the shared data directory, which is not inside any prefix. Asking
			// for both is a contradiction, and the safe reading of it is not
			// obvious enough to guess at when the cost of guessing wrong is a
			// deleted knowledge base.
			//
			// Keyed off whether the flag was *given*, not whether it is
			// non-empty. `--prefix ""` set prefix to the empty string, which
			// slipped this check and then meant "no prefix" downstream: the
			// user asked to scope the removal and got a full-scope one.
			prefixGiven := cmd.Flags().Changed("prefix")
			if prefixGiven && strings.TrimSpace(prefix) == "" {
				return fmt.Errorf("--prefix requires a directory (got an empty value)")
			}
			if prefixGiven && all {
				return fmt.Errorf("--prefix and --all are mutually exclusive: " +
					"--prefix removes one install, --all removes the shared data directory.\n" +
					"Run them separately if you mean both")
			}

			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			dataDir := cfg.DataDir

			// Show info mode
			if showInfo {
				info, err := setuppkg.GetUninstallInfo(ctx, dataDir)
				if err != nil {
					return fmt.Errorf("failed to get uninstall info: %w", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(info)
				}
				setuppkg.PrintUninstallInfo(info)
				return nil
			}

			// Build options based on flags
			var opts setuppkg.UninstallOptions

			switch {
			case all:
				opts = setuppkg.NewUninstallOptionsAll()
			case keepData:
				opts = setuppkg.NewUninstallOptionsKeepData()
			default:
				// Interactive mode - show current state and ask
				info, err := setuppkg.GetUninstallInfo(ctx, dataDir)
				if err != nil {
					return fmt.Errorf("failed to get uninstall info: %w", err)
				}
				setuppkg.PrintUninstallInfo(info)

				// If nothing to remove, exit
				if !info.HasBinaries && !info.HasDataDir {
					fmt.Println("Nothing to uninstall.")
					return nil
				}

				// Interactive prompts
				fmt.Println("Choose uninstall option:")
				fmt.Println()
				fmt.Println("  [1] Keep Data - Remove the binary and PATH entries, keep indexed data")
				fmt.Println("  [2] Remove All - Remove the binary, PATH entries and all data")
				fmt.Println("  [q] Cancel")
				fmt.Println()

				reader := bufio.NewReader(os.Stdin)
				fmt.Print("Enter choice [1/2/q]: ")
				choice, _ := reader.ReadString('\n')
				choice = strings.TrimSpace(choice)

				switch choice {
				case "1":
					opts = setuppkg.NewUninstallOptionsKeepData()
				case "2":
					opts = setuppkg.NewUninstallOptionsAll()
				default:
					fmt.Println("Uninstallation cancelled.")
					return exitWith(exitCodeUserCancelled)
				}
			}

			opts.Force = force
			opts.DryRun = dryRun
			opts.JSON = jsonOutput
			opts.Prefix = prefix

			// Data removal is vetted and confirmed against the RESOLVED
			// directory.
			//
			// dataDir comes from config, and config precedence includes a
			// conduit.yaml in the working directory -- so the path about to be
			// deleted is not necessarily the one the user has in mind, and the
			// old prompt ("this will permanently delete all Conduit data")
			// never said which one it was. The single fact needed to catch the
			// mistake was the fact it omitted.
			if opts.RemoveDataDir && !dryRun {
				resolved, serr := setuppkg.AssertRemovableDataDir(dataDir)
				if serr != nil {
					return serr
				}
				dataDir = resolved
				summary := setuppkg.SummarizeDataDir(resolved)

				// A directory with none of Conduit's own files in it is almost
				// certainly a mistyped --data-dir. Deleting it recursively on
				// the strength of a typo is not a risk worth taking silently.
				if summary.Exists && !summary.LooksLikeConduit && !force {
					return fmt.Errorf("%s contains no conduit.db or conduit.yaml, so it does not look like a Conduit data directory.\n"+
						"Refusing to delete it. Check --data-dir, or pass --force if you are certain", resolved)
				}

				if !force {
					fmt.Println()
					fmt.Println("⚠  WARNING: this permanently deletes the directory below.")
					fmt.Println()
					fmt.Printf("    %s\n", resolved)
					if summary.Exists {
						fmt.Printf("    %s on disk\n", summary.Size)
					} else {
						fmt.Println("    (does not exist)")
					}
					fmt.Println()

					reader := bufio.NewReader(os.Stdin)
					fmt.Print("Type 'UNINSTALL' to confirm: ")
					confirm, rerr := reader.ReadString('\n')
					if rerr != nil || strings.TrimSpace(confirm) != "UNINSTALL" {
						fmt.Println("Uninstallation cancelled.")
						// Non-zero, and distinct from a failure. A wrapper
						// script has to be able to tell "the user said no" from
						// "it worked"; returning 0 here is what let uninstall.sh
						// carry on and delete the binary after the user had
						// just declined.
						return exitWith(exitCodeUserCancelled)
					}
				}
			}

			// Execute uninstall
			result, err := setuppkg.Uninstall(ctx, dataDir, opts)
			if err != nil {
				return fmt.Errorf("uninstall failed: %w", err)
			}

			// Strip the MCP entries. A client left pointing at a binary that no
			// longer exists fails at every startup with an error the user
			// cannot act on, so this is not gated on --all: the entry describes
			// the program, not the data.
			//
			// --prefix is the exception. It removes one install out of several,
			// and an MCP entry says `command: conduit` -- it belongs to
			// whichever copy is on PATH, which is exactly the one that flag
			// promised to leave alone.
			switch {
			case prefix != "":
				fmt.Fprintln(os.Stderr,
					"Note: --prefix given; MCP client entries, shell PATH lines and GUI state were left alone.")
			case dryRun:
				for _, id := range setuppkg.MCPClientIDs() {
					if client, lerr := setuppkg.LookupMCPClient(id); lerr == nil {
						if configured, _ := setuppkg.IsMCPClientConfigured(client.ConfigPath); configured {
							result.ItemsRemoved = append(result.ItemsRemoved,
								fmt.Sprintf("[DRY RUN] Would remove MCP entry from %s (%s)", id, client.ConfigPath))
						}
					}
				}
			default:
				removals, rerrs := setuppkg.RemoveAllMCPClients()
				for _, r := range removals {
					if r.Removed {
						result.ItemsRemoved = append(result.ItemsRemoved,
							fmt.Sprintf("MCP entry: %s (%s)", r.ClientID, r.ConfigPath))
					}
				}
				for _, rerr := range rerrs {
					result.ItemsFailed = append(result.ItemsFailed, fmt.Sprintf("MCP entry: %v", rerr))
					result.Errors = append(result.Errors, rerr.Error())
				}
			}

			// ALWAYS remove GUI state (Electron app userData)
			// This ensures a clean slate on reinstall, regardless of --keep-data flag
			// GUI state should NEVER persist independently of CLI state
			//
			// Except under --prefix, for the same reason as the MCP entries:
			// there is one GUI state directory per user, and it belongs to the
			// install that is actually in use.
			home, _ := os.UserHomeDir()
			electronDataDirs := []string{
				filepath.Join(home, "Library", "Application Support", "conduit-desktop"), // macOS
				filepath.Join(home, ".config", "conduit-desktop"),                        // Linux
			}
			if prefix != "" {
				electronDataDirs = nil
			}

			for _, dir := range electronDataDirs {
				if _, statErr := os.Stat(dir); statErr == nil {
					if dryRun {
						result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove GUI state: %s", dir))
					} else {
						if removeErr := os.RemoveAll(dir); removeErr != nil {
							result.ItemsFailed = append(result.ItemsFailed, fmt.Sprintf("GUI state: %s", dir))
							result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove GUI state %s: %v", dir, removeErr))
						} else {
							result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("GUI state: %s", dir))
						}
					}
				}
			}

			// Output results
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Print results
			fmt.Println()
			if dryRun {
				fmt.Println("═══════════════════════════════════════════════════════════════")
				fmt.Println("                     DRY RUN - No changes made                  ")
				fmt.Println("═══════════════════════════════════════════════════════════════")
			} else {
				fmt.Println("═══════════════════════════════════════════════════════════════")
				fmt.Println("                     Uninstallation Complete                    ")
				fmt.Println("═══════════════════════════════════════════════════════════════")
			}
			fmt.Println()

			if len(result.ItemsRemoved) > 0 {
				for _, item := range result.ItemsRemoved {
					fmt.Printf("  ✓ %s\n", item)
				}
			}

			if len(result.ItemsFailed) > 0 {
				fmt.Println()
				fmt.Println("Failed to remove:")
				for _, item := range result.ItemsFailed {
					fmt.Printf("  ✗ %s\n", item)
				}
			}

			if len(result.Errors) > 0 {
				fmt.Println()
				fmt.Println("Errors:")
				for _, err := range result.Errors {
					fmt.Printf("  • %s\n", err)
				}
			}

			// Print manual cleanup guidance
			if !dryRun && result.Success {
				fmt.Println()
				fmt.Println("Conduit never removes tools you may share with other projects.")
				fmt.Println("To remove them yourself, if nothing else needs them:")
				fmt.Println("  • Ollama:  rm -rf ~/.ollama && brew uninstall ollama")
				fmt.Println("  • poppler: brew uninstall poppler")
				fmt.Println()
				fmt.Println("If this machine ever ran a Conduit 1.x installer, it also has a")
				fmt.Println("daemon service and container leftovers. Remove them with:")
				fmt.Println("  • scripts/remove-v1.sh --dry-run   (then re-run with --yes)")
			}

			fmt.Println()

			return nil
		},
	}

	// Uninstall options
	cmd.Flags().BoolVar(&keepData, "keep-data", false, "Remove binaries/service, keep data for reinstall")
	cmd.Flags().BoolVar(&all, "all", false, "Remove everything including data directory")

	// Safety flags
	cmd.Flags().BoolVar(&force, "force", false, "Skip all confirmations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without removing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	cmd.Flags().BoolVar(&showInfo, "info", false, "Show installation status without uninstalling")
	cmd.Flags().StringVar(&prefix, "prefix", "",
		"Remove only the install in this directory, leaving any other copy alone")

	return cmd
}
