package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

func kbKagSyncCmd() *cobra.Command {
	var force bool
	var provider string
	var advanced bool

	cmd := &cobra.Command{
		Use:   "kag-sync",
		Short: "Extract entities from indexed documents",
		Long: `Extract entities and relationships from indexed documents into the knowledge graph.

This command processes chunks from your knowledge base and extracts:
- Named entities (concepts, people, organizations, technologies, etc.)
- Relationships between entities (mentions, defines, relates_to, etc.)

The extracted graph enables multi-hop reasoning queries.

Examples:
  conduit kb kag-sync                    # Extract from all unprocessed chunks
  conduit kb kag-sync --force            # Re-extract from all chunks
  conduit kb kag-sync --advanced         # Show advanced options`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Check if KAG is enabled
			if !cfg.KB.KAG.Enabled {
				fmt.Println("KAG is not enabled. Enable it in your config:")
				fmt.Println()
				fmt.Println("  kb:")
				fmt.Println("    kag:")
				fmt.Println("      enabled: true")
				fmt.Println()
				fmt.Println("Or set CONDUIT_KB_KAG_ENABLED=true")
				return nil
			}

			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			// Create KAG config
			kagCfg := kb.DefaultKAGConfig()
			kagCfg.Enabled = true
			kagCfg.Extraction.EnableBackground = false // CLI does synchronous extraction
			if provider != "" {
				kagCfg.Provider = provider
			}

			// Check Ollama is running and model is available
			if kagCfg.Provider == "ollama" {
				if !checkOllamaRunning() {
					return fmt.Errorf("Ollama is not running.\n\nStart with: ollama serve")
				}

				// Check if model is available
				kagModel := kagCfg.Ollama.Model
				if kagModel == "" {
					kagModel = "mistral:7b-instruct-q4_K_M"
				}

				models, err := getOllamaModels()
				if err != nil {
					return fmt.Errorf("cannot list Ollama models: %w", err)
				}

				hasModel := false
				for _, m := range models {
					if strings.Contains(m, "mistral") {
						hasModel = true
						break
					}
				}

				if !hasModel {
					fmt.Printf("KAG extraction model not found: %s\n\n", kagModel)
					fmt.Println("Pull the model first:")
					fmt.Printf("  ollama pull %s\n\n", kagModel)
					fmt.Println("This may take a few minutes to download (~4GB).")
					return nil
				}

				fmt.Printf("Using extraction model: %s\n", kagModel)
			}

			// Create provider
			factory := kb.NewProviderFactory()
			llmProvider, err := factory.CreateProvider(kagCfg)
			if err != nil {
				return fmt.Errorf("create LLM provider: %w", err)
			}
			defer llmProvider.Close()

			// Create the graph store. WP-2.3 replaced the FalkorDB container
			// with edge tables in this same SQLite file, so there is nothing to
			// connect to and nothing that can be unavailable. Running kag-sync is
			// an explicit request to populate the graph, so the schema is created
			// here if it does not exist yet.
			graphStore := kb.NewGraphStore(db.DB(), true)
			if err := graphStore.EnsureSchema(cmd.Context()); err != nil {
				return fmt.Errorf("create graph tables: %w", err)
			}

			// Create entity extractor
			extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
				Provider:   llmProvider,
				DB:         db.DB(),
				GraphStore: graphStore,
				Config:     kagCfg,
				NumWorkers: 2,
			})
			if err != nil {
				return fmt.Errorf("create extractor: %w", err)
			}
			defer extractor.Close()

			// Count total chunks to process FIRST (before opening cursor)
			ctx := cmd.Context()
			var totalChunks int
			if force {
				db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_chunks").Scan(&totalChunks)
			} else {
				db.DB().QueryRowContext(ctx, `
					SELECT COUNT(*) FROM kb_chunks c
					LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					WHERE s.status IS NULL OR s.status = 'error'
				`).Scan(&totalChunks)
			}

			if totalChunks == 0 {
				fmt.Println("No chunks to process. All documents have been extracted.")
				fmt.Println()
				fmt.Println("Use --force to re-extract all chunks.")
				return nil
			}

			// Now query the actual chunks to process (include title to avoid nested queries)
			var query string
			if force {
				query = `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, '')
					FROM kb_chunks c
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					ORDER BY c.chunk_id
				`
			} else {
				query = `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, '')
					FROM kb_chunks c
					LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					WHERE s.status IS NULL OR s.status = 'error'
					ORDER BY c.chunk_id
				`
			}

			rows, err := db.DB().QueryContext(ctx, query)
			if err != nil {
				return fmt.Errorf("query chunks: %w", err)
			}

			// Collect all chunks into slice FIRST to avoid SQLite cursor conflicts
			// (storeEntities uses transactions which conflict with open cursors)
			type chunkData struct {
				ChunkID    string
				DocumentID string
				Content    string
				Title      string
			}
			var chunks []chunkData
			for rows.Next() {
				var c chunkData
				if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.Content, &c.Title); err != nil {
					continue
				}
				chunks = append(chunks, c)
			}
			rows.Close() // Close cursor BEFORE processing

			// Process chunks
			var processed, errors int
			fmt.Printf("Extracting entities from %d chunks...\n", totalChunks)
			fmt.Println()

			// Auto-warmup: Check if model is loaded and warm it up if not
			fmt.Print("Checking model status... ")
			os.Stdout.Sync()

			ollamaBin := findOllamaBinary()
			psOut, psErr := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			modelLoaded := psErr == nil && strings.Contains(string(psOut), "mistral")

			if modelLoaded {
				fmt.Println("✓ Model already loaded")
			} else {
				fmt.Println("model not loaded")
				fmt.Print("Warming up mistral model (this may take 1-2 minutes)... ")
				os.Stdout.Sync()

				warmupStart := time.Now()
				warmupCmd := exec.CommandContext(ctx, ollamaBin, "run", "mistral:7b-instruct-q4_K_M", "hello")
				warmupCmd.Stdin = strings.NewReader("")
				if err := warmupCmd.Run(); err != nil {
					fmt.Printf("✗ warmup failed: %v\n", err)
					fmt.Println("Continuing anyway - first extraction will be slower.")
				} else {
					fmt.Printf("✓ ready (%s)\n", formatDuration(time.Since(warmupStart)))
				}
			}
			fmt.Println()
			os.Stdout.Sync() // Flush output before blocking extraction calls

			// Track timing for ETA calculation
			var totalElapsed time.Duration
			syncStartTime := time.Now()

			for _, chunk := range chunks {
				chunkID := chunk.ChunkID
				documentID := chunk.DocumentID
				content := chunk.Content
				title := chunk.Title

				// Show progress with ETA
				current := processed + errors + 1
				remaining := totalChunks - current + 1

				// Calculate ETA based on average processing time
				var etaStr string
				if current > 1 && totalElapsed > 0 {
					avgPerChunk := totalElapsed / time.Duration(current-1)
					eta := avgPerChunk * time.Duration(remaining)
					etaStr = fmt.Sprintf(" | ETA: %s", formatDuration(eta))
				}

				fmt.Printf("[%d/%d] Processing chunk %s...%s\n", current, totalChunks, chunkID[:16], etaStr)
				os.Stdout.Sync() // Flush before blocking extraction call

				startTime := time.Now()
				result, err := extractor.ExtractFromChunk(ctx, chunkID, documentID, title, content)
				elapsed := time.Since(startTime)
				totalElapsed += elapsed

				if err != nil {
					errors++
					fmt.Printf("        ✗ Error: %v (%.1fs)\n", err, elapsed.Seconds())
					os.Stdout.Sync()
				} else {
					processed++
					fmt.Printf("        ✓ %d entities, %d relations (%.1fs)\n",
						len(result.Entities), len(result.Relations), elapsed.Seconds())
					os.Stdout.Sync()
				}
			}

			// Show completion summary
			totalTime := time.Since(syncStartTime)
			fmt.Println()
			fmt.Println("Extraction Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Processed:   %d chunks in %s\n", processed, formatDuration(totalTime))
			if errors > 0 {
				fmt.Printf("Errors:      %d chunks failed\n", errors)

				// Show error breakdown
				errorRows, err := db.DB().QueryContext(ctx, `
					SELECT error_message FROM kb_extraction_status WHERE status = 'error'
				`)
				if err == nil {
					defer errorRows.Close()
					errorTypes := make(map[string]int)
					for errorRows.Next() {
						var errMsg string
						errorRows.Scan(&errMsg)
						errType := categorizeError(errMsg)
						errorTypes[errType]++
					}
					for errType, count := range errorTypes {
						fmt.Printf("  - %-18s %d\n", errType+":", count)
					}
				}

				fmt.Println()
				fmt.Println("Note: Failed chunks are still searchable via FTS5")
				fmt.Println("Use 'conduit kb kag-retry' to retry failed extractions")
			}

			// Show stats
			stats, _ := extractor.GetExtractionStats(ctx)
			if stats != nil {
				fmt.Println()
				fmt.Println("Knowledge Graph Statistics:")
				fmt.Printf("  Entities:  %d\n", stats["total_entities"])
				fmt.Printf("  Relations: %d\n", stats["total_relations"])
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Re-extract from all chunks, even previously processed")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider: ollama, openai, anthropic")
	cmd.Flags().BoolVar(&advanced, "advanced", false, "Show advanced options and verbose output")

	return cmd
}

func kbKagStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kag-status",
		Short: "Show detailed KAG extraction status dashboard",
		Long: `Display a comprehensive dashboard of KAG extraction status including:
- Progress bar with completion percentage
- Entity and relation extraction statistics
- Error breakdown by type
- System resource usage (CPU, RAM, storage)
- Ollama model status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			fmt.Println()
			fmt.Println("KAG Extraction Status")
			fmt.Println("═══════════════════════════════════════════════════════════")
			fmt.Println()

			// Get status counts
			statusCounts := make(map[string]int)
			rows, err := db.DB().QueryContext(ctx, `
				SELECT status, COUNT(*) as count
				FROM kb_extraction_status
				GROUP BY status
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var status string
					var count int
					rows.Scan(&status, &count)
					statusCounts[status] = count
				}
			}

			// Count pending (no status)
			var pendingCount int
			db.DB().QueryRowContext(ctx, `
				SELECT COUNT(*) FROM kb_chunks c
				LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
				WHERE s.status IS NULL
			`).Scan(&pendingCount)
			if pendingCount > 0 {
				statusCounts["pending"] = pendingCount
			}

			// Calculate totals
			var totalChunks int
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_chunks").Scan(&totalChunks)

			completedCount := statusCounts["completed"]
			errorCount := statusCounts["error"]
			pendingTotal := pendingCount

			// Progress bar
			fmt.Println("Progress:")
			progressPercent := 0.0
			if totalChunks > 0 {
				progressPercent = float64(completedCount+errorCount) / float64(totalChunks) * 100
			}

			barWidth := 40
			filledWidth := int(float64(barWidth) * progressPercent / 100)
			bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
			fmt.Printf("  %s %d/%d chunks (%.1f%%)\n", bar, completedCount+errorCount, totalChunks, progressPercent)
			fmt.Println()

			// Status breakdown
			fmt.Printf("  Completed:  %d (%.1f%%)\n", completedCount, float64(completedCount)/float64(totalChunks)*100)
			fmt.Printf("  Errors:     %d (%.1f%%)\n", errorCount, float64(errorCount)/float64(totalChunks)*100)
			fmt.Printf("  Pending:    %d (%.1f%%)\n", pendingTotal, float64(pendingTotal)/float64(totalChunks)*100)
			fmt.Println()

			// Entity and relation counts
			var entityCount, relationCount int
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)

			fmt.Println("Entities & Relations:")
			fmt.Println("───────────────────────────────────────────────────────────")
			fmt.Printf("  Entities:   %d extracted\n", entityCount)
			fmt.Printf("  Relations:  %d extracted\n", relationCount)
			if completedCount > 0 {
				fmt.Printf("  Avg/chunk:  %.1f entities, %.1f relations\n",
					float64(entityCount)/float64(completedCount),
					float64(relationCount)/float64(completedCount))
			}
			fmt.Println()

			// Error breakdown (if errors exist)
			if errorCount > 0 {
				fmt.Println("Error Breakdown:")
				fmt.Println("───────────────────────────────────────────────────────────")

				errorRows, err := db.DB().QueryContext(ctx, `
					SELECT error_message FROM kb_extraction_status WHERE status = 'error'
				`)
				if err == nil {
					defer errorRows.Close()
					errorTypes := make(map[string]int)
					for errorRows.Next() {
						var errMsg string
						errorRows.Scan(&errMsg)
						errType := categorizeError(errMsg)
						errorTypes[errType]++
					}
					for errType, count := range errorTypes {
						fmt.Printf("  %-20s %d chunks\n", errType+":", count)
					}
				}
				fmt.Println()
			}

			// System Resources
			fmt.Println("System Resources:")
			fmt.Println("───────────────────────────────────────────────────────────")

			// RAM usage (Go runtime)
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			ramMB := float64(memStats.Alloc) / 1024 / 1024
			fmt.Printf("  RAM:        %.1f MB (Go process)\n", ramMB)

			// Storage usage (data directory)
			conduitDir := filepath.Dir(dbPath)
			var totalSize int64
			filepath.Walk(conduitDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					totalSize += info.Size()
				}
				return nil
			})
			storageMB := float64(totalSize) / 1024 / 1024
			fmt.Printf("  Storage:    %.1f MB (~/.conduit/)\n", storageMB)

			// CPU cores
			fmt.Printf("  CPU Cores:  %d available\n", runtime.NumCPU())
			fmt.Println()

			// Ollama Status
			fmt.Println("Ollama Status:")
			fmt.Println("───────────────────────────────────────────────────────────")

			ollamaBin := findOllamaBinary()
			ollamaOut, err := exec.Command(ollamaBin, "ps").Output()
			if err != nil {
				fmt.Println("  Status:     not running or not accessible")
			} else {
				lines := strings.Split(strings.TrimSpace(string(ollamaOut)), "\n")
				if len(lines) <= 1 {
					fmt.Println("  Status:     running (no models loaded)")
				} else {
					// Parse loaded models
					for i, line := range lines {
						if i == 0 {
							continue // Skip header
						}
						fields := strings.Fields(line)
						if len(fields) >= 4 {
							modelName := fields[0]
							size := fields[2]
							until := strings.Join(fields[4:], " ")
							fmt.Printf("  Model:      %s\n", modelName)
							fmt.Printf("  Size:       %s\n", size)
							fmt.Printf("  Until:      %s\n", until)
						}
					}
				}
			}
			fmt.Println()

			// Suggested commands
			fmt.Println("Commands:")
			fmt.Println("───────────────────────────────────────────────────────────")
			if errorCount > 0 {
				fmt.Println("  conduit kb kag-retry        # Retry failed chunks")
			}
			if pendingTotal > 0 {
				fmt.Println("  conduit kb kag-sync         # Continue extraction")
			}
			fmt.Println("  conduit kb kag-sync --force # Re-extract all chunks")
			fmt.Println()

			return nil
		},
	}
}

