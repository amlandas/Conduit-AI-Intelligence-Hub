// Package config handles Conduit configuration loading and management.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return homeDir
	}
	return path
}

// Config holds all Conduit configuration.
type Config struct {
	// Daemon configuration
	DataDir    string `mapstructure:"data_dir"`
	SocketPath string `mapstructure:"socket"`
	LogLevel   string `mapstructure:"log_level"`
	LogFormat  string `mapstructure:"log_format"`

	// API configuration
	API APIConfig `mapstructure:"api"`

	// Runtime configuration
	Runtime RuntimeConfig `mapstructure:"runtime"`

	// KB configuration
	KB KBConfig `mapstructure:"kb"`

	// Policy configuration
	Policy PolicyConfig `mapstructure:"policy"`

	// AI configuration
	AI AIConfig `mapstructure:"ai"`

	// MCP configuration
	MCP MCPConfig `mapstructure:"mcp"`
}

// AIConfig holds AI provider configuration.
type AIConfig struct {
	// Provider: "ollama" (default, local) or "anthropic" (BYOK)
	Provider string `mapstructure:"provider"`

	// Model to use (e.g., "qwen2.5-coder:7b" for Ollama, "claude-sonnet-4-20250514" for Anthropic)
	Model string `mapstructure:"model"`

	// Endpoint for Ollama API (default: http://localhost:11434)
	Endpoint string `mapstructure:"endpoint"`

	// TimeoutSeconds for AI API calls
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// MaxRetries for failed AI requests
	MaxRetries int `mapstructure:"max_retries"`

	// ConfidenceThreshold below which to warn the user
	ConfidenceThreshold float64 `mapstructure:"confidence_threshold"`
}

// APIConfig holds API server configuration.
type APIConfig struct {
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

// RuntimeConfig holds container runtime configuration.
type RuntimeConfig struct {
	Preferred      string        `mapstructure:"preferred"` // "podman", "docker", or "auto"
	PullTimeout    time.Duration `mapstructure:"pull_timeout"`
	StartTimeout   time.Duration `mapstructure:"start_timeout"`
	StopTimeout    time.Duration `mapstructure:"stop_timeout"`
	HealthInterval time.Duration `mapstructure:"health_interval"`
}

// KBConfig holds knowledge base configuration.
type KBConfig struct {
	Workers       int           `mapstructure:"workers"`
	MaxFileSize   int64         `mapstructure:"max_file_size"`
	ChunkSize     int           `mapstructure:"chunk_size"`
	ChunkOverlap  int           `mapstructure:"chunk_overlap"`
	WatchDebounce time.Duration `mapstructure:"watch_debounce"`

	// RAG (Retrieval-Augmented Generation) settings
	RAG RAGConfig `mapstructure:"rag"`

	// KAG (Knowledge-Augmented Generation) settings
	KAG KAGConfig `mapstructure:"kag"`
}

// KAGConfig holds Knowledge-Augmented Generation configuration.
// Note: Full config is defined in internal/kb/kag_config.go
type KAGConfig struct {
	// Enabled controls whether KAG pipeline is active
	Enabled bool `mapstructure:"enabled"`

	// PreloadModel loads the extraction model into memory on daemon startup
	// This eliminates cold-start delays but uses ~4GB RAM continuously
	// Default: false (opt-in)
	PreloadModel bool `mapstructure:"preload_model"`

	// Provider specifies the LLM provider: "ollama", "openai", "anthropic"
	Provider string `mapstructure:"provider"`

	// Graph holds graph database configuration
	Graph KAGGraphConfig `mapstructure:"graph"`

	// Extraction holds entity extraction settings
	Extraction KAGExtractionConfig `mapstructure:"extraction"`

	// Ollama holds Ollama-specific configuration
	Ollama KAGOllamaConfig `mapstructure:"ollama"`
}

// KAGGraphConfig holds graph database configuration.
type KAGGraphConfig struct {
	Backend  string              `mapstructure:"backend"`
	FalkorDB KAGFalkorDBConfig   `mapstructure:"falkordb"`
}

// KAGFalkorDBConfig holds FalkorDB configuration.
type KAGFalkorDBConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	GraphName string `mapstructure:"graph_name"`
	Password  string `mapstructure:"password"`
}

// KAGExtractionConfig holds entity extraction settings.
type KAGExtractionConfig struct {
	ConfidenceThreshold  float64 `mapstructure:"confidence_threshold"`
	MaxEntitiesPerChunk  int     `mapstructure:"max_entities_per_chunk"`
	MaxRelationsPerChunk int     `mapstructure:"max_relations_per_chunk"`
	BatchSize            int     `mapstructure:"batch_size"`
	TimeoutSeconds       int     `mapstructure:"timeout_seconds"`
}

