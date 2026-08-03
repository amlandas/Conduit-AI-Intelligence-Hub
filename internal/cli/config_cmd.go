package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/simpleflo/conduit/internal/config"
)

// configCmd shows configuration
func configCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show Conduit configuration",
		Long: `Display the current Conduit configuration.

Shows configuration loaded from:
  - ~/.conduit/conduit.yaml
  - /etc/conduit/conduit.yaml
  - Environment variables (CONDUIT_*)

Examples:
  conduit config
  conduit config --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			fmt.Println("Conduit Configuration")
			fmt.Println("═══════════════════════════════════════════════════════")

			fmt.Println("\n📁 Paths:")
			fmt.Printf("  Data Directory:  %s\n", cfg.DataDir)
			fmt.Printf("  Database Path:   %s\n", cfg.DatabasePath())
			fmt.Printf("  Log Path:        %s\n", cfg.LogPath())
			fmt.Printf("  Backups Dir:     %s\n", cfg.BackupsDir())

			fmt.Println("\n📝 Logging:")
			fmt.Printf("  Log Level:       %s\n", cfg.LogLevel)
			fmt.Printf("  Log Format:      %s\n", cfg.LogFormat)

			fmt.Println("\n🧠 Embeddings:")
			fmt.Printf("  Provider:        %s\n", cfg.Embed.Provider)
			if cfg.Embed.Provider == config.EmbedProviderNone {
				fmt.Println("  Mode:            lexical-only (FTS5); no model is loaded")
			} else {
				model := cfg.Embed.Model
				if model == "" {
					model = "(registry default)"
				}
				fmt.Printf("  Model:           %s\n", model)
				fmt.Printf("  Timeout:         %d seconds\n", cfg.Embed.TimeoutSeconds)
			}

			fmt.Println("\n🕸️ Knowledge Graph:")
			fmt.Printf("  Enabled:         %v\n", cfg.KB.KAG.Enabled)
			if cfg.KB.KAG.Enabled {
				fmt.Printf("  Provider:        %s\n", cfg.KB.KAG.Provider)
				fmt.Printf("  Max Hops:        %d\n", cfg.KB.KAG.Graph.MaxHops)
			}

			if showAll {
				fmt.Println("\n📚 Knowledge Base:")
				fmt.Printf("  Workers:         %d\n", cfg.KB.Workers)
				fmt.Printf("  Max File Size:   %s\n", formatBytes(cfg.KB.MaxFileSize))
				fmt.Printf("  Chunk Size:      %d\n", cfg.KB.ChunkSize)
				fmt.Printf("  Chunk Overlap:   %d\n", cfg.KB.ChunkOverlap)

				fmt.Println("\n🔍 Retrieval:")
				fmt.Printf("  Default Limit:   %d\n", cfg.KB.RAG.DefaultLimit)
				fmt.Printf("  Min Score:       %.2f\n", cfg.KB.RAG.MinScore)
				fmt.Printf("  Recall Mode:     %s\n", cfg.KB.RAG.RecallMode)

				fmt.Println("\n🔒 Policy:")
				fmt.Println("  Forbidden Paths:")
				for _, p := range cfg.Policy.ForbiddenPaths {
					fmt.Printf("    - %s\n", p)
				}
				fmt.Println("  Warn Paths:")
				for _, p := range cfg.Policy.WarnPaths {
					fmt.Printf("    - %s\n", p)
				}

				fmt.Println("\n📡 Telemetry (local only, never uploaded):")
				fmt.Printf("  Query Shape Log: %v\n", cfg.Telemetry.LocalQueryLog)
			}

			// Show config file location
			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("\n📄 Config File: %s\n", configPath)
			} else {
				fmt.Println("\n📄 Config File: (using defaults, no config file found)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all configuration options")

	// Add subcommands
	cmd.AddCommand(configGetCmd())
	cmd.AddCommand(configSetCmd())
	cmd.AddCommand(configUnsetCmd())

	return cmd
}

// configGetCmd retrieves a configuration value
func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a specific configuration value.

Keys use dot notation to access nested values.

Examples:
  conduit config get embed.provider
  conduit config get kb.rag.default_limit
  conduit config get kb.kag.enabled`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")

			v := viper.New()
			v.SetConfigFile(configPath)
			v.SetConfigType("yaml")

			if err := v.ReadInConfig(); err != nil {
				// If config file doesn't exist, return empty
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("read config: %w", err)
			}

			value := v.Get(key)
			if value == nil {
				return nil // Key not found, return empty
			}

			// Output the value
			switch v := value.(type) {
			case string:
				fmt.Println(v)
			case bool:
				fmt.Printf("%v\n", v)
			case int, int64, float64:
				fmt.Printf("%v\n", v)
			default:
				// For complex values, output as JSON
				data, _ := json.Marshal(v)
				fmt.Println(string(data))
			}

			return nil
		},
	}
}

