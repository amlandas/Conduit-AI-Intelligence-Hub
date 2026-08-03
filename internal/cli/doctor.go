package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/kbservice"
)

// checkStatus is the outcome of a single doctor check.
type checkStatus string

const (
	checkOK   checkStatus = "ok"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
	checkSkip checkStatus = "skip"
)

// check is one diagnostic result.
type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	// Remedy is the concrete next command, when there is one.
	Remedy string `json:"remedy,omitempty"`
}

func (c check) icon() string {
	switch c.Status {
	case checkOK:
		return "✓"
	case checkWarn:
		return "⚠"
	case checkFail:
		return "✗"
	default:
		return "-"
	}
}

// doctorCmd diagnoses a Conduit installation.
//
// It was rewritten for the v2 topology. There is no daemon to ping, no socket
// to stat, no container runtime to detect and no vector server to reach; what
// is left is the set of things that can actually be wrong: the knowledge base
// file, FTS5, the embedding provider, and whether any AI client is wired up.
func doctorCmd() *cobra.Command {
	var jsonOutput bool
	var probeSeconds int

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the Conduit installation",
		Long: `Check that Conduit can do its job, and say what to do when it cannot.

Checks:
  - configuration loads, and contains no keys Conduit no longer understands
  - the knowledge base file is present, readable and writable
  - SQLite FTS5 (lexical search) is compiled in and initialised
  - the embedding provider is configured and reachable (skipped when
    embed.provider is "none", which is a supported lexical-only mode)
  - the vector index, and whether it is populated
  - at least one AI client has the MCP server configured

Exit codes:
  0  everything needed works (warnings may still be printed)
  1  at least one check failed

Examples:
  conduit doctor
  conduit doctor --json
  conduit doctor --probe-timeout 30    # allow a cold embedding model to load`,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runDoctor(cmd.Context(), time.Duration(probeSeconds)*time.Second)

			failed := 0
			for _, c := range checks {
				if c.Status == checkFail {
					failed++
				}
			}

			if jsonOutput {
				out := map[string]interface{}{
					"checks":  checks,
					"healthy": failed == 0,
					"failed":  failed,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
				if failed > 0 {
					return exitWith(1)
				}
				return nil
			}

			fmt.Println("Conduit Doctor")
			fmt.Println("═══════════════════════════════════════════════════════")
			for _, c := range checks {
				fmt.Printf("  %s %-28s %s\n", c.icon(), c.Name, c.Detail)
				if c.Remedy != "" && c.Status != checkOK {
					fmt.Printf("      → %s\n", c.Remedy)
				}
			}
			fmt.Println()
			if failed == 0 {
				fmt.Println("✓ Conduit is healthy.")
				return nil
			}
			fmt.Printf("✗ %d check(s) failed.\n", failed)
			return exitWith(1)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	cmd.Flags().IntVar(&probeSeconds, "probe-timeout", 15,
		"Seconds to wait for the embedding provider to answer")

	return cmd
}

// runDoctor performs every check and returns the results in display order.
func runDoctor(ctx context.Context, probeTimeout time.Duration) []check {
	if ctx == nil {
		ctx = context.Background()
	}
	var checks []check

	// ---- configuration ----------------------------------------------------
	res, err := config.LoadWithFlags(globalFlags)
	if err != nil {
		return append(checks, check{
			Name:   "configuration",
			Status: checkFail,
			Detail: err.Error(),
			Remedy: "fix or remove the config file, then re-run",
		})
	}
	cfg := res.Config

	if res.File == "" {
		checks = append(checks, check{
			Name: "configuration", Status: checkOK,
			Detail: "using built-in defaults (no config file)",
		})
	} else if len(res.UnknownKeys) > 0 {
		checks = append(checks, check{
			Name: "configuration", Status: checkWarn,
			Detail: fmt.Sprintf("%s has %d unrecognised key(s): %v",
				res.File, len(res.UnknownKeys), res.UnknownKeys),
			Remedy: "these are ignored; remove them (daemon, container and vector-server keys went away in 2.0)",
		})
	} else {
		checks = append(checks, check{
			Name: "configuration", Status: checkOK, Detail: res.File,
		})
	}

	// ---- knowledge base file ----------------------------------------------
	dbPath := cfg.DatabasePath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		checks = append(checks, check{
			Name: "knowledge base file", Status: checkFail,
			Detail: fmt.Sprintf("cannot create %s: %v", filepath.Dir(dbPath), err),
			Remedy: "check directory permissions, or set --db to a writable path",
		})
		return checks
	}

	svc, err := kbservice.Open(cfg)
	if err != nil {
		checks = append(checks, check{
			Name: "knowledge base file", Status: checkFail,
			Detail: fmt.Sprintf("%s: %v", dbPath, err),
			Remedy: "check permissions on the file, or use --db to point elsewhere",
		})
		return checks
	}
	defer svc.Close()

	if info, statErr := os.Stat(dbPath); statErr == nil {
		checks = append(checks, check{
			Name: "knowledge base file", Status: checkOK,
			Detail: fmt.Sprintf("%s (%s)", dbPath, formatBytes(info.Size())),
		})
	} else {
		checks = append(checks, check{
			Name: "knowledge base file", Status: checkOK, Detail: dbPath,
		})
	}

	// A write probe is the only honest test of writability: the file can exist,
	// be readable, and still sit on a read-only mount or be locked.
	if err := probeWritable(ctx, svc); err != nil {
		checks = append(checks, check{
			Name: "knowledge base writable", Status: checkFail,
			Detail: err.Error(),
			Remedy: "close other Conduit processes, or check filesystem permissions",
		})
	} else {
		checks = append(checks, check{
			Name: "knowledge base writable", Status: checkOK, Detail: "yes",
		})
	}

	// ---- capabilities ------------------------------------------------------
	caps := kb.DetectCapabilitiesWithTimeout(ctx, svc.DB(), svc.Embedder(), probeTimeout)

	if caps.FTS5Available {
		checks = append(checks, check{
			Name: "FTS5 lexical search", Status: checkOK, Detail: "available",
		})
	} else {
		checks = append(checks, check{
			Name: "FTS5 lexical search", Status: checkFail,
			Detail: "the kb_fts table is missing",
			Remedy: "this binary may be built without the fts5 tag; reinstall Conduit",
		})
	}

	// ---- embedding provider -------------------------------------------------
	embedInfo := svc.EmbedInfo()
	switch {
	case cfg.Embed.Provider == config.EmbedProviderNone:
		checks = append(checks, check{
			Name: "embedding provider", Status: checkSkip,
			Detail: "disabled (embed.provider = none); search is lexical-only",
		})
	case !embedInfo.Available:
		checks = append(checks, check{
			Name: "embedding provider", Status: checkFail,
			Detail: fmt.Sprintf("%s could not be configured: %s", embedInfo.Provider, embedInfo.Err),
			Remedy: "fix the provider settings, or set embed.provider to \"none\" for lexical-only search",
		})
	case caps.SemanticAvailable:
		checks = append(checks, check{
			Name: "embedding provider", Status: checkOK,
			Detail: fmt.Sprintf("%s reachable (model %s)", embedInfo.Provider, embedInfo.Model),
		})
	default:
		checks = append(checks, check{
			Name: "embedding provider", Status: checkFail,
			Detail: fmt.Sprintf("%s: %s", embedInfo.Provider, caps.EmbedStatus),
			Remedy: embedRemedy(cfg),
		})
	}

	// ---- vector index -------------------------------------------------------
	if cfg.Embed.Provider == config.EmbedProviderNone {
		checks = append(checks, check{
			Name: "vector index", Status: checkSkip,
			Detail: "not used in lexical-only mode",
		})
	} else {
		vectors, verr := svc.VectorCount(ctx)
		switch {
		case verr != nil:
			checks = append(checks, check{
				Name: "vector index", Status: checkWarn,
				Detail: fmt.Sprintf("%s", caps.VectorStatus),
				Remedy: "run 'conduit kb sync' to build it",
			})
		case vectors == 0:
			checks = append(checks, check{
				Name: "vector index", Status: checkWarn,
				Detail: "empty; semantic search will return nothing",
				Remedy: "run 'conduit kb sync' (or 'conduit kb migrate' for already-indexed documents)",
			})
		default:
			checks = append(checks, check{
				Name: "vector index", Status: checkOK,
				Detail: fmt.Sprintf("%d vectors", vectors),
			})
		}
	}

	// ---- content ------------------------------------------------------------
	if totals, _, serr := svc.Stats(ctx); serr != nil {
		checks = append(checks, check{
			Name: "indexed content", Status: checkWarn, Detail: serr.Error(),
		})
	} else if totals.Documents == 0 {
		checks = append(checks, check{
			Name: "indexed content", Status: checkWarn,
			Detail: "no documents indexed",
			Remedy: "run 'conduit kb add <path>' then 'conduit kb sync'",
		})
	} else {
		checks = append(checks, check{
			Name: "indexed content", Status: checkOK,
			Detail: fmt.Sprintf("%d sources, %d documents, %d chunks",
				totals.Sources, totals.Documents, totals.Chunks),
		})
	}

	// ---- MCP client configuration -------------------------------------------
	checks = append(checks, mcpClientCheck())

	return checks
}

