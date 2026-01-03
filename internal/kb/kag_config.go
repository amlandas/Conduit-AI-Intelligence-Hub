// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// kag_config.go defines configuration for the KAG pipeline.
package kb

import "time"

// KAGConfig holds Knowledge-Augmented Generation configuration.
// Security: All values have safe defaults. KAG is opt-in (Enabled: false by default).
type KAGConfig struct {
	// Enabled controls whether KAG pipeline is active
	// Default: false (opt-in for security)
	Enabled bool `mapstructure:"enabled"`

	// PreloadModel loads the extraction model into memory on daemon startup
	// This eliminates cold-start delays but uses ~4GB RAM continuously
	// Default: false (opt-in for RAM management)
	PreloadModel bool `mapstructure:"preload_model"`

	// Provider specifies the LLM provider for entity extraction
	// Options: "ollama" (default), "openai", "anthropic"
	Provider string `mapstructure:"provider"`

	// Graph holds graph database configuration
	Graph GraphConfig `mapstructure:"graph"`

	// Extraction holds entity extraction settings
	Extraction ExtractionConfig `mapstructure:"extraction"`

	// Ollama holds Ollama-specific configuration
	Ollama OllamaConfig `mapstructure:"ollama"`

	// OpenAI holds OpenAI-specific configuration (optional)
	OpenAI OpenAIConfig `mapstructure:"openai"`

	// Anthropic holds Anthropic-specific configuration (optional)
	Anthropic AnthropicConfig `mapstructure:"anthropic"`
}

// GraphConfig holds graph database configuration.
type GraphConfig struct {
	// Backend specifies the graph database to use
	// Options: "falkordb" (default), "sqlite" (embedded fallback)
	Backend string `mapstructure:"backend"`

	// FalkorDB holds FalkorDB-specific settings
	FalkorDB FalkorDBConfig `mapstructure:"falkordb"`
}

