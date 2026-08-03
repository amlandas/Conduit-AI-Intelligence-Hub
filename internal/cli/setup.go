package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/embed"
	setuppkg "github.com/simpleflo/conduit/internal/setup"
)

// setupCmd prepares a machine to use Conduit.
//
// The v1 wizard installed Podman, pulled container images, started Qdrant and
// FalkorDB, and registered a launchd/systemd service. None of that exists any
// more, and pretending otherwise was the main reason setup could fail on a
// machine where Conduit would have worked fine. What remains is genuinely
// needed: a data directory, an initialised knowledge base, optional document
// extraction tools, and an AI client that knows where to find the MCP server.
func setupCmd() *cobra.Command {
	var skipTools bool
	var client string
	var downloadModel bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Prepare this machine to use Conduit",
		Long: `Initialise Conduit.

Steps:
  1. create the data directory and the knowledge base file
  2. install optional document extraction tools (PDF, DOC, RTF)
  3. optionally download the embedding model (--model)
  4. configure the MCP server in an AI client
  5. report what still needs attention

There is no service to install and no containers to pull: Conduit is one
binary that runs when you call it.

The embedding model is a few hundred megabytes and is NOT downloaded unless
--model is passed, because setup should not start a large transfer on a
metered connection without being asked. Without it, search works on keyword
matching until you run 'conduit model download'.

Examples:
  conduit setup
  conduit setup --model
  conduit setup --skip-tools
  conduit setup --client cursor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			fmt.Println("Conduit Setup")
			fmt.Println("═══════════════════════════════════════════════════════")

			// ---- 1. data directory + knowledge base ------------------------
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			svc, err := openKB()
			if err != nil {
				return fmt.Errorf("initialise knowledge base: %w", err)
			}
			fmt.Printf("\n✓ Knowledge base ready: %s\n", svc.DatabasePath())
			svc.Close()

			// ---- 2. document extraction tools ------------------------------
			if skipTools {
				fmt.Println("- Skipping document extraction tools (--skip-tools)")
			} else {
				fmt.Println("\nChecking document extraction tools...")
				results, terr := setuppkg.InstallDocumentTools(ctx, false)
				if terr != nil {
					fmt.Printf("  ⚠ %v\n", terr)
				}
				for _, r := range results {
					if r.Success {
						fmt.Printf("  ✓ %s\n", r.Name)
					} else {
						fmt.Printf("  ⚠ %s: %s\n", r.Name, r.Message)
					}
				}
				if len(results) == 0 {
					fmt.Println("  ✓ nothing to install")
				}
			}

			// ---- 3. embedding model ----------------------------------------
			switch {
			case !downloadModel:
				fmt.Println("\n- Skipping embedding model download (pass --model to fetch it)")
			case cfg.Embed.Provider != config.EmbedProviderLlamaServer:
				fmt.Printf("\n- Skipping model download: embed.provider is %q, which does not use a local GGUF\n",
					cfg.Embed.Provider)
			default:
				if err := downloadConfiguredModel(ctx, cfg); err != nil {
					fmt.Printf("  ⚠ %v\n", err)
					fmt.Println("  Run 'conduit model download' to retry.")
				}
			}

			// ---- 4. MCP client ---------------------------------------------
			fmt.Printf("\nConfiguring MCP server for %s...\n", client)
			if err := configureMCPClient(client, false); err != nil {
				fmt.Printf("  ⚠ %v\n", err)
				fmt.Println("  Run 'conduit mcp configure --client <name>' to retry.")
			}

			// ---- 5. what still needs attention ------------------------------
			fmt.Println("\nDiagnostics")
			fmt.Println("───────────────────────────────────────────────────────")
			for _, c := range runDoctor(ctx, 15*time.Second) {
				if c.Status == checkOK || c.Status == checkSkip {
					continue
				}
				fmt.Printf("  %s %s: %s\n", c.icon(), c.Name, c.Detail)
				if c.Remedy != "" {
					fmt.Printf("      → %s\n", c.Remedy)
				}
			}

			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  conduit kb add <folder>    # index a folder")
			fmt.Println("  conduit kb sync            # build the index")
			fmt.Println("  conduit kb search \"query\"   # check it works")
			if cfg.Embed.Provider == config.EmbedProviderNone {
				fmt.Println()
				fmt.Println("Note: embeddings are disabled (embed.provider = none).")
				fmt.Println("Search will use keyword matching only.")
			} else if !downloadModel {
				fmt.Println("  conduit model download     # enable semantic search")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&skipTools, "skip-tools", false, "Skip document extraction tool installation")
	cmd.Flags().BoolVar(&downloadModel, "model", false,
		"Download and verify the embedding model (a few hundred MB)")
	cmd.Flags().StringVarP(&client, "client", "c", "claude-code",
		"AI client to configure (claude-code, cursor, vscode)")

	return cmd
}

// downloadConfiguredModel fetches the model this machine is configured to use.
//
// It shares the downloader and the progress renderer with `conduit model
// download`, so setup cannot drift into a different download path with
// different verification.
func downloadConfiguredModel(ctx context.Context, cfg *config.Config) error {
	spec, err := embed.LookupModel(resolveModelID(cfg, nil))
	if err != nil {
		return err
	}

	// Say nothing beyond a confirmation when the model is already there: a
	// re-run of setup on a working machine should be quiet.
	if st := embed.StatModel(spec, cfg.DataDir, false); st.Present {
		if verr := embed.VerifyModel(spec, cfg.DataDir); verr == nil {
			fmt.Printf("\n✓ Embedding model already present: %s\n", spec.ID)
			return nil
		}
	}

	fmt.Printf("\nDownloading embedding model %s (%s)...\n", spec.ID, humanBytes(spec.SizeBytes))
	d := &embed.Downloader{Progress: newProgressPrinter(os.Stderr)}

	res, err := d.Download(ctx, spec, cfg.DataDir)
	if err != nil {
		return err
	}
	fmt.Printf("✓ Embedding model ready: %s\n", res.Path)
	return nil
}