// probeWritable verifies the knowledge base accepts a write.
func probeWritable(ctx context.Context, svc *kbservice.Service) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// A temporary table is written to the database but leaves nothing behind,
	// so the probe cannot corrupt or grow a user's knowledge base.
	if _, err := svc.DB().ExecContext(probeCtx,
		`CREATE TEMP TABLE IF NOT EXISTS conduit_doctor_probe (ok INTEGER)`); err != nil {
		return err
	}
	_, err := svc.DB().ExecContext(probeCtx, `DROP TABLE IF EXISTS conduit_doctor_probe`)
	return err
}

// embedRemedy returns the actionable next step for an unreachable provider.
func embedRemedy(cfg *config.Config) string {
	switch cfg.Embed.Provider {
	case config.EmbedProviderOllama:
		return fmt.Sprintf("start Ollama (it should answer at %s), or set embed.provider to \"none\"",
			cfg.Embed.Ollama.Host)
	case config.EmbedProviderLlamaServer:
		return "install llama-server (brew install llama.cpp) and download the model, " +
			"or set embed.provider to \"none\" for lexical-only search"
	default:
		return "set embed.provider to \"llama-server\", \"ollama\" or \"none\""
	}
}

// mcpClientCheck reports whether any supported AI client is wired to Conduit.
func mcpClientCheck() check {
	homeDir, _ := os.UserHomeDir()
	paths := map[string]string{
		"claude-code": filepath.Join(homeDir, ".claude.json"),
		"cursor":      filepath.Join(homeDir, ".cursor", "settings", "extensions.json"),
		"vscode":      filepath.Join(homeDir, ".vscode", "settings.json"),
	}

	var configured []string
	for name, path := range paths {
		if ok, _ := checkMCPClientConfigured(path); ok {
			configured = append(configured, name)
		}
	}

	if len(configured) == 0 {
		return check{
			Name: "MCP client configured", Status: checkWarn,
			Detail: "no AI client is pointed at Conduit",
			Remedy: "run 'conduit mcp configure'",
		}
	}
	return check{
		Name: "MCP client configured", Status: checkOK,
		Detail: fmt.Sprintf("%v", configured),
	}
}
