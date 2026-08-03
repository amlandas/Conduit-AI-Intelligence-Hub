package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/kbservice"
)

// kbCmd is the parent command for knowledge base operations
func kbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge base operations",
		Long: `Manage the Conduit knowledge base.

The knowledge base indexes your documents for AI-powered search,
allowing AI clients to find relevant information quickly.

Examples:
  conduit kb add ./docs --name "My Docs"
  conduit kb list
  conduit kb sync
  conduit kb search "authentication"
  conduit kb stats`,
	}

	cmd.AddCommand(kbAddCmd())
	cmd.AddCommand(kbListCmd())
	cmd.AddCommand(kbRemoveCmd())
	cmd.AddCommand(kbSearchCmd())
	cmd.AddCommand(kbSyncCmd())
	cmd.AddCommand(kbStatsCmd())
	cmd.AddCommand(kbMigrateCmd())
	cmd.AddCommand(kbKagSyncCmd())
	cmd.AddCommand(kbKagStatusCmd())
	cmd.AddCommand(kbKagRetryCmd())
	cmd.AddCommand(kbKagDedupeCmd())
	cmd.AddCommand(kbKagVectorizeCmd())
	cmd.AddCommand(kbKagQueryCmd())

	return cmd
}

func kbStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show knowledge base statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openKB()
			if err != nil {
				return fmt.Errorf("get sources: %w", err)
			}
			defer svc.Close()

			totals, sources, err := svc.Stats(cmd.Context())
			if err != nil {
				return fmt.Errorf("get sources: %w", err)
			}

			fmt.Println("Knowledge Base Statistics")
			fmt.Println("═════════════════════════════════════════")

			if len(sources) == 0 {
				fmt.Println("No sources configured")
				return nil
			}

			fmt.Printf("Sources:     %d\n", totals.Sources)
			fmt.Printf("Documents:   %d\n", totals.Documents)
			fmt.Printf("Chunks:      %d\n", totals.Chunks)
			fmt.Printf("Total Size:  %s\n", formatBytes(totals.TotalBytes))
			fmt.Println()
			fmt.Println("By Source:")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-20s %-8s %-8s %s\n", "NAME", "DOCS", "CHUNKS", "SIZE")

			for _, source := range sources {
				fmt.Printf("%-20s %-8d %-8d %s\n",
					truncate(source.Name, 20), source.DocCount, source.ChunkCount,
					formatBytes(source.SizeBytes))
			}

			return nil
		},
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration as a human-readable string (e.g., "2h 15m", "45m 30s")
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func kbAddCmd() *cobra.Command {
	var name string
	var patterns string
	var excludes string
	var syncMode string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a folder to the knowledge base",
		Long: `Add a folder to the knowledge base for document indexing.

The folder will be scanned for matching files which are then indexed
for full-text search. By default, common text and code files are indexed.

Examples:
  conduit kb add ./docs --name "Project Docs"
  conduit kb add /path/to/notes --patterns "*.md,*.txt"
  conduit kb add ./src --excludes "node_modules,dist"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := args[0]

			// Resolve to absolute path
			absPath, err := filepath.Abs(sourcePath)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"resolve path: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("resolve path: %w", err)
			}

			// Check path exists
			info, err := os.Stat(absPath)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"path not accessible: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("path not accessible: %w", err)
			}
			if !info.IsDir() {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"path is not a directory: %s"}`, absPath)
					return nil
				}
				return fmt.Errorf("path is not a directory: %s", absPath)
			}

			// Build request
			req := kb.AddSourceRequest{Path: absPath}
			if name != "" {
				req.Name = name
			} else {
				req.Name = filepath.Base(absPath)
			}
			if patterns != "" {
				req.Patterns = strings.Split(patterns, ",")
			}
			if excludes != "" {
				req.Excludes = strings.Split(excludes, ",")
			}
			if syncMode != "" {
				req.SyncMode = syncMode
			}

			svc, err := openKB()
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"add source: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("add source: %w", err)
			}
			defer svc.Close()

			source, warnings, err := svc.AddSourceWithWarnings(cmd.Context(), req)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"add source: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("add source: %w", err)
			}

			// JSON output for GUI consumption: the source object, exactly as the
			// daemon's POST /kb/sources returned it.
			if jsonOutput {
				data, merr := json.Marshal(source)
				if merr != nil {
					fmt.Printf(`{"success":false,"error":"marshal source: %s"}`, merr.Error())
					return nil
				}
				fmt.Println(string(data))
				return nil
			}

			for _, w := range warnings {
				fmt.Printf("! %s\n", w)
			}
			if len(warnings) > 0 {
				fmt.Println()
			}

			fmt.Printf("✓ Added source: %s\n", source.Name)
			fmt.Printf("  ID:   %s\n", source.SourceID)
			fmt.Printf("  Path: %s\n", absPath)
			fmt.Println()
			fmt.Println("Run 'conduit kb sync' to index documents")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Display name for the source")
	cmd.Flags().StringVar(&patterns, "patterns", "", "File patterns to index (comma-separated, e.g., '*.md,*.txt')")
	cmd.Flags().StringVar(&excludes, "excludes", "", "Directories to exclude (comma-separated, e.g., 'node_modules,dist')")
	cmd.Flags().StringVar(&syncMode, "sync", "manual", "Sync mode: manual or auto")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func kbListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List knowledge base sources",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openKB()
			if err != nil {
				return fmt.Errorf("failed to list KB sources: %w", err)
			}
			defer svc.Close()

			list, err := svc.ListSources(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to list KB sources: %w", err)
			}

			// JSON output for GUI consumption: {"sources":[...],"count":N},
			// exactly as the daemon's GET /kb/sources returned it.
			if jsonOutput {
				data, merr := json.Marshal(list)
				if merr != nil {
					return fmt.Errorf("failed to list KB sources: %w", merr)
				}
				fmt.Println(string(data))
				return nil
			}

			if len(list.Sources) == 0 {
				fmt.Println("No knowledge base sources configured")
				return nil
			}

			fmt.Printf("%-20s %-40s %-10s\n", "NAME", "PATH", "DOCS")
			for _, s := range list.Sources {
				fmt.Printf("%-20s %-40s %-10v\n",
					s.Name,
					s.Path,
					s.DocCount,
				)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

func kbRemoveCmd() *cobra.Command {
	var force bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "remove <name-or-id>",
		Short: "Remove a knowledge base source",
		Long: `Remove a knowledge base source and all its indexed documents.

Use 'conduit kb list' to see source names.

Examples:
  conduit kb remove "User Files"
  conduit kb remove test --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nameOrID := args[0]

			svc, err := openKB()
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"failed to list sources: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("failed to list sources: %w", err)
			}
			defer svc.Close()

			// Find matching source by ID, name, or path
			matchedSource, err := svc.FindSource(cmd.Context(), nameOrID)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"source not found: %s"}`, nameOrID)
					return nil
				}
				return fmt.Errorf("source not found: %s\nUse 'conduit kb list' to see available sources", nameOrID)
			}

			sourceID := matchedSource.SourceID
			sourceName := matchedSource.Name
			docCount := matchedSource.DocCount

			// JSON mode implies force (non-interactive)
			if !jsonOutput && !force && docCount > 0 {
				fmt.Printf("Source '%s' has %d indexed documents.\n", sourceName, docCount)
				if !confirmAction("Remove source and all documents?") {
					fmt.Println("Cancelled")
					return nil
				}
			}

			// Delete the source and get deletion statistics
			deleteResult, err := svc.RemoveSource(cmd.Context(), sourceID)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"remove source failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("remove source: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				result := map[string]interface{}{
					"success":           true,
					"source_id":         sourceID,
					"source_name":       sourceName,
					"documents_deleted": deleteResult.DocumentsDeleted,
					"vectors_deleted":   deleteResult.VectorsDeleted,
				}
				jsonBytes, _ := json.Marshal(result)
				fmt.Println(string(jsonBytes))
				return nil
			}

			if deleteResult.VectorsDeleted > 0 {
				fmt.Printf("✓ Removed source: %s (%d documents, %d vectors)\n",
					sourceName, deleteResult.DocumentsDeleted, deleteResult.VectorsDeleted)
			} else {
				fmt.Printf("✓ Removed source: %s (%d documents)\n", sourceName, docCount)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func kbSearchCmd() *cobra.Command {
	var semantic, fts5, raw, jsonOutput bool
	var contextChunks, limit int
	var minScore float64
	var recallMode string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the knowledge base",
		Long: `Search the knowledge base using hybrid, semantic, or keyword search.

By default, hybrid search uses RRF (Reciprocal Rank Fusion) to combine
results from both semantic (vector) and lexical (FTS5) search. This gives
the best of both worlds: semantic understanding AND exact phrase matching.

The hybrid mode automatically detects:
- Quoted phrases → prioritizes lexical exact matching
- Proper nouns (e.g., "Oak Ridge") → boosts exact matches
- Natural language → balances semantic and lexical

Results are processed by default (merged chunks, filtered boilerplate).
Use --raw to get unprocessed results.

ADVANCED MODE: retrieval tuning:
  --recall            Precision/recall preset: high, balanced (default), precise
  --min-score         Minimum similarity threshold for --semantic (0.0-1.0)

Examples:
  conduit kb search "how does authentication work"    # Hybrid RRF (default)
  conduit kb search "Oak Ridge laboratories"          # Auto-detects proper noun
  conduit kb search "authentication" --semantic       # Force semantic only
  conduit kb search "class AuthProvider" --fts5       # Force keyword only
  conduit kb search "query" --raw                     # Raw chunks without processing

  # Advanced: widen recall, keeping every potentially relevant chunk
  conduit kb search "ASL-3 safeguards" --recall high

  # Advanced: Pure semantic search with low threshold
  conduit kb search "AI safety deployment" --semantic --min-score 0.0

  # Advanced: fewer, more distinct results
  conduit kb search "authentication" --recall precise`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Determine search mode
			if semantic && fts5 {
				return fmt.Errorf("cannot use both --semantic and --fts5 flags")
			}
			searchReq := kbservice.NewSearchRequest(query)
			if semantic {
				searchReq.Mode = kbservice.SearchModeSemantic
			} else if fts5 {
				searchReq.Mode = kbservice.SearchModeFTS5
			}
			searchReq.Raw = raw
			searchReq.Limit = limit
			searchReq.MinScore = minScore
			searchReq.RecallMode = recallMode
			// --context is accepted and ignored, as it was by the HTTP layer.
			_ = contextChunks

			svc, err := openKB()
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"search failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("search failed: %w", err)
			}
			defer svc.Close()

			result, err := svc.Search(cmd.Context(), searchReq)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"search failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("search failed: %w", err)
			}

			data, err := json.Marshal(result)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"search failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("search failed: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}

			// The rendering below walks the response generically. Decoding the
			// marshalled form keeps it byte-identical to the HTTP era: numbers
			// arrive as float64 and structs as maps, which is what it expects.
			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			results, _ := resp["results"].([]interface{})
			searchMode, _ := resp["search_mode"].(string)

			if len(results) == 0 {
				fmt.Printf("No results found for: %s\n", query)
				return nil
			}

			// Show search mode indicator
			modeLabel := ""
			switch searchMode {
			case "semantic":
				modeLabel = " [semantic]"
			case "fts5", "lexical":
				modeLabel = " [keyword]"
			case "fusion":
				modeLabel = " [hybrid RRF]"
			case "auto":
				modeLabel = " [hybrid]"
			}

			// Check if results are processed (merged)
			isProcessed, _ := resp["processed"].(bool)
			if isProcessed {
				modeLabel += " [processed]"
			}

			fmt.Printf("Found %v results for: %s%s\n\n", resp["total_hits"], query, modeLabel)

			// Display results based on whether they're processed or raw
			if isProcessed {
				// Processed results have merged content
				for _, r := range results {
					result := r.(map[string]interface{})
					path, _ := result["path"].(string)
					content, _ := result["content"].(string)
					chunkCount := 1
					if cc, ok := result["chunk_count"].(float64); ok {
						chunkCount = int(cc)
					}

					// Extract filename for cleaner display
					parts := strings.Split(path, "/")
					filename := path
					if len(parts) > 0 {
						filename = parts[len(parts)-1]
					}

					if chunkCount > 1 {
						fmt.Printf("• %s (%d chunks merged)\n", filename, chunkCount)
					} else {
						fmt.Printf("• %s\n", filename)
					}
					fmt.Printf("  Path: %s\n", path)
					fmt.Printf("  %s\n\n", content)
				}
			} else {
				// Raw results show individual chunks
				for _, r := range results {
					result := r.(map[string]interface{})
					path, _ := result["path"].(string)
					snippet, _ := result["snippet"].(string)

					// Show confidence for semantic results
					confidence, hasConfidence := result["confidence"].(string)
					if hasConfidence && confidence != "" {
						fmt.Printf("• %s [%s]\n  %s\n\n", path, confidence, snippet)
					} else {
						fmt.Printf("• %s\n  %s\n\n", path, snippet)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&semantic, "semantic", false, "Force semantic search (requires Ollama for query embedding)")
	cmd.Flags().BoolVar(&fts5, "fts5", false, "Force FTS5 keyword search")
	cmd.Flags().BoolVar(&raw, "raw", false, "Return raw chunks without processing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	cmd.Flags().IntVar(&contextChunks, "context", 0, "Number of adjacent chunks to include")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum results to return (default: 10)")

	// Advanced RAG tuning flags
	cmd.Flags().Float64Var(&minScore, "min-score", -1, "Minimum similarity threshold for --semantic (0.0-1.0)")
	cmd.Flags().StringVar(&recallMode, "recall", "", "Precision/recall preset: high, balanced, precise")

	return cmd
}

func kbSyncCmd() *cobra.Command {
	var rebuildVectors bool

	cmd := &cobra.Command{
		Use:   "sync [source-id]",
		Short: "Sync knowledge base sources",
		Long: `Synchronize knowledge base sources to index new and updated documents.

If a source ID is provided, only that source is synced.
If no source ID is provided, all sources are synced.

Exit Codes:
  0  Full success (FTS + semantic indexing)
  1  Error (sync failed)
  2  Partial success (FTS only, semantic indexing failed)

Examples:
  conduit kb sync                    # Sync all sources
  conduit kb sync abc123-def456      # Sync specific source
  conduit kb sync --rebuild-vectors  # Force rebuild vector index`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Indexing large corpora is slow; there is no request deadline to
			// worry about any more, only the user's patience.
			svc, err := openKB()
			if err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}
			defer svc.Close()

			if len(args) > 0 {
				// Sync specific source
				sourceID := args[0]

				if rebuildVectors {
					fmt.Printf("Rebuilding vectors for source: %s\n", sourceID)
				} else {
					fmt.Printf("Syncing source: %s\n", sourceID)
				}

				syncResult, err := svc.Sync(cmd.Context(), sourceID, rebuildVectors)
				if err != nil {
					return fmt.Errorf("sync failed: %w", err)
				}

				data, err := json.Marshal(syncResult)
				if err != nil {
					return fmt.Errorf("sync failed: %w", err)
				}
				var result map[string]interface{}
				json.Unmarshal(data, &result)

				added := int(result["added"].(float64))
				updated := int(result["updated"].(float64))
				deleted := int(result["deleted"].(float64))

				fmt.Printf("✓ Sync complete\n")
				fmt.Printf("  Added:   %d documents\n", added)
				fmt.Printf("  Updated: %d documents\n", updated)
				fmt.Printf("  Deleted: %d documents\n", deleted)

				// Show semantic search status
				semanticErrors := 0
				if semanticEnabled, ok := result["semantic_enabled"].(bool); ok {
					if semanticEnabled {
						if se, ok := result["semantic_errors"].(float64); ok {
							semanticErrors = int(se)
						}
						if semanticErrors > 0 {
							fmt.Printf("  Vectors: ⚠️  %d documents failed (FTS5 fallback used)\n", semanticErrors)
						} else {
							fmt.Printf("  Vectors: ✓ indexed\n")
						}
					} else {
						fmt.Printf("  Vectors: disabled (semantic search unavailable)\n")
					}
				}

				// Exit with code 2 for partial success (semantic errors)
				if semanticErrors > 0 {
					fmt.Println()
					fmt.Println("   To diagnose: conduit doctor")
					fmt.Println("   To retry:    conduit kb sync --rebuild-vectors")
					return exitWith(2)
				}

				if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
					fmt.Printf("  Errors:  %d\n", len(errors))
					for _, e := range errors {
						errInfo := e.(map[string]interface{})
						fmt.Printf("    - %s: %s\n", errInfo["path"], errInfo["message"])
					}
				}
			} else {
				// Sync all sources
				if rebuildVectors {
					fmt.Println("Rebuilding vector index for all sources...")
				} else {
					fmt.Println("Syncing all sources...")
				}

				// Get list of sources
				list, err := svc.ListSources(cmd.Context())
				if err != nil {
					return fmt.Errorf("list sources: %w", err)
				}

				sources := list.Sources
				if len(sources) == 0 {
					fmt.Println("No sources to sync")
					return nil
				}

				totalAdded := 0
				totalUpdated := 0
				totalDeleted := 0
				totalSemanticErrors := 0
				semanticEnabled := false

				for _, source := range sources {
					sourceID := source.SourceID
					sourceName := source.Name

					fmt.Printf("  Syncing: %s... ", sourceName)

					syncResult, err := svc.Sync(cmd.Context(), sourceID, rebuildVectors)
					if err != nil {
						fmt.Printf("ERROR: %v\n", err)
						continue
					}

					syncData, err := json.Marshal(syncResult)
					if err != nil {
						fmt.Printf("ERROR: %v\n", err)
						continue
					}

					var result map[string]interface{}
					json.Unmarshal(syncData, &result)

					// Safely extract numeric fields with nil checks
					var added, updated, deleted, semanticErrors int
					if v, ok := result["added"].(float64); ok {
						added = int(v)
					}
					if v, ok := result["updated"].(float64); ok {
						updated = int(v)
					}
					if v, ok := result["deleted"].(float64); ok {
						deleted = int(v)
					}
					if v, ok := result["semantic_errors"].(float64); ok {
						semanticErrors = int(v)
					}
					if v, ok := result["semantic_enabled"].(bool); ok && v {
						semanticEnabled = true
					}

					totalAdded += added
					totalUpdated += updated
					totalDeleted += deleted
					totalSemanticErrors += semanticErrors

					if semanticErrors > 0 {
						fmt.Printf("done (+%d/~%d/-%d) ⚠️  %d vector errors\n", added, updated, deleted, semanticErrors)
					} else {
						fmt.Printf("done (+%d/~%d/-%d)\n", added, updated, deleted)
					}
				}

				fmt.Println()
				fmt.Printf("✓ Sync complete: %d added, %d updated, %d deleted\n",
					totalAdded, totalUpdated, totalDeleted)

				// Show semantic search summary with actionable guidance
				if totalSemanticErrors > 0 {
					fmt.Println()
					fmt.Printf("⚠️  Semantic indexing failed for %d documents (FTS5 fallback used)\n", totalSemanticErrors)
					fmt.Println("   Search will use keyword matching only for affected documents.")
					fmt.Println()
					fmt.Println("   To diagnose: conduit doctor")
					fmt.Println("   To retry:    conduit kb sync --rebuild-vectors")
					// Return exit code 2 for partial success
					return exitWith(2)
				} else if semanticEnabled && (totalAdded > 0 || totalUpdated > 0) {
					fmt.Println("   Vectors: ✓ all documents indexed")
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&rebuildVectors, "rebuild-vectors", false, "Force rebuild of vector index for all documents")
	return cmd
}

func kbMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrate FTS documents to vector search",
		Long: `Migrate existing FTS5-indexed documents to the vector search index.

This is required to enable semantic search for documents that were indexed
before semantic search was enabled. New documents are automatically indexed
in both FTS5 and vector search.

Requires an embedding provider (see 'embed.provider' in the config); it fails
when that is set to "none".

Examples:
  conduit kb migrate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openKB()
			if err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}
			defer svc.Close()

			fmt.Println("Migrating documents to vector search...")
			fmt.Println("This may take a while for large knowledge bases.")
			fmt.Println()

			migrated, err := svc.Migrate(cmd.Context(), nil)
			if err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			fmt.Printf("✓ Migration complete: %d documents migrated to vector search\n", migrated)

			return nil
		},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// confirmAction prompts the user for confirmation
func confirmAction(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
