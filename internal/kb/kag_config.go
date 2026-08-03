// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// kag_config.go defines configuration for the KAG pipeline.
package kb

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

	// Provider specifies the extraction backend.
	// Options: "pattern" (default, no LLM, no network), "ollama", "openai", "anthropic".
	//
	// The default is deliberately the cheap pattern extractor: enabling the
	// knowledge graph must not silently enable a 4GB model load or an outbound
	// API call. LLM extraction is a second, explicit opt-in on top of KAG.
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

// GraphConfig holds graph storage configuration.
//
// WP-2.3 deleted the FalkorDB backend along with its container, its loopback
// port and the go-redis dependency. The knowledge graph now lives in the same
// SQLite file as the rest of the knowledge base, so there is nothing to connect
// to and nothing to authenticate against.
type GraphConfig struct {
	// Backend specifies the graph storage engine.
	// The only supported value is "sqlite"; the field is retained so an existing
	// config file that names a backend still parses.
	Backend string `mapstructure:"backend"`

	// MaxHops caps traversal depth for a graph query.
	// Range: 1-2 (see MaxGraphHops), Default: 2
	MaxHops int `mapstructure:"max_hops"`
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
		Enabled:      false,     // Opt-in: no graph tables exist until this is true
		PreloadModel: false,     // Opt-in for RAM management
		Provider:     "pattern", // No LLM, no network, in the default enabled path

		Graph: GraphConfig{
			Backend: "sqlite",
			MaxHops: MaxGraphHops,
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
		"pattern":   true,
		"ollama":    true,
		"openai":    true,
		"anthropic": true,
	}
	if !validProviders[c.Provider] {
		return ErrInvalidLLMProvider
	}

	// Validate graph backend. SQLite is the only engine; an empty value is
	// treated as "sqlite" so a config written before WP-2.3 still loads.
	if c.Graph.Backend != "" && c.Graph.Backend != "sqlite" {
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

// UsesLLM reports whether the configured extraction provider makes model calls.
// The default provider ("pattern") does not.
func (c *KAGConfig) UsesLLM() bool {
	return c.Provider != "" && c.Provider != "pattern"
}

// GraphMaxHops returns the configured traversal depth, clamped to MaxGraphHops.
func (c *KAGConfig) GraphMaxHops() int {
	hops := c.Graph.MaxHops
	if hops <= 0 {
		return MaxGraphHops
	}
	if hops > MaxGraphHops {
		return MaxGraphHops
	}
	return hops
}

// GetProviderModel returns the model string for the configured provider.
func (c *KAGConfig) GetProviderModel() string {
	switch c.Provider {
	case "pattern":
		return "pattern"
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
	case "pattern":
		return "" // local, in-process
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