func kbKagRetryCmd() *cobra.Command {
	var chunkIDs []string
	var maxRetries int
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "kag-retry",
		Short: "Retry failed KAG extractions",
		Long: `Retry entity extraction for failed chunks.

Without flags, retries all failed chunks. Use --chunk-id to retry specific chunks.

Examples:
  conduit kb kag-retry                    # Retry all failed chunks
  conduit kb kag-retry --chunk-id abc123  # Retry specific chunk
  conduit kb kag-retry --dry-run          # Preview what would be retried
  conduit kb kag-retry --max-retries 3    # Retry with 3 attempts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Build query for failed chunks
			var failedChunks []struct {
				ChunkID    string
				DocumentID string
				Content    string
				Title      string
				Error      string
			}

			if len(chunkIDs) > 0 {
				// Specific chunks
				for _, cid := range chunkIDs {
					var chunk struct {
						ChunkID    string
						DocumentID string
						Content    string
						Title      string
						Error      string
					}
					err := db.DB().QueryRowContext(ctx, `
						SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, ''), COALESCE(s.error_message, '')
						FROM kb_chunks c
						LEFT JOIN kb_documents d ON c.document_id = d.document_id
						LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
						WHERE c.chunk_id = ? AND s.status = 'error'
					`, cid).Scan(&chunk.ChunkID, &chunk.DocumentID, &chunk.Content, &chunk.Title, &chunk.Error)
					if err == nil {
						failedChunks = append(failedChunks, chunk)
					}
				}
			} else {
				// All failed chunks
				rows, err := db.DB().QueryContext(ctx, `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, ''), COALESCE(s.error_message, '')
					FROM kb_chunks c
					JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					WHERE s.status = 'error'
				`)
				if err != nil {
					return fmt.Errorf("query failed chunks: %w", err)
				}
				defer rows.Close()

				for rows.Next() {
					var chunk struct {
						ChunkID    string
						DocumentID string
						Content    string
						Title      string
						Error      string
					}
					if err := rows.Scan(&chunk.ChunkID, &chunk.DocumentID, &chunk.Content, &chunk.Title, &chunk.Error); err != nil {
						continue
					}
					failedChunks = append(failedChunks, chunk)
				}
			}

			if len(failedChunks) == 0 {
				fmt.Println("No failed chunks to retry")
				return nil
			}

			fmt.Printf("Found %d failed chunks\n", len(failedChunks))

			// Show error breakdown
			errorCounts := make(map[string]int)
			for _, chunk := range failedChunks {
				errType := categorizeError(chunk.Error)
				errorCounts[errType]++
			}
			fmt.Println("\nError breakdown:")
			for errType, count := range errorCounts {
				fmt.Printf("  %-20s %d chunks\n", errType+":", count)
			}
			fmt.Println()

			if dryRun {
				fmt.Println("Dry run mode - no changes made")
				fmt.Println("\nChunks that would be retried:")
				for i, chunk := range failedChunks {
					if i >= 10 {
						fmt.Printf("  ... and %d more\n", len(failedChunks)-10)
						break
					}
					fmt.Printf("  %s: %s\n", chunk.ChunkID[:12], truncateString(chunk.Error, 50))
				}
				return nil
			}

			// Create Ollama provider
			ollamaHost := "http://localhost:11434"
			ollamaModel := "mistral:7b-instruct-q4_K_M"

			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{
				Host:  ollamaHost,
				Model: ollamaModel,
			})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}
			defer provider.Close()

			// Check if provider is available
			if !provider.IsAvailable(ctx) {
				return fmt.Errorf("Ollama is not available at %s", ollamaHost)
			}

			// Warm up model
			fmt.Printf("Warming up %s model...", ollamaModel)
			if err := provider.WarmUp(ctx); err != nil {
				fmt.Println(" failed")
				return fmt.Errorf("warmup failed: %w", err)
			}
			fmt.Println(" ready")

			// Create extractor config
			kagCfg := kb.DefaultKAGConfig()
			if maxRetries > 0 {
				kagCfg.Extraction.RetryAttempts = maxRetries
			}

			extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
				Provider: provider,
				DB:       db.DB(),
				Config:   kagCfg,
			})
			if err != nil {
				return fmt.Errorf("create extractor: %w", err)
			}
			defer extractor.Close()

			// Process failed chunks
			fmt.Printf("\nRetrying %d chunks (max %d attempts each):\n", len(failedChunks), kagCfg.Extraction.RetryAttempts)

			successCount := 0
			failCount := 0
			startTime := time.Now()

			for i, chunk := range failedChunks {
				fmt.Printf("[%d/%d] %s...", i+1, len(failedChunks), chunk.ChunkID[:12])

				result, err := extractor.ExtractFromChunkWithRetry(
					ctx,
					chunk.ChunkID,
					chunk.DocumentID,
					chunk.Title,
					chunk.Content,
					maxRetries,
				)

				if err != nil {
					fmt.Printf(" failed: %s\n", truncateString(err.Error(), 40))
					failCount++
				} else {
					fmt.Printf(" ✓ %d entities, %d relations\n", len(result.Entities), len(result.Relations))
					successCount++
				}
			}

			elapsed := time.Since(startTime)
			fmt.Println()
			fmt.Println("Retry Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Successful:  %d chunks\n", successCount)
			fmt.Printf("Failed:      %d chunks\n", failCount)
			fmt.Printf("Duration:    %s\n", elapsed.Round(time.Second))

			if failCount > 0 {
				fmt.Println("\nSome chunks still failed. Check 'conduit kb kag-status' for details.")
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&chunkIDs, "chunk-id", nil, "Specific chunk IDs to retry (can repeat)")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 0, "Maximum retry attempts (default: 2, max: 5)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without executing")

	return cmd
}

// categorizeError classifies extraction errors into categories
func categorizeError(errMsg string) string {
	errLower := strings.ToLower(errMsg)

	if strings.Contains(errLower, "incomplete json") || strings.Contains(errLower, "incomplete") {
		return "Incomplete JSON"
	}
	if strings.Contains(errLower, "invalid escape") || strings.Contains(errLower, "\\_") {
		return "Invalid escape"
	}
	if strings.Contains(errLower, "array") || strings.Contains(errLower, "schema") || strings.Contains(errLower, "type mismatch") {
		return "Schema mismatch"
	}
	if strings.Contains(errLower, "timeout") {
		return "Timeout"
	}
	if strings.Contains(errLower, "connection") || strings.Contains(errLower, "unavailable") {
		return "Connection"
	}
	if strings.Contains(errLower, "parse json") || strings.Contains(errLower, "no json found") {
		return "Parse error"
	}

	return "Other"
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func kbKagDedupeCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "kag-dedupe",
		Short: "Deduplicate entities in the knowledge graph",
		Long: `Merge duplicate entities that have the same normalized name and type.