// KAGOllamaConfig holds Ollama-specific configuration.
type KAGOllamaConfig struct {
	Model     string `mapstructure:"model"`
	Host      string `mapstructure:"host"`
	KeepAlive string `mapstructure:"keep_alive"`
}

// RAGConfig holds advanced RAG/search tuning parameters.
// These settings control how semantic search retrieves and ranks results.
// Lower thresholds = more results (better recall), higher = fewer results (better precision).
type RAGConfig struct {
	// MinScore is the minimum similarity score threshold (0.0-1.0).
	// Results below this score are filtered out.
	// Default: 0.1 (permissive - let the LLM decide relevance)
	MinScore float64 `mapstructure:"min_score"`

	// SemanticWeight controls the balance between semantic and lexical search (0.0-1.0).
	// 0.0 = pure lexical (FTS5), 1.0 = pure semantic (vectors), 0.5 = balanced
	// Default: 0.5
	SemanticWeight float64 `mapstructure:"semantic_weight"`

	// EnableMMR enables Maximal Marginal Relevance for result diversity.
	// When true, results are diversified to avoid redundant content.
	// Default: true
	EnableMMR bool `mapstructure:"enable_mmr"`

	// MMRLambda controls the relevance vs diversity tradeoff (0.0-1.0).
	// 0.0 = maximum diversity, 1.0 = maximum relevance
	// Default: 0.7
	MMRLambda float64 `mapstructure:"mmr_lambda"`

	// EnableRerank enables semantic reranking of top candidates.
	// Improves precision by re-scoring using semantic similarity.
	// Default: true
	EnableRerank bool `mapstructure:"enable_rerank"`

	// DefaultLimit is the default number of results to return.
	// Default: 10
	DefaultLimit int `mapstructure:"default_limit"`
}

// PolicyConfig holds policy engine configuration.
type PolicyConfig struct {
	AllowNetworkEgress bool     `mapstructure:"allow_network_egress"`
	ForbiddenPaths     []string `mapstructure:"forbidden_paths"`
	WarnPaths          []string `mapstructure:"warn_paths"`
}

// MCPConfig holds MCP (Model Context Protocol) server configuration.
type MCPConfig struct {
	// KB holds Knowledge Base MCP server settings
	KB MCPKBConfig `mapstructure:"kb"`
}

// MCPKBConfig holds Knowledge Base MCP server settings.
type MCPKBConfig struct {
	// Search settings
	Search MCPSearchConfig `mapstructure:"search"`

	// Logging settings
	Logging MCPLoggingConfig `mapstructure:"logging"`
}

// MCPSearchConfig holds MCP search behavior settings.
type MCPSearchConfig struct {
	// DefaultMode is the default search mode: "hybrid", "semantic", or "fts5"
	// Default: "hybrid"
	DefaultMode string `mapstructure:"default_mode"`

	// DefaultLimit is the default number of results per search
	// Default: 10
	DefaultLimit int `mapstructure:"default_limit"`

	// MaxLimit is the maximum allowed limit for search results
	// Default: 50
	MaxLimit int `mapstructure:"max_limit"`

	// SemanticFallback enables fallback to FTS5 when semantic search is unavailable
	// Default: true
	SemanticFallback bool `mapstructure:"semantic_fallback"`
}

