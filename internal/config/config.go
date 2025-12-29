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
}

// PolicyConfig holds policy engine configuration.
type PolicyConfig struct {
	AllowNetworkEgress bool     `mapstructure:"allow_network_egress"`
	ForbiddenPaths     []string `mapstructure:"forbidden_paths"`
	WarnPaths          []string `mapstructure:"warn_paths"`
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
			WriteTimeout: 30 * time.Second,
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
			MaxFileSize:   10 * 1024 * 1024, // 10MB
			ChunkSize:     1000,
			ChunkOverlap:  100,
			WatchDebounce: 500 * time.Millisecond,
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