This command identifies entities that are semantically the same (e.g., "Threat Model"
and "threat model") and merges them, keeping the highest confidence and best description.

Examples:
  conduit kb kag-dedupe           # Deduplicate all entities
  conduit kb kag-dedupe --dry-run # Preview without making changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Find duplicate entities (same normalized name + type but different IDs)
			fmt.Println("Analyzing entities for duplicates...")

			rows, err := db.DB().QueryContext(ctx, `
				SELECT entity_id, name, type, description, confidence, source_document_id
				FROM kb_entities
				ORDER BY name COLLATE NOCASE, type, confidence DESC
			`)
			if err != nil {
				return fmt.Errorf("query entities: %w", err)
			}
			defer rows.Close()

			type entityInfo struct {
				ID          string
				Name        string
				Type        string
				Description string
				Confidence  float64
				SourceDocs  string
			}

			// Group entities by normalized name + type
			groups := make(map[string][]entityInfo)
			var totalEntities int

			for rows.Next() {
				var e entityInfo
				if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocs); err != nil {
					continue
				}
				totalEntities++

				// Create normalized key
				key := strings.ToLower(strings.TrimSpace(e.Name)) + "|" + e.Type
				groups[key] = append(groups[key], e)
			}

			// Find groups with duplicates
			var duplicateGroups int
			var totalDuplicates int
			for _, entities := range groups {
				if len(entities) > 1 {
					duplicateGroups++
					totalDuplicates += len(entities) - 1 // Count extras as duplicates
				}
			}

			fmt.Printf("\nFound %d entities in %d groups\n", totalEntities, len(groups))
			fmt.Printf("Duplicate groups: %d (containing %d extra entities)\n", duplicateGroups, totalDuplicates)

			if duplicateGroups == 0 {
				fmt.Println("\nNo duplicates found. Knowledge graph is clean.")
				return nil
			}

			if dryRun {
				fmt.Println("\n--dry-run: Showing what would be merged:")
				shown := 0
				for key, entities := range groups {
					if len(entities) > 1 && shown < 10 {
						parts := strings.SplitN(key, "|", 2)
						fmt.Printf("  \"%s\" (%s): %d entities → 1\n", parts[0], parts[1], len(entities))
						shown++
					}
				}
				if duplicateGroups > 10 {
					fmt.Printf("  ... and %d more groups\n", duplicateGroups-10)
				}
				fmt.Println("\nRun without --dry-run to merge duplicates.")
				return nil
			}

			// Perform deduplication
			fmt.Println("\nMerging duplicates...")

			merged := 0
			deleted := 0

			for _, entities := range groups {
				if len(entities) <= 1 {
					continue
				}

				// First entity (highest confidence) becomes the canonical one
				canonical := entities[0]
				canonicalID := kb.GenerateCanonicalEntityID(canonical.Name, kb.EntityType(canonical.Type))

				// Best description is the longest
				bestDesc := canonical.Description
				for _, e := range entities[1:] {
					if len(e.Description) > len(bestDesc) {
						bestDesc = e.Description
					}
				}

				// Combine source documents
				sourceDocs := canonical.SourceDocs
				for _, e := range entities[1:] {
					if e.SourceDocs != "" && !strings.Contains(sourceDocs, e.SourceDocs) {
						if sourceDocs != "" {
							sourceDocs += "," + e.SourceDocs
						} else {
							sourceDocs = e.SourceDocs
						}
					}
				}

				// Update/insert canonical entity
				now := time.Now().Format(time.RFC3339)
				_, err := db.DB().ExecContext(ctx, `
					INSERT OR REPLACE INTO kb_entities
					(entity_id, name, type, description, source_chunk_id, source_document_id,
					 confidence, metadata, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
				`, canonicalID, canonical.Name, canonical.Type, bestDesc,
					"", sourceDocs, canonical.Confidence, now, now)
				if err != nil {
					return fmt.Errorf("upsert canonical entity: %w", err)
				}

				// Delete old entities
				for _, e := range entities {
					if e.ID != canonicalID {
						_, err := db.DB().ExecContext(ctx, `DELETE FROM kb_entities WHERE entity_id = ?`, e.ID)
						if err == nil {
							deleted++
						}
					}
				}
				merged++
			}

			fmt.Println("\nDeduplication Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Groups merged:    %d\n", merged)
			fmt.Printf("Entities deleted: %d\n", deleted)
			fmt.Printf("Entities after:   %d\n", totalEntities-deleted)

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without making changes")

	return cmd
}