// FalkorDBConfig holds FalkorDB (Redis-based graph DB) configuration.
// Security: Binds to localhost by default for security.
type FalkorDBConfig struct {
	// Host is the FalkorDB/Redis host
	// Default: "localhost" (no remote access by default)
	Host string `mapstructure:"host"`

	// Port is the FalkorDB/Redis port
	// Default: 6379
	Port int `mapstructure:"port"`

	// GraphName is the name of the graph in FalkorDB
	// Default: "conduit_kg"
	GraphName string `mapstructure:"graph_name"`

	// Password for Redis authentication (optional)
	// Security: Should be set via environment variable CONDUIT_KB_KAG_GRAPH_FALKORDB_PASSWORD
	Password string `mapstructure:"password"`

	// Database is the Redis database number
	// Default: 0
	Database int `mapstructure:"database"`

	// PoolSize is the connection pool size
	// Default: 10
	PoolSize int `mapstructure:"pool_size"`

	// ConnectTimeout is the connection timeout
	// Default: 5s
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"`

	// ReadTimeout is the read operation timeout
	// Default: 30s
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// WriteTimeout is the write operation timeout
	// Default: 30s
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// ExtractionConfig holds entity extraction settings.
type ExtractionConfig struct {
	// ConfidenceThreshold filters entities/relations below this score
	// Range: 0.0-1.0, Default: 0.7
	ConfidenceThreshold float64 `mapstructure:"confidence_threshold"`

	// MaxEntitiesPerChunk limits entities extracted from each chunk
	// Default: 20 (security: prevents resource exhaustion)
	MaxEntitiesPerChunk int `mapstructure:"max_entities_per_chunk"`

	// MaxRelationsPerChunk limits relations extracted from each chunk
	// Default: 50
	MaxRelationsPerChunk int `mapstructure:"max_relations_per_chunk"`

	// BatchSize is the number of chunks to process concurrently
	// Default: 10
	BatchSize int `mapstructure:"batch_size"`

	// TimeoutSeconds is the timeout for each extraction request
	// Default: 60
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// RetryAttempts is the number of retry attempts on failure
	// Default: 2
	RetryAttempts int `mapstructure:"retry_attempts"`

	// EnableBackground enables asynchronous extraction during sync
	// Default: true
	EnableBackground bool `mapstructure:"enable_background"`
}

// OllamaConfig holds Ollama-specific configuration.
type OllamaConfig struct {
	// Model is the Ollama model to use for extraction
	// Default: "mistral:7b-instruct-q4_K_M" (Apache 2.0, best F1 for NER)
	Model string `mapstructure:"model"`

	// Host is the Ollama API endpoint
	// Default: "http://localhost:11434"
	Host string `mapstructure:"host"`

	// KeepAlive is how long to keep the model loaded
	// Default: "5m"
	KeepAlive string `mapstructure:"keep_alive"`
}

// OpenAIConfig holds OpenAI-specific configuration.
// Security: API key should be set via environment variable OPENAI_API_KEY
type OpenAIConfig struct {
	// Model is the OpenAI model to use
	// Default: "gpt-4o-mini" (cost-effective for extraction)
	Model string `mapstructure:"model"`

	// APIKey is the OpenAI API key
	// Security: Prefer setting via OPENAI_API_KEY environment variable
	APIKey string `mapstructure:"api_key"`

	// BaseURL is the API base URL (for Azure OpenAI or proxies)
	// Default: empty (uses official OpenAI API)
	BaseURL string `mapstructure:"base_url"`
}

// AnthropicConfig holds Anthropic-specific configuration.
// Security: API key should be set via environment variable ANTHROPIC_API_KEY
type AnthropicConfig struct {
	// Model is the Anthropic model to use
	// Default: "claude-3-5-haiku-latest" (fast and cost-effective)
	Model string `mapstructure:"model"`

	// APIKey is the Anthropic API key
	// Security: Prefer setting via ANTHROPIC_API_KEY environment variable
	APIKey string `mapstructure:"api_key"`
}

// DefaultKAGConfig returns secure default configuration for KAG.
func DefaultKAGConfig() KAGConfig {
	return KAGConfig{
		Enabled:      false, // Opt-in for security
		PreloadModel: false, // Opt-in for RAM management
		Provider:     "ollama",

		Graph: GraphConfig{
			Backend: "falkordb",
			FalkorDB: FalkorDBConfig{
				Host:           "localhost", // No remote by default
				Port:           6379,
				GraphName:      "conduit_kg",
				Password:       "",
				Database:       0,
				PoolSize:       10,
				ConnectTimeout: 5 * time.Second,
				ReadTimeout:    30 * time.Second,
				WriteTimeout:   30 * time.Second,
			},
		},

		Extraction: ExtractionConfig{
			ConfidenceThreshold:  0.7,
			MaxEntitiesPerChunk:  20,
			MaxRelationsPerChunk: 50,
			BatchSize:            10,
			TimeoutSeconds:       60,
			RetryAttempts:        2,
			EnableBackground:     true,
		},

		Ollama: OllamaConfig{
			Model:     "mistral:7b-instruct-q4_K_M",
			Host:      "http://localhost:11434",
			KeepAlive: "5m",
		},

		OpenAI: OpenAIConfig{
			Model:   "gpt-4o-mini",
			APIKey:  "", // Must be set via env var
			BaseURL: "",
		},

		Anthropic: AnthropicConfig{
			Model:  "claude-3-5-haiku-latest",
			APIKey: "", // Must be set via env var
		},
	}
}

// Validate checks KAG configuration for errors.
func (c *KAGConfig) Validate() error {
	if !c.Enabled {
		return nil // Nothing to validate if disabled
	}

	// Validate provider
	validProviders := map[string]bool{
		"ollama":    true,
		"openai":    true,
		"anthropic": true,
	}
	if !validProviders[c.Provider] {
		return ErrInvalidLLMProvider
	}

	// Validate graph backend
	validBackends := map[string]bool{
		"falkordb": true,
		"sqlite":   true,
	}
	if !validBackends[c.Graph.Backend] {
		return ErrInvalidGraphBackend
	}

	// Validate confidence threshold
	if c.Extraction.ConfidenceThreshold < 0.0 || c.Extraction.ConfidenceThreshold > 1.0 {
		return ErrInvalidConfidence
	}

	return nil
}

// IsEnabled returns whether KAG is enabled and properly configured.
func (c *KAGConfig) IsEnabled() bool {
	return c.Enabled && c.Validate() == nil
}

// GetProviderModel returns the model string for the configured provider.
func (c *KAGConfig) GetProviderModel() string {
	switch c.Provider {
	case "ollama":
		return c.Ollama.Model
	case "openai":
		return c.OpenAI.Model
	case "anthropic":
		return c.Anthropic.Model
	default:
		return c.Ollama.Model
	}
}

// GetProviderEndpoint returns the endpoint for the configured provider.
func (c *KAGConfig) GetProviderEndpoint() string {
	switch c.Provider {
	case "ollama":
		return c.Ollama.Host
	case "openai":
		if c.OpenAI.BaseURL != "" {
			return c.OpenAI.BaseURL
		}
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	default:
		return c.Ollama.Host
	}
}
