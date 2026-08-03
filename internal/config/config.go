// Package config is the single source of truth for Conduit configuration.
//
// # Precedence
//
// Values are resolved highest-wins in this order:
//
//  1. command-line flags   (bound explicitly via BindFlags)
//  2. environment          (CONDUIT_* , nested keys use "_" for ".")
//  3. configuration file   (~/.conduit/conduit.yaml, /etc/conduit, ./conduit.yaml)
//  4. compiled defaults    (DefaultConfig)
//
// # One schema
//
// The Config struct below IS the schema. Anything not reachable from it is not
// a Conduit setting; Load reports unknown keys found in the file so that a
// stale key (from a version that had a daemon, containers or a vector server)
// is visible rather than silently ignored.
//
// # What is deliberately absent
//
// WP-3.2 removed the daemon, the container runtime and the external vector and
// graph servers. There is therefore no socket path, no HTTP server timeout, no
// container runtime preference, no Qdrant port and no FalkorDB host. A config
// file that still carries those keys loads fine and warns.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Embedding provider identifiers accepted by embed.provider.
const (
	// EmbedProviderLlamaServer is the default: a managed llama-server sidecar
	// bound to loopback, started on demand by Conduit itself.
	EmbedProviderLlamaServer = "llama-server"

	// EmbedProviderOllama uses a user-installed Ollama daemon.
	EmbedProviderOllama = "ollama"

	// EmbedProviderNone disables embeddings. Search runs lexical-only (FTS5).
	// This is a fully supported mode, not a degraded one.
	EmbedProviderNone = "none"
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
	// DataDir is the root of everything Conduit owns on disk.
	DataDir string `mapstructure:"data_dir"`

	// DBPath overrides the knowledge base SQLite file location. Empty means
	// <data_dir>/conduit.db. The --db global flag binds here, which is the
	// workspace-isolation seam: one binary, many independent knowledge bases.
	DBPath string `mapstructure:"db_path"`

	LogLevel  string `mapstructure:"log_level"`
	LogFormat string `mapstructure:"log_format"`

	// KB holds knowledge base ingestion and retrieval settings.
	KB KBConfig `mapstructure:"kb"`

	// Embed selects and configures the embedding provider.
	Embed EmbedConfig `mapstructure:"embed"`

	// MCP holds Model Context Protocol server settings.
	MCP MCPConfig `mapstructure:"mcp"`

	// Policy holds path-safety settings.
	Policy PolicyConfig `mapstructure:"policy"`

	// Telemetry holds local-only instrumentation settings.
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
}

// EmbedConfig selects the embedding provider and its transport settings.
//
// Provider selection is the only knob most users need. The per-provider
// sub-sections exist for people who already run their own stack.
type EmbedConfig struct {
	// Provider is one of "llama-server" (default), "ollama" or "none".
	//
	// "none" is a first-class mode: the knowledge base indexes and searches
	// with FTS5 alone, no model is loaded and no port is opened.
	Provider string `mapstructure:"provider"`

	// Model is the embedding model identifier. For llama-server it must be a
	// key in the pinned model registry (internal/embed/registry.go); for
	// Ollama it is the Ollama model tag.
	Model string `mapstructure:"model"`

	// Dimensions overrides the vector width. 0 means "take it from the model
	// registry", which is the only value that cannot be wrong.
	Dimensions int `mapstructure:"dimensions"`

	// TimeoutSeconds bounds a single embedding call including retries.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// BatchSize caps how many texts go into one provider request.
	BatchSize int `mapstructure:"batch_size"`

	// LlamaServer configures the managed sidecar.
	LlamaServer LlamaServerConfig `mapstructure:"llama_server"`

	// Ollama configures the optional Ollama provider.
	Ollama EmbedOllamaConfig `mapstructure:"ollama"`
}

