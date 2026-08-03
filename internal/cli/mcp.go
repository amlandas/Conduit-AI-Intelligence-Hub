package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/kbservice"
	"github.com/simpleflo/conduit/internal/mcpserver"
	setuppkg "github.com/simpleflo/conduit/internal/setup"
)

// mcpCmd is the parent command for MCP server operations
func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server operations",
		Long:  "Run MCP servers for AI client integration",
	}

	cmd.AddCommand(mcpKBCmd())
	cmd.AddCommand(mcpStatusCmd())
	cmd.AddCommand(mcpLogsCmd())
	cmd.AddCommand(mcpConfigureCmd())

	return cmd
}

// mcpConfigureCmd auto-configures the MCP KB server in AI clients
func mcpConfigureCmd() *cobra.Command {
	var clientID string
	var forceOverwrite bool

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Auto-configure MCP KB server in AI clients",
		Long: `Auto-configure the Conduit MCP KB server in AI clients.

This adds the MCP server configuration to the client's config file,
enabling AI-powered document search from your Knowledge Base.

Supported clients:
  - claude-code: Claude Code CLI (~/.claude.json)
  - cursor: Cursor IDE (.cursor/settings/extensions.json)
  - vscode: VS Code (.vscode/settings.json)

Examples:
  conduit mcp configure                    # Configure for Claude Code (default)
  conduit mcp configure --client cursor    # Configure for Cursor IDE
  conduit mcp configure --check            # Check if already configured`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureMCPClient(clientID, forceOverwrite)
		},
	}

	cmd.Flags().StringVarP(&clientID, "client", "c", "claude-code", "Client to configure (claude-code, cursor, vscode)")
	cmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false, "Overwrite existing configuration")

	return cmd
}

// configureMCPClient registers Conduit's MCP server in an AI client and reports
// the outcome. The work itself lives in internal/setup; this is presentation.
func configureMCPClient(clientID string, forceOverwrite bool) error {
	res, err := setuppkg.ConfigureMCPClient(clientID, forceOverwrite)
	if err != nil {
		return err
	}

	if res.AlreadyConfigured {
		fmt.Println("✓ MCP KB server already configured")
		fmt.Printf("  Client: %s\n", res.ClientID)
		fmt.Printf("  Config: %s\n", res.ConfigPath)
		return nil
	}

	fmt.Println("✓ MCP KB server configured")
	fmt.Printf("  Client: %s\n", res.ClientID)
	fmt.Printf("  Config: %s\n", res.ConfigPath)
	fmt.Println()
	fmt.Printf("Restart %s for the configuration to take effect.\n", res.ClientID)

	return nil
}

