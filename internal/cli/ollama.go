package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/kb"
)

// ollamaCmd returns the ollama parent command
func ollamaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Manage Ollama models for AI features",
		Long: `Manage Ollama models used for semantic search and entity extraction.

Conduit uses Ollama for:
  • nomic-embed-text - Embedding model for semantic search
  • mistral:7b-instruct-q4_K_M - Entity extraction for KAG

Examples:
  conduit ollama status     # Show loaded models and Ollama status
  conduit ollama models     # List available models
  conduit ollama pull <model>  # Download a model
  conduit ollama warmup     # Preload required models into memory`,
	}

	cmd.AddCommand(ollamaStatusCmd())
	cmd.AddCommand(ollamaModelsCmd())
	cmd.AddCommand(ollamaPullCmd())
	cmd.AddCommand(ollamaWarmupCmd())

	return cmd
}

// findOllamaBinary searches for the ollama CLI in common locations
func findOllamaBinary() string {
	// Check PATH first
	if path, err := exec.LookPath("ollama"); err == nil {
		return path
	}

	// Check common installation locations
	locations := []string{
		"/opt/homebrew/bin/ollama", // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/ollama",    // macOS Homebrew (Intel) / Linux
		"/usr/bin/ollama",          // Linux system install
		"/snap/bin/ollama",         // Linux snap install
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return "ollama" // Fall back to PATH lookup
}

// ollamaStatusCmd shows Ollama status and loaded models
func ollamaStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Ollama status and loaded models",
		Long: `Check if Ollama is running and show currently loaded models.

Loaded models are kept in memory for fast inference. Models that
aren't loaded will have a cold-start delay on first use (1-2 minutes).

Use 'conduit ollama warmup' to preload models for faster response times.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with:")
				fmt.Println("  ollama serve")
				fmt.Println()
				fmt.Println("Or install Ollama from: https://ollama.ai")
				return nil
			}

			fmt.Println("✓ Ollama is running")
			fmt.Println()

			// Get loaded models using ollama ps
			ollamaBin := findOllamaBinary()
			out, err := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			if err != nil {
				fmt.Println("Loaded models: (unable to query)")
				return nil
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) <= 1 {
				fmt.Println("Loaded models: none")
				fmt.Println()
				fmt.Println("No models are currently loaded in memory.")
				fmt.Println("Models will be loaded on first use (1-2 minute delay).")
				fmt.Println()
				fmt.Println("To preload models for faster response times:")
				fmt.Println("  conduit ollama warmup")
			} else {
				fmt.Println("Loaded models:")
				for _, line := range lines {
					fmt.Println("  " + line)
				}
			}

			// Show required models status
			fmt.Println()
			fmt.Println("Required models for Conduit:")
			fmt.Println("  • nomic-embed-text       - Semantic search embeddings")
			fmt.Println("  • mistral:7b-instruct-q4_K_M - Entity extraction (KAG)")

			return nil
		},
	}
}

// ollamaModelsCmd lists available Ollama models
func ollamaModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List available Ollama models",
		Long: `List all Ollama models installed on the system.

Shows which models are available locally. Missing models will be
automatically downloaded on first use, or you can pull them manually:

  ollama pull nomic-embed-text
  ollama pull mistral:7b-instruct-q4_K_M`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with: ollama serve")
				return nil
			}

			// Get available models using ollama list
			ollamaBin := findOllamaBinary()
			out, err := exec.CommandContext(ctx, ollamaBin, "list").Output()
			if err != nil {
				return fmt.Errorf("list models: %w", err)
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) <= 1 {
				fmt.Println("No models installed.")
				fmt.Println()
				fmt.Println("Pull required models with:")
				fmt.Println("  ollama pull nomic-embed-text")
				fmt.Println("  ollama pull mistral:7b-instruct-q4_K_M")
				return nil
			}

			fmt.Println("Available models:")
			for _, line := range lines {
				fmt.Println("  " + line)
			}

			// Check for required models
			modelsStr := string(out)
			fmt.Println()
			fmt.Println("Required models status:")

			if strings.Contains(modelsStr, "nomic-embed-text") {
				fmt.Println("  ✓ nomic-embed-text (installed)")
			} else {
				fmt.Println("  ✗ nomic-embed-text (not installed)")
				fmt.Println("    Pull with: ollama pull nomic-embed-text")
			}

			if strings.Contains(modelsStr, "mistral") {
				fmt.Println("  ✓ mistral (installed)")
			} else {
				fmt.Println("  ✗ mistral (not installed)")
				fmt.Println("    Pull with: ollama pull mistral:7b-instruct-q4_K_M")
			}

			return nil
		},
	}
}

// ensureOllamaRunning starts Ollama server if not running and waits for it to be ready
func ensureOllamaRunning(ctx context.Context) error {
	// Check if already running
	provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	if provider.IsAvailable(ctx) {
		return nil // Already running
	}

	// Try to start Ollama
	ollamaBin := findOllamaBinary()
	if ollamaBin == "" {
		return fmt.Errorf("Ollama binary not found. Install with: brew install ollama")
	}

	fmt.Println("Starting Ollama server...")

	// Start ollama serve in background
	serveCmd := exec.Command(ollamaBin, "serve")
	serveCmd.Stdout = nil
	serveCmd.Stderr = nil
	if err := serveCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Ollama: %w", err)
	}

	// Wait for Ollama to become available (up to 30 seconds)
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if provider.IsAvailable(ctx) {
			fmt.Println("✓ Ollama server started")
			return nil
		}
	}

	return fmt.Errorf("Ollama did not start in time. Try running: %s serve", ollamaBin)
}

// ollamaPullCmd pulls an Ollama model with progress streaming
func ollamaPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <model>",
		Short: "Pull an Ollama model",
		Long: `Pull (download) an Ollama model from the registry.

Progress is streamed to stdout, making it suitable for GUI integration.
If Ollama is not running, it will be started automatically.

Examples:
  conduit ollama pull nomic-embed-text
  conduit ollama pull mistral:7b-instruct-q4_K_M`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			model := args[0]

			// Ensure Ollama is running (start it if needed)
			if err := ensureOllamaRunning(ctx); err != nil {
				return err
			}

			fmt.Printf("Pulling model: %s\n", model)

			// Run ollama pull with output streaming
			ollamaBin := findOllamaBinary()
			pullCmd := exec.CommandContext(ctx, ollamaBin, "pull", model)

			// Capture both stdout and stderr for progress
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr

			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}

			fmt.Printf("\n✓ Model %s pulled successfully\n", model)
			return nil
		},
	}
}

// ollamaWarmupCmd preloads required models into memory
func ollamaWarmupCmd() *cobra.Command {
	var models []string

	cmd := &cobra.Command{
		Use:   "warmup",
		Short: "Preload models into memory for faster inference",
		Long: `Preload Ollama models into memory to eliminate cold-start delays.

By default, warms up both required models:
  • nomic-embed-text - For semantic search
  • mistral:7b-instruct-q4_K_M - For entity extraction

Models stay loaded based on Ollama's keep_alive setting (default: 5 minutes).

Examples:
  conduit ollama warmup                           # Warm up all required models
  conduit ollama warmup --models nomic-embed-text # Warm up specific model`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with: ollama serve")
				return nil
			}

			// Default models if none specified
			if len(models) == 0 {
				models = []string{"nomic-embed-text", "mistral:7b-instruct-q4_K_M"}
			}

			fmt.Println("Warming up Ollama models...")
			fmt.Println("This may take 1-2 minutes per model on first load.")
			fmt.Println()

			ollamaBin := findOllamaBinary()

			for _, model := range models {
				fmt.Printf("Loading %s... ", model)
				os.Stdout.Sync()

				startTime := time.Now()

				// Send a minimal request to load the model
				// Using ollama run with a simple prompt
				runCmd := exec.CommandContext(ctx, ollamaBin, "run", model, "hello")
				runCmd.Stdin = strings.NewReader("")

				if err := runCmd.Run(); err != nil {
					fmt.Printf("✗ failed\n")
					fmt.Printf("  Error: %v\n", err)
					fmt.Printf("  Try pulling the model: ollama pull %s\n", model)
					continue
				}

				elapsed := time.Since(startTime)
				fmt.Printf("✓ loaded (%s)\n", formatDuration(elapsed))
			}

			fmt.Println()
			fmt.Println("Models are now loaded and ready for fast inference.")
			fmt.Println("They will stay loaded based on Ollama's keep_alive setting.")

			// Show current status
			fmt.Println()
			out, err := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			if err == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				if len(lines) > 1 {
					fmt.Println("Currently loaded:")
					for _, line := range lines {
						fmt.Println("  " + line)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", nil, "Specific models to warm up (default: all required)")

	return cmd
}

// getOllamaModels returns a list of installed Ollama models
func getOllamaModels() ([]string, error) {
	cmd := exec.Command("curl", "-s", "http://localhost:11434/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	var models []string
	if modelsData, ok := result["models"].([]interface{}); ok {
		for _, m := range modelsData {
			if model, ok := m.(map[string]interface{}); ok {
				if name, ok := model["name"].(string); ok {
					models = append(models, name)
				}
			}
		}
	}
	return models, nil
}

// checkOllamaRunning checks if Ollama is running
func checkOllamaRunning() bool {
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:11434/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "200"
}

// checkCommand checks if a command is available
func checkCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