// LlamaServerConfig configures the managed llama-server sidecar.
type LlamaServerConfig struct {
	// Binary overrides llama-server discovery. Empty means search PATH and
	// the Conduit data directory.
	Binary string `mapstructure:"binary"`

	// ModelPath overrides the GGUF file location. Empty means
	// <data_dir>/models/<registry filename>.
	ModelPath string `mapstructure:"model_path"`

	// Port is the loopback port for the sidecar. 0 means pick a free one.
	Port int `mapstructure:"port"`

	// IdleTimeout stops the sidecar after this much inactivity. 0 disables
	// automatic shutdown.
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`
}

// EmbedOllamaConfig configures the optional Ollama embedding provider.
type EmbedOllamaConfig struct {
	// Host is the Ollama base URL.
	Host string `mapstructure:"host"`
}

// TelemetryConfig holds local-only instrumentation settings.
//
// Nothing in this section sends anything anywhere. There is no endpoint, no
// upload, no identifier. It writes a file under the Conduit data directory that
// only the machine's owner can read, and Conduit itself never reads it back.
type TelemetryConfig struct {
	// LocalQueryLog appends one line per knowledge base query to
	// <data_dir>/query-shape.jsonl, recording query *shape* only -- token count,
	// whether the query looks like it names an entity, and the requested
	// traversal depth. Never the query text, never the results.
	//
	// Default: true. Its purpose is to answer one question with evidence
	// instead of opinion: does anyone actually ask multi-hop questions? The
	// knowledge graph's future is gated on that answer. Set to false to turn it
	// off entirely; no file is created.
	LocalQueryLog bool `mapstructure:"local_query_log"`
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
type KAGConfig struct {
	// Enabled controls whether the knowledge graph is active. Off by default:
	// no graph tables exist in the database until this is true.
	Enabled bool `mapstructure:"enabled"`

	// PreloadModel loads the extraction model into memory up front. This
	// eliminates cold-start delay but holds the model's RAM for the process
	// lifetime. Only meaningful when Provider is "ollama".
	PreloadModel bool `mapstructure:"preload_model"`

	// Provider selects the extraction backend: "pattern" (default, no LLM, no
	// network) or "ollama".
	Provider string `mapstructure:"provider"`

	// Graph holds graph storage configuration.
	Graph KAGGraphConfig `mapstructure:"graph"`

	// Extraction holds entity extraction settings.
	Extraction KAGExtractionConfig `mapstructure:"extraction"`

	// Ollama holds Ollama-specific extraction settings.
	Ollama KAGOllamaConfig `mapstructure:"ollama"`
}

// KAGGraphConfig holds graph storage configuration.
//
// WP-2.3 deleted the FalkorDB backend; the graph is stored in the knowledge base
// SQLite file. There is no host, port or password because there is no server.
type KAGGraphConfig struct {
	// Backend is the storage engine. Only "sqlite" is supported; the field is
	// kept so a config file written before WP-2.3 still parses.
	Backend string `mapstructure:"backend"`

	// MaxHops caps graph traversal depth (1-2).
	MaxHops int `mapstructure:"max_hops"`
}

// KAGExtractionConfig holds entity extraction settings.
type KAGExtractionConfig struct {
	ConfidenceThreshold  float64 `mapstructure:"confidence_threshold"`
	MaxEntitiesPerChunk  int     `mapstructure:"max_entities_per_chunk"`
	MaxRelationsPerChunk int     `mapstructure:"max_relations_per_chunk"`
	BatchSize            int     `mapstructure:"batch_size"`
	TimeoutSeconds       int     `mapstructure:"timeout_seconds"`
}

// KAGOllamaConfig holds Ollama-specific configuration for graph extraction.
type KAGOllamaConfig struct {
	Model     string `mapstructure:"model"`
	Host      string `mapstructure:"host"`
	KeepAlive string `mapstructure:"keep_alive"`
}

// RAGConfig holds retrieval tuning parameters.
// Lower thresholds = more results (better recall), higher = fewer (better precision).
type RAGConfig struct {
	// MinScore is the minimum similarity score threshold (0.0-1.0).
	MinScore float64 `mapstructure:"min_score"`

	// SemanticWeight balances semantic against lexical retrieval (0.0-1.0).
	SemanticWeight float64 `mapstructure:"semantic_weight"`

	// EnableMMR turns on Maximal Marginal Relevance result diversification.
	EnableMMR bool `mapstructure:"enable_mmr"`

	// MMRLambda trades relevance against diversity (0.0-1.0).
	MMRLambda float64 `mapstructure:"mmr_lambda"`

	// EnableRerank turns on semantic reranking of top candidates.
	EnableRerank bool `mapstructure:"enable_rerank"`

	// DefaultLimit is the default number of results to return.
	DefaultLimit int `mapstructure:"default_limit"`
}

// PolicyConfig holds path-safety configuration.
type PolicyConfig struct {
	ForbiddenPaths []string `mapstructure:"forbidden_paths"`
	WarnPaths      []string `mapstructure:"warn_paths"`
}

// MCPConfig holds MCP (Model Context Protocol) server configuration.
type MCPConfig struct {
	// KB holds Knowledge Base MCP server settings
	KB MCPKBConfig `mapstructure:"kb"`
}

// MCPKBConfig holds Knowledge Base MCP server settings.
type MCPKBConfig struct {
	Search  MCPSearchConfig  `mapstructure:"search"`
	Logging MCPLoggingConfig `mapstructure:"logging"`
}

// MCPSearchConfig holds MCP search behavior settings.
type MCPSearchConfig struct {
	// DefaultMode is the default search mode: "hybrid", "semantic", or "fts5"
	DefaultMode string `mapstructure:"default_mode"`

	// DefaultLimit is the default number of results per search
	DefaultLimit int `mapstructure:"default_limit"`

	// MaxLimit is the maximum allowed limit for search results
	MaxLimit int `mapstructure:"max_limit"`

	// SemanticFallback enables fallback to FTS5 when semantic search is unavailable
	SemanticFallback bool `mapstructure:"semantic_fallback"`
}

// MCPLoggingConfig holds MCP logging settings.
type MCPLoggingConfig struct {
	// Level is the log level: "debug", "info", "warn", "error"
	Level string `mapstructure:"level"`

	// ToStderr enables logging to stderr (visible in the AI client)
	ToStderr bool `mapstructure:"to_stderr"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".conduit")

	return &Config{
		DataDir:   dataDir,
		LogLevel:  "info",
		LogFormat: "json",

		KB: KBConfig{
			Workers:       4,
			MaxFileSize:   100 * 1024 * 1024, // 100MB
			ChunkSize:     1000,
			ChunkOverlap:  100,
			WatchDebounce: 500 * time.Millisecond,
			RAG: RAGConfig{
				MinScore:       0.0,  // No filtering - return all results, let the client decide
				SemanticWeight: 0.5,  // Balanced hybrid search
				EnableMMR:      true, // Diversity enabled
				MMRLambda:      0.7,  // 70% relevance, 30% diversity
				EnableRerank:   true, // Reranking enabled
				DefaultLimit:   10,
			},
			KAG: KAGConfig{
				Enabled:      false,     // Opt-in: no graph tables exist until this is true
				PreloadModel: false,     // Opt-in for RAM management
				Provider:     "pattern", // No LLM, no network, on the default enabled path
				Graph: KAGGraphConfig{
					Backend: "sqlite",
					MaxHops: 2,
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

		Embed: EmbedConfig{
			Provider:       EmbedProviderLlamaServer,
			Model:          "", // empty means embed.DefaultModelID
			Dimensions:     0,  // 0 means take it from the model registry
			TimeoutSeconds: 30,
			BatchSize:      32,
			LlamaServer: LlamaServerConfig{
				Port:        0, // 0 means pick a free loopback port
				IdleTimeout: 10 * time.Minute,
			},
			Ollama: EmbedOllamaConfig{
				Host: "http://localhost:11434",
			},
		},

		Policy: PolicyConfig{
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

		Telemetry: TelemetryConfig{
			// On by default because it is local-only and because the question it
			// answers -- is multi-hop retrieval actually wanted? -- cannot be
			// answered retroactively.
			LocalQueryLog: true,
		},
	}
}

// LoadResult carries a loaded config plus anything worth telling the operator.
type LoadResult struct {
	Config *Config

	// File is the configuration file that was read, or "" if none was found.
	File string

	// UnknownKeys lists dotted keys present in the file that the schema does
	// not define. These are almost always leftovers from a removed subsystem.
	UnknownKeys []string
}

// Load reads configuration using the documented precedence and returns the
// result. It never fails because a config file is absent.
func Load() (*Config, error) {
	res, err := LoadWithFlags(nil)
	if err != nil {
		return nil, err
	}
	return res.Config, nil
}

// LoadDetailed is Load plus the diagnostics an operator-facing command wants.
func LoadDetailed() (*LoadResult, error) {
	return LoadWithFlags(nil)
}

// LoadWithFlags loads configuration and, if flags is non-nil, gives any flag
// the user actually set the highest precedence.
//
// Flag names map to config keys by replacing "-" with "_" and "." stays "."
// (e.g. --log-level -> log_level, --db -> db_path via the explicit alias below).
func LoadWithFlags(flags *pflag.FlagSet) (*LoadResult, error) {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetConfigName("conduit")
	v.SetConfigType("yaml")

	// Configuration search paths, first match wins.
	homeDir, _ := os.UserHomeDir()
	v.AddConfigPath(filepath.Join(homeDir, ".conduit"))
	v.AddConfigPath("/etc/conduit")
	v.AddConfigPath(".")

	// Environment: CONDUIT_KB_CHUNK_SIZE -> kb.chunk_size
	v.SetEnvPrefix("CONDUIT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	res := &LoadResult{}

	if err := v.ReadInConfig(); err != nil {
		// A missing config file is the normal case, not an error.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	} else {
		res.File = v.ConfigFileUsed()
		res.UnknownKeys = unknownKeys(v.AllSettings())
	}

	// Flags win over everything, but only when explicitly set: an unset flag
	// carries its own default, which would otherwise clobber the file.
	if flags != nil {
		bindChangedFlags(v, flags)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	cfg.normalize()
	res.Config = cfg
	return res, nil
}

// flagToKey maps a command-line flag name to its config key.
var flagToKey = map[string]string{
	"db":        "db_path",
	"data-dir":  "data_dir",
	"log-level": "log_level",
}

func bindChangedFlags(v *viper.Viper, flags *pflag.FlagSet) {
	flags.Visit(func(f *pflag.Flag) {
		key, ok := flagToKey[f.Name]
		if !ok {
			return
		}
		v.Set(key, f.Value.String())
	})
}

// normalize applies path expansion and repairs values that cannot be honoured.
func (c *Config) normalize() {
	c.DataDir = expandPath(c.DataDir)
	c.DBPath = expandPath(c.DBPath)

	for i, p := range c.Policy.ForbiddenPaths {
		c.Policy.ForbiddenPaths[i] = expandPath(p)
	}
	for i, p := range c.Policy.WarnPaths {
		c.Policy.WarnPaths[i] = expandPath(p)
	}

	switch c.Embed.Provider {
	case EmbedProviderLlamaServer, EmbedProviderOllama, EmbedProviderNone:
	case "":
		c.Embed.Provider = EmbedProviderLlamaServer
	default:
		// An unrecognised provider must not silently become the default: that
		// would index with the wrong model. Fall back to lexical-only, which is
		// always correct, and let doctor report it.
		c.Embed.Provider = EmbedProviderNone
	}

	// The graph has exactly one backend since WP-2.3.
	if c.KB.KAG.Graph.Backend == "" {
		c.KB.KAG.Graph.Backend = "sqlite"
	}
}

// DatabasePath returns the path to the knowledge base SQLite file.
//
// db_path (and therefore the --db flag) wins; otherwise the file lives under
// the data directory.
func (c *Config) DatabasePath() string {
	if c.DBPath != "" {
		return c.DBPath
	}
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

// ModelsDir returns the directory holding downloaded embedding models.
func (c *Config) ModelsDir() string {
	return filepath.Join(c.DataDir, "models")
}

// QueryLogPath returns the path to the local query-shape log.
func (c *Config) QueryLogPath() string {
	return filepath.Join(c.DataDir, "query-shape.jsonl")
}

// EnsureDirectories creates required directories.
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.DataDir,
		c.BackupsDir(),
		c.ModelsDir(),
		filepath.Join(c.DataDir, "kb"),
	}

	// A --db pointing outside the data directory still needs its parent.
	if c.DBPath != "" {
		dirs = append(dirs, filepath.Dir(c.DBPath))
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	return nil
}

// KnownKeys returns every dotted configuration key the schema defines, sorted.
//
// It is derived from the Config struct by reflection, so the schema and this
// list cannot drift.
func KnownKeys() []string {
	set := make(map[string]struct{})
	collectKeys(reflect.TypeOf(Config{}), "", set)
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func collectKeys(t reflect.Type, prefix string, out map[string]struct{}) {
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		out[key] = struct{}{}

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		// time.Duration is a struct-free named int64; nothing to recurse into.
		if ft.Kind() == reflect.Struct && ft != reflect.TypeOf(time.Time{}) {
			collectKeys(ft, key, out)
		}
	}
}

// unknownKeys reports dotted keys in settings that the schema does not define.
func unknownKeys(settings map[string]interface{}) []string {
	known := make(map[string]struct{}, len(KnownKeys()))
	for _, k := range KnownKeys() {
		known[k] = struct{}{}
	}

	var unknown []string
	var walk func(m map[string]interface{}, prefix string)
	walk = func(m map[string]interface{}, prefix string) {
		for k, val := range m {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			if _, ok := known[key]; !ok {
				// Report the outermost unknown key only; listing every leaf
				// under a removed section is noise.
				unknown = append(unknown, key)
				continue
			}
			if child, ok := val.(map[string]interface{}); ok {
				walk(child, key)
			}
		}
	}
	walk(settings, "")
	sort.Strings(unknown)
	return unknown
}

// FormatUnknownKeys renders a human-readable warning, or "" when there is
// nothing to warn about.
func FormatUnknownKeys(file string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "warning: %s contains %d unrecognised key(s):\n", file, len(keys))
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s\n", k)
	}
	b.WriteString("These are ignored. Keys for the daemon, container runtime, " +
		"Qdrant and FalkorDB were removed in Conduit 2.0.\n")
	return b.String()
}