func kbKagVectorizeCmd() *cobra.Command {
	var batchSize int
	var ollamaHost string

	cmd := &cobra.Command{
		Use:   "kag-vectorize",
		Short: "Generate vector embeddings for KAG entities",
		Long: `Generate and store vector embeddings for all entities in the knowledge graph.

This enables semantic search over entities using vector similarity.
Entity embeddings are stored in the knowledge base file, in a table separate
from the chunk vectors.

Requirements:
  - Ollama running with nomic-embed-text model

Examples:
  conduit kb kag-vectorize
  conduit kb kag-vectorize --batch-size 50
  conduit kb kag-vectorize --ollama-host http://192.168.1.60:11434`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Create the embedding provider.
			//
			// WP-3.4 (#71) repointed this at internal/embed. The old
			// kb.NewEmbeddingService built its Ollama client on
			// http.DefaultClient, which has no timeout, so a hung daemon
			// blocked this command forever. Every internal/embed call is
			// bounded by an http.Client timeout as well as the context.
			fmt.Println("Connecting to Ollama...")
			provider, err := embed.NewOllamaProvider(embed.OllamaConfig{
				Host:       ollamaHost,
				Model:      kb.DefaultEmbeddingModel,
				Dimensions: kb.DefaultEmbeddingDimension,
				BatchSize:  batchSize,
			})
			if err != nil {
				return fmt.Errorf("create embedding provider: %w", err)
			}
			defer provider.Close()
			embeddingSvc := kb.NewProviderEmbedder(provider)

			// Verify the model is reachable and usable before doing any work.
			if err := embeddingSvc.HealthCheck(ctx); err != nil {
				return fmt.Errorf("embedding provider unavailable: %w", err)
			}

			// Open the entity vector index in the knowledge base file
			vectorIndex, err := kb.NewSQLiteVectorIndex(db.DB(), kb.VectorIndexConfig{
				Dimension: embeddingSvc.Dimension(),
			})
			if err != nil {
				return fmt.Errorf("open vector index: %w", err)
			}
			defer vectorIndex.Close()

			// Query all entities from database
			fmt.Println("Loading entities from database...")
			rows, err := db.DB().QueryContext(ctx, `
				SELECT entity_id, name, type, description, confidence, source_document_id
				FROM kb_entities
				ORDER BY name
			`)
			if err != nil {
				return fmt.Errorf("query entities: %w", err)
			}
			defer rows.Close()

			type entityInfo struct {
				ID          string
				Name        string
				Type        string
				Description string
				Confidence  float64
				SourceDocs  string
			}

			var entities []entityInfo
			for rows.Next() {
				var e entityInfo
				if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocs); err != nil {
					continue
				}
				entities = append(entities, e)
			}

			if len(entities) == 0 {
				fmt.Println("No entities found to vectorize.")
				return nil
			}

			fmt.Printf("Found %d entities to vectorize\n", len(entities))

			// Process in batches
			var vectorized, failed int
			for i := 0; i < len(entities); i += batchSize {
				end := i + batchSize
				if end > len(entities) {
					end = len(entities)
				}
				batch := entities[i:end]

				// Generate embeddings for this batch
				texts := make([]string, len(batch))
				for j, e := range batch {
					// Combine name and description for richer embeddings
					texts[j] = e.Name
					if e.Description != "" {
						texts[j] += ": " + e.Description
					}
				}

				embeddings, err := embeddingSvc.EmbedBatch(ctx, texts)
				if err != nil {
					fmt.Printf("  Batch %d-%d: embedding failed: %v\n", i+1, end, err)
					failed += len(batch)
					continue
				}

				// Convert to entity vector points
				points := make([]kb.EntityVectorPoint, len(batch))
				for j, e := range batch {
					points[j] = kb.EntityVectorPoint{
						ID:          e.ID,
						Vector:      embeddings[j],
						Name:        e.Name,
						Type:        e.Type,
						Description: e.Description,
						SourceIDs:   e.SourceDocs,
						Confidence:  e.Confidence,
					}
				}

				// Write to the entity vector index
				if err := vectorIndex.UpsertEntityBatch(ctx, points); err != nil {
					fmt.Printf("  Batch %d-%d: upsert failed: %v\n", i+1, end, err)
					failed += len(batch)
					continue
				}

				vectorized += len(batch)
				fmt.Printf("  Vectorized %d/%d entities\r", vectorized, len(entities))
			}

			fmt.Println() // New line after progress
			fmt.Println("\nVectorization Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Total entities:   %d\n", len(entities))
			fmt.Printf("Vectorized:       %d\n", vectorized)
			if failed > 0 {
				fmt.Printf("Failed:           %d\n", failed)
			}

			// Show collection stats
			stats, err := vectorIndex.GetEntityStats(ctx)
			if err == nil {
				fmt.Printf("\nEntity Collection: %s\n", stats.CollectionName)
				fmt.Printf("  Vectors: %d\n", stats.VectorCount)
				fmt.Printf("  Status:  %s\n", stats.Status)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&batchSize, "batch-size", 20, "Number of entities to process at a time")
	cmd.Flags().StringVar(&ollamaHost, "ollama-host", "http://localhost:11434", "Ollama API endpoint")

	return cmd
}