// MCPLoggingConfig holds MCP logging settings.
type MCPLoggingConfig struct {
	// Level is the log level: "debug", "info", "warn", "error"
	// Default: "info"
	Level string `mapstructure:"level"`

	// ToStderr enables logging to stderr (visible in AI client)
	// Default: false
	ToStderr bool `mapstructure:"to_stderr"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".conduit")
	socketPath := filepath.Join(dataDir, "conduit.sock")

	if runtime.GOOS == "windows" {
		socketPath = `\\.\pipe\conduit`
	}

	return &Config{
		DataDir:    dataDir,
		SocketPath: socketPath,
		LogLevel:   "info",
		LogFormat:  "json",

		API: APIConfig{
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute, // Long-running ops like kb sync need time
			IdleTimeout:  120 * time.Second,
		},

		Runtime: RuntimeConfig{
			Preferred:      "auto",
			PullTimeout:    10 * time.Minute,
			StartTimeout:   30 * time.Second,
			StopTimeout:    10 * time.Second,
			HealthInterval: 30 * time.Second,
		},

		KB: KBConfig{
			Workers:       4,
			MaxFileSize:   100 * 1024 * 1024, // 100MB
			ChunkSize:     1000,
			ChunkOverlap:  100,
			WatchDebounce: 500 * time.Millisecond,
			RAG: RAGConfig{
				MinScore:       0.0,  // No filtering - return all results, let LLM decide relevance
				SemanticWeight: 0.5,  // Balanced hybrid search
				EnableMMR:      true, // Diversity enabled
				MMRLambda:      0.7,  // 70% relevance, 30% diversity
				EnableRerank:   true, // Reranking enabled
				DefaultLimit:   10,   // 10 results by default
			},
			KAG: KAGConfig{
				Enabled:      false, // Opt-in for security
				PreloadModel: false, // Opt-in for RAM management
				Provider:     "ollama",
				Graph: KAGGraphConfig{
					Backend: "falkordb",
					FalkorDB: KAGFalkorDBConfig{
						Host:      "localhost",
						Port:      6379,
						GraphName: "conduit_kg",
						Password:  "",
					},
				},
				Extraction: KAGExtractionConfig{
					ConfidenceThreshold:  0.7,
					MaxEntitiesPerChunk:  20,
					MaxRelationsPerChunk: 50,
					BatchSize:            10,
					TimeoutSeconds:       60,
				},
				Ollama: KAGOllamaConfig{
					Model:     "mistral:7b-instruct-q4_K_M",
					Host:      "http://localhost:11434",
					KeepAlive: "5m",
				},
			},
		},

		Policy: PolicyConfig{
			AllowNetworkEgress: false,
			ForbiddenPaths: []string{
				"/",
				"/etc",
				"/var",
				"/usr",
				"~/.ssh",
				"~/.aws",
				"~/.gnupg",
				"~/.config/gcloud",
				"~/.kube",
			},
			WarnPaths: []string{
				"~/.config",
				"~/Documents",
				"~/Desktop",
			},
		},

		AI: AIConfig{
			Provider:            "ollama",
			Model:               "qwen2.5-coder:7b",
			Endpoint:            "http://localhost:11434",
			TimeoutSeconds:      120,
			MaxRetries:          2,
			ConfidenceThreshold: 0.6,
		},

		MCP: MCPConfig{
			KB: MCPKBConfig{
				Search: MCPSearchConfig{
					DefaultMode:      "hybrid",
					DefaultLimit:     10,
					MaxLimit:         50,
					SemanticFallback: true,
				},
				Logging: MCPLoggingConfig{
					Level:    "info",
					ToStderr: false,
				},
			},
		},
	}
}

// Load loads configuration from files and environment.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetConfigName("conduit")
	v.SetConfigType("yaml")

	// Configuration search paths
	homeDir, _ := os.UserHomeDir()
	v.AddConfigPath(filepath.Join(homeDir, ".conduit"))
	v.AddConfigPath("/etc/conduit")
	v.AddConfigPath(".")

	// Environment variable binding
	v.SetEnvPrefix("CONDUIT")
	v.AutomaticEnv()

	// Read configuration file if it exists
	if err := v.ReadInConfig(); err != nil {
		// Config file not found is OK, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	// Unmarshal into config struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Expand tildes in path fields
	cfg.DataDir = expandPath(cfg.DataDir)
	cfg.SocketPath = expandPath(cfg.SocketPath)

	// Expand tildes in policy paths
	for i, p := range cfg.Policy.ForbiddenPaths {
		cfg.Policy.ForbiddenPaths[i] = expandPath(p)
	}
	for i, p := range cfg.Policy.WarnPaths {
		cfg.Policy.WarnPaths[i] = expandPath(p)
	}

	return cfg, nil
}

// DatabasePath returns the path to the SQLite database.
func (c *Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "conduit.db")
}

// BackupsDir returns the path to the backups directory.
func (c *Config) BackupsDir() string {
	return filepath.Join(c.DataDir, "backups")
}

// LogPath returns the path to the log file.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "conduit.log")
}

// EnsureDirectories creates required directories.
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.DataDir,
		c.BackupsDir(),
		filepath.Join(c.DataDir, "connectors"),
		filepath.Join(c.DataDir, "kb"),
		c.AICacheDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	return nil
}

// AICacheDir returns the path to the AI cache directory.
func (c *Config) AICacheDir() string {
	return filepath.Join(c.DataDir, "ai-cache")
}

// ConnectorsDir returns the path to the connectors directory.
func (c *Config) ConnectorsDir() string {
	return filepath.Join(c.DataDir, "connectors")
}