// configSetCmd sets a configuration value
func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a specific configuration value.

Keys use dot notation to access nested values.
Values are stored in ~/.conduit/conduit.yaml.

Examples:
  conduit config set embed.provider none
  conduit config set kb.rag.default_limit 20
  conduit config set kb.kag.enabled true`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			homeDir, _ := os.UserHomeDir()
			configDir := filepath.Join(homeDir, ".conduit")
			configPath := filepath.Join(configDir, "conduit.yaml")

			// Ensure config directory exists
			if err := os.MkdirAll(configDir, 0700); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}

			v := viper.New()
			v.SetConfigFile(configPath)
			v.SetConfigType("yaml")

			// Read existing config if it exists
			if err := v.ReadInConfig(); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("read config: %w", err)
				}
				// Config doesn't exist yet, that's fine
			}

			// Set the value
			v.Set(key, value)

			// Write the config
			if err := v.WriteConfig(); err != nil {
				// If the config file doesn't exist, create it
				if os.IsNotExist(err) {
					if err := v.SafeWriteConfig(); err != nil {
						return fmt.Errorf("write config: %w", err)
					}
				} else {
					return fmt.Errorf("write config: %w", err)
				}
			}

			fmt.Printf("Set %s = %s\n", key, value)
			return nil
		},
	}
}

// configUnsetCmd removes a configuration value
func configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a configuration value",
		Long: `Remove a specific configuration value.

Keys use dot notation to access nested values.
The value will be removed from ~/.conduit/conduit.yaml.

Examples:
  conduit config unset embed.model
  conduit config unset kb.rag.default_limit`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")

			// Read current config file directly as YAML
			data, err := os.ReadFile(configPath)
			if err != nil {
				if os.IsNotExist(err) {
					// Config doesn't exist, nothing to unset
					return nil
				}
				return fmt.Errorf("read config: %w", err)
			}

			// Parse the YAML into a map
			var configMap map[string]interface{}
			if err := yaml.Unmarshal(data, &configMap); err != nil {
				return fmt.Errorf("parse config: %w", err)
			}

			// Remove the key using dot notation
			if removeNestedKey(configMap, strings.Split(key, ".")) {
				// Write back
				newData, err := yaml.Marshal(configMap)
				if err != nil {
					return fmt.Errorf("marshal config: %w", err)
				}
				if err := os.WriteFile(configPath, newData, 0600); err != nil {
					return fmt.Errorf("write config: %w", err)
				}
				fmt.Printf("Unset %s\n", key)
			}

			return nil
		},
	}
}

// removeNestedKey removes a nested key from a map using a slice of key parts
func removeNestedKey(m map[string]interface{}, keys []string) bool {
	if len(keys) == 0 {
		return false
	}

	if len(keys) == 1 {
		if _, exists := m[keys[0]]; exists {
			delete(m, keys[0])
			return true
		}
		return false
	}

	// Navigate to nested map
	if nested, ok := m[keys[0]].(map[string]interface{}); ok {
		if removeNestedKey(nested, keys[1:]) {
			// If the nested map is now empty, remove it too
			if len(nested) == 0 {
				delete(m, keys[0])
			}
			return true
		}
	}

	return false
}