func kbKagQueryCmd() *cobra.Command {
	var maxHops int
	var format string
	var hybrid bool
	var ollamaHost string

	cmd := &cobra.Command{
		Use:   "kag-query <query>",
		Short: "Query the knowledge graph",
		Long: `Query the knowledge graph for entities and relationships.

The --hybrid flag enables hybrid search (lexical + semantic) for improved recall.
Requires Ollama (nomic-embed-text) and entities vectorized via kag-vectorize.

Examples:
  conduit kb kag-query "threat models"
  conduit kb kag-query "authentication" --max-hops 3
  conduit kb kag-query "API security" --format json
  conduit kb kag-query "threat model summary" --hybrid  # Uses semantic search`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Open database
			dbPath := kbPath()
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Create KAGSearcher configuration. The graph store is enabled only
			// when the config says so; when it is off, entity lookup still works
			// and relations come from the legacy kb_relations table.
			graphEnabled := false
			if cfg, err := config.Load(); err == nil {
				graphEnabled = cfg.KB.KAG.Enabled
			}
			kagCfg := kb.KAGSearcherConfig{
				DB:         db.DB(),
				GraphStore: kb.NewGraphStore(db.DB(), graphEnabled),
			}

			// Set up hybrid search if requested
			if hybrid {
				// Create the embedding provider (#71: bounded by a timeout).
				provider, err := embed.NewOllamaProvider(embed.OllamaConfig{
					Host:       ollamaHost,
					Model:      kb.DefaultEmbeddingModel,
					Dimensions: kb.DefaultEmbeddingDimension,
				})
				if err != nil {
					fmt.Printf("Warning: Could not connect to Ollama, falling back to lexical search: %v\n", err)
				} else {
					embeddingSvc := kb.NewProviderEmbedder(provider)
					// Open the entity vector index in the knowledge base file
					vectorIndex, err := kb.NewSQLiteVectorIndex(db.DB(), kb.VectorIndexConfig{
						Dimension: embeddingSvc.Dimension(),
					})
					if err != nil {
						fmt.Printf("Warning: Could not open the vector index, falling back to lexical search: %v\n", err)
					} else {
						kagCfg.EntityVectors = vectorIndex
						kagCfg.EmbeddingService = embeddingSvc
						defer vectorIndex.Close()
					}
				}
			}

			// Use KAGSearcher for improved tokenized search
			kagSearcher := kb.NewKAGSearcherWithConfig(kagCfg)
			result, err := kagSearcher.Search(ctx, &kb.KAGSearchRequest{
				Query:            query,
				MaxHops:          maxHops,
				Limit:            20,
				IncludeRelations: maxHops > 0,
			})
			if err != nil {
				return fmt.Errorf("search entities: %w", err)
			}

			if format == "json" {
				output := map[string]interface{}{
					"query":    query,
					"entities": result.Entities,
				}
				if len(result.Relations) > 0 {
					output["relations"] = result.Relations
				}
				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Printf("Query: %s\n", query)
				fmt.Println("═══════════════════════════════════════")
				fmt.Println()

				if len(result.Entities) == 0 {
					fmt.Println("No matching entities found.")
					return nil
				}

				for _, e := range result.Entities {
					fmt.Printf("• %s (%s)\n", e.Name, e.Type)
					if e.Description != "" {
						fmt.Printf("  %s\n", truncate(e.Description, 80))
					}
					fmt.Printf("  Confidence: %.0f%%\n", e.Confidence*100)
					fmt.Println()
				}

				// Show relations if any
				if len(result.Relations) > 0 {
					fmt.Println("Relationships:")
					for _, r := range result.Relations {
						fmt.Printf("  %s → %s → %s\n", r.SubjectName, r.Predicate, r.ObjectName)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&maxHops, "max-hops", 2, "Maximum relationship hops to traverse")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&hybrid, "hybrid", false, "Enable hybrid search (lexical + semantic)")
	cmd.Flags().StringVar(&ollamaHost, "ollama-host", "http://localhost:11434", "Ollama API endpoint")

	return cmd
}