// newKBMCPServer builds the KB MCP server and the knowledge base it serves.
//
// It is separate from the command so a test can drive the very same wiring over
// an in-memory transport. The caller owns the returned service and must Close
// it.
//
// Opening through kbservice is what makes `conduit mcp kb` and
// `conduit kb search` retrieve identically: both get the same searchers and the
// same configured embedding provider.
func newKBMCPServer(cfg *config.Config) (*mcpserver.Server, *kbservice.Service, error) {
	svc, err := kbservice.Open(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	// Backed by the official MCP Go SDK (internal/mcpserver); it speaks the
	// current spec revision and negotiates down for older clients.
	server := mcpserver.NewWithOptions(svc.DB(), svc.Hybrid(), mcpserver.Options{
		GraphEnabled:    cfg.KB.KAG.Enabled,
		GraphMaxHops:    cfg.KB.KAG.Graph.MaxHops,
		QueryLogDir:     cfg.DataDir,
		QueryLogEnabled: cfg.Telemetry.LocalQueryLog,
	})

	return server, svc, nil
}

// mcpKBCmd runs the KB MCP server
func mcpKBCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kb",
		Short: "Run Knowledge Base MCP server",
		Long: `Run the Knowledge Base MCP server over stdio.

This server provides search and document retrieval tools for AI clients
to access your private knowledge base.

Example MCP client configuration:
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// stdio owns stdout: nothing below may print there, or the protocol
			// frame stream is corrupted. Diagnostics go to stderr.
			mcpCfg, cfgErr := loadConfig()
			if cfgErr != nil {
				// A broken config file must not stop the server from serving
				// retrieval; fall back to defaults (graph off, query log on).
				mcpCfg = config.DefaultConfig()
			}

			server, svc, err := newKBMCPServer(mcpCfg)
			if err != nil {
				return err
			}
			defer svc.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle shutdown signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return server.Run(ctx)
		},
	}
}

// mcpStatusCmd shows MCP server status and capabilities
func mcpStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP server status and capabilities",
		Long: `Display the status and capabilities of the MCP KB server.

Shows:
- MCP configuration status in AI clients (Claude Code, Cursor, VS Code)
- Search capabilities (FTS5, semantic search availability)
- Vector index and Ollama connectivity status
- Knowledge base sources and statistics

Use --json for machine-readable output (used by GUI).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check MCP configuration in each supported client
			type ClientConfig struct {
				Configured bool   `json:"configured"`
				ConfigPath string `json:"configPath"`
				ServerName string `json:"serverName,omitempty"`
			}

			// The client list comes from internal/setup so that 'mcp status'
			// and 'mcp configure' can never look at different files.
			clients := map[string]ClientConfig{}
			for name, client := range setuppkg.MCPClients() {
				configured, serverName := checkMCPClientConfigured(client.ConfigPath)
				clients[name] = ClientConfig{
					Configured: configured,
					ConfigPath: client.ConfigPath,
					ServerName: serverName,
				}
			}

			// JSON output mode for GUI consumption
			if jsonOutput {
				result := make(map[string]interface{})
				for name, cfg := range clients {
					result[name] = map[string]interface{}{
						"configured": cfg.Configured,
						"configPath": cfg.ConfigPath,
						"serverName": cfg.ServerName,
					}
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Human-readable output
			fmt.Println("MCP KB Server Status")
			fmt.Println("════════════════════════════════════════════════════════")

			// Client configuration status
			fmt.Println("\n🔧 Client Configuration:")
			fmt.Println("────────────────────────────────────────────────────────")
			for name, cfg := range clients {
				status := "✗ Not configured"
				if cfg.Configured {
					status = "✓ Configured"
				}
				fmt.Printf("  %-12s %s\n", name+":", status)
				fmt.Printf("    └─ %s\n", cfg.ConfigPath)
			}

			// Open the knowledge base for the capabilities check
			svc, err := openKB()
			if err != nil {
				// Database not available - skip capabilities section
				fmt.Println("\n⚠️  Knowledge base not initialized. Run 'conduit kb add <path>' first.")
				return nil
			}
			defer svc.Close()

			// Detect capabilities against the configured embedding provider
			caps := kb.DetectCapabilities(ctx, svc.DB(), svc.Embedder())

			// Capabilities
			fmt.Println("\n📋 Search Capabilities:")
			fmt.Println("────────────────────────────────────────────────────────")
			if caps.FTS5Available {
				fmt.Println("  ✓ FTS5 (keyword search): available")
			} else {
				fmt.Println("  ✗ FTS5 (keyword search): not available")
			}

			if caps.SemanticAvailable {
				fmt.Printf("  ✓ Semantic search: available (model: %s)\n", caps.EmbeddingModel)
			} else {
				fmt.Println("  ✗ Semantic search: not available")
			}

			fmt.Printf("  → Recommended mode: %s\n", caps.SearchMode())

			// Service status
			fmt.Println("\n🔌 Service Connectivity:")
			fmt.Println("────────────────────────────────────────────────────────")
			fmt.Printf("  Vector index (in knowledge base): %s\n", caps.VectorStatus)
			fmt.Printf("  Embeddings (%s): %s\n", svc.EmbedInfo().Provider, caps.EmbedStatus)

			// Knowledge base stats
			fmt.Println("\n📚 Knowledge Base:")
			fmt.Println("────────────────────────────────────────────────────────")
			sources, err := svc.Sources().List(ctx)
			if err != nil {
				fmt.Printf("  Error listing sources: %v\n", err)
			} else if len(sources) == 0 {
				fmt.Println("  No sources indexed. Use 'conduit kb add <path>' to add sources.")
			} else {
				fmt.Printf("  Sources: %d\n", len(sources))
				for _, src := range sources {
					fmt.Printf("    • %s (%d docs, %d chunks)\n", src.Name, src.DocCount, src.ChunkCount)
				}
			}

			// Configuration help if not configured
			anyConfigured := false
			for _, cfg := range clients {
				if cfg.Configured {
					anyConfigured = true
					break
				}
			}

			if !anyConfigured {
				fmt.Println("\n⚙️  To configure MCP in an AI client:")
				fmt.Println("────────────────────────────────────────────────────────")
				fmt.Println("  Run: conduit mcp configure --client <client-name>")
				fmt.Println()
				fmt.Println("  Or add manually to client config:")
				fmt.Println(`  {
    "mcpServers": {
      "conduit-kb": {
        "command": "conduit",
        "args": ["mcp", "kb"]
      }
    }
  }`)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

// mcpLogsCmd shows MCP-related logs
func mcpLogsCmd() *cobra.Command {
	var tail int
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show MCP server logs",
		Long: `Display logs from MCP server operations.

Note: The MCP KB server runs synchronously when invoked by an AI client.
This command shows daemon logs related to MCP operations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// MCP server logs are typically in the daemon log or stderr
			homeDir, _ := os.UserHomeDir()
			logPath := filepath.Join(homeDir, ".conduit", "logs", "mcp.log")

			// Check if log file exists
			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				fmt.Println("MCP Log Status")
				fmt.Println("════════════════════════════════════════════════════════")
				fmt.Println()
				fmt.Println("ℹ️  No MCP logs found.")
				fmt.Println()
				fmt.Println("The MCP KB server runs synchronously when invoked by AI clients.")
				fmt.Println("Logs are written to stderr and captured by the AI client.")
				fmt.Println()
				fmt.Println("To debug MCP issues:")
				fmt.Println("  1. Check your AI client's MCP server logs")
				fmt.Println("  2. Run 'conduit mcp kb' manually and send JSON-RPC requests")
				fmt.Println("  3. Use 'conduit mcp status' to verify capabilities")
				return nil
			}

			// Read and display log file
			file, err := os.Open(logPath)
			if err != nil {
				return fmt.Errorf("open log file: %w", err)
			}
			defer file.Close()

			if follow {
				// Follow mode - tail -f style
				fmt.Printf("Following MCP logs (Ctrl+C to stop)...\n\n")

				// Seek to end minus tail lines
				scanner := bufio.NewScanner(file)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
					if len(lines) > tail {
						lines = lines[1:]
					}
				}

				for _, line := range lines {
					fmt.Println(line)
				}

				// Continue watching for new content
				for {
					select {
					case <-cmd.Context().Done():
						return nil
					default:
						line, err := bufio.NewReader(file).ReadString('\n')
						if err != nil {
							time.Sleep(100 * time.Millisecond)
							continue
						}
						fmt.Print(line)
					}
				}
			} else {
				// Print last N lines
				scanner := bufio.NewScanner(file)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
					if tail > 0 && len(lines) > tail {
						lines = lines[1:]
					}
				}

				if len(lines) == 0 {
					fmt.Println("No MCP log entries found.")
				} else {
					for _, line := range lines {
						fmt.Println(line)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 50, "Number of lines to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")

	return cmd
}

// checkMCPClientConfigured reports whether a client config already registers
// Conduit's MCP server. It delegates so that 'mcp configure' writing an entry
// and 'mcp status' looking for one can never disagree about its shape.
func checkMCPClientConfigured(configPath string) (bool, string) {
	return setuppkg.IsMCPClientConfigured(configPath)
}

// readLastNLines reads the last N lines from a file
func readLastNLines(f *os.File, n int) []string {
	scanner := bufio.NewScanner(f)
	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}

	return lines
}
