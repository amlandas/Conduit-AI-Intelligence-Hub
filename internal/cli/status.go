package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
)

// statusCmd reports the state of the knowledge base.
//
// Before WP-3.2 this command asked a daemon over a unix socket whether it was
// alive. There is no daemon, so "is it running?" is not a question any more:
// what an operator actually wants to know is whether the knowledge base is
// there, what is in it, and whether retrieval is wired up. That is what this
// reports. Deep diagnosis, with remedies, lives in `conduit doctor`.
func statusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show knowledge base status",
		Long: `Show the state of the knowledge base.

Conduit has no background service: there is nothing to start and nothing that
can be "down". This reports the knowledge base file, what it contains, and how
retrieval is configured.

For diagnosis with remedies, use 'conduit doctor'.

Examples:
  conduit status
  conduit status --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			svc, err := openKB()
			if err != nil {
				if jsonOutput {
					out := map[string]interface{}{
						"database":  cfg.DatabasePath(),
						"available": false,
						"error":     err.Error(),
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(out)
				}
				fmt.Println("Conduit Status")
				fmt.Println("═══════════════════════════════════════════")
				fmt.Printf("  Knowledge base: %s\n", cfg.DatabasePath())
				fmt.Printf("  State:          unavailable (%v)\n", err)
				fmt.Println()
				fmt.Println("Run 'conduit doctor' for diagnosis.")
				return nil
			}
			defer svc.Close()

			totals, sources, err := svc.Stats(ctx)
			if err != nil {
				return fmt.Errorf("read knowledge base: %w", err)
			}

			embedInfo := svc.EmbedInfo()
			vectors, _ := svc.VectorCount(ctx)

			searchMode := "lexical (FTS5)"
			if svc.SemanticAvailable() {
				searchMode = "hybrid (FTS5 + vectors)"
			}

			if jsonOutput {
				out := map[string]interface{}{
					"version":     version,
					"database":    svc.DatabasePath(),
					"available":   true,
					"sources":     totals.Sources,
					"documents":   totals.Documents,
					"chunks":      totals.Chunks,
					"total_bytes": totals.TotalBytes,
					"vectors":     vectors,
					"search_mode": searchMode,
					"embed": map[string]interface{}{
						"provider":   embedInfo.Provider,
						"model":      embedInfo.Model,
						"configured": embedInfo.Available,
					},
					"graph_enabled": cfg.KB.KAG.Enabled,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Println("Conduit Status")
			fmt.Println("═══════════════════════════════════════════")
			fmt.Printf("  Version:        %s\n", version)
			fmt.Printf("  Knowledge base: %s\n", svc.DatabasePath())
			fmt.Println()

			fmt.Println("📚 Content:")
			fmt.Printf("  Sources:      %d\n", totals.Sources)
			fmt.Printf("  Documents:    %d\n", totals.Documents)
			fmt.Printf("  Chunks:       %d\n", totals.Chunks)
			fmt.Printf("  Size:         %s\n", formatBytes(totals.TotalBytes))
			fmt.Println()

			fmt.Println("🔍 Retrieval:")
			fmt.Printf("  Mode:         %s\n", searchMode)
			if embedInfo.Provider == config.EmbedProviderNone {
				fmt.Println("  Embeddings:   disabled (embed.provider = none)")
			} else {
				fmt.Printf("  Embeddings:   %s (model %s)\n", embedInfo.Provider, embedInfo.Model)
				fmt.Printf("  Vectors:      %d\n", vectors)
			}
			fmt.Printf("  Graph:        %v\n", cfg.KB.KAG.Enabled)

			if len(sources) > 0 {
				fmt.Println()
				fmt.Println("📁 Sources:")
				for _, src := range sources {
					fmt.Printf("  • %-24s %d docs\n", truncate(src.Name, 24), src.DocCount)
				}
			} else {
				fmt.Println()
				fmt.Println("No sources yet. Run 'conduit kb add <path>' to index documents.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}
