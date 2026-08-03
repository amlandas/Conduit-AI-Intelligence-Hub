package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/embed"
)

// modelCmd manages the local embedding model artifacts.
//
// Conduit ships no model weights. The first time a machine needs embeddings it
// has to fetch a few hundred megabytes of GGUF, and this is the command that
// does it. Everything about which model, from which repository, at which exact
// hash, comes from the pinned registry in internal/embed; nothing here knows a
// URL.
func modelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage the local embedding model",
		Long: `Download and verify the embedding model Conduit uses for semantic search.

Models are pinned: each one is tied to an exact file in an exact HuggingFace
repository with an exact SHA-256. Downloads are verified against that hash and
discarded if they do not match, so a corrupted or substituted file can never be
installed.

Without a model, Conduit still works: search falls back to keyword matching
(FTS5). Set embed.provider to "none" to make that the intended behaviour rather
than a degraded one.

Examples:
  conduit model list
  conduit model download
  conduit model download qwen3-embedding-0.6b
  conduit model verify
  conduit model path`,
	}

	cmd.AddCommand(modelListCmd())
	cmd.AddCommand(modelDownloadCmd())
	cmd.AddCommand(modelVerifyCmd())
	cmd.AddCommand(modelPathCmd())

	return cmd
}

// resolveModelID picks the model a command should act on.
//
// An explicit argument wins. Otherwise the configured model is used, falling
// back to the registry default. Ollama model tags are not registry keys, so a
// machine configured for Ollama still resolves to the default rather than
// trying to look up an Ollama tag in the GGUF registry.
func resolveModelID(cfg *config.Config, args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	if cfg != nil && cfg.Embed.Provider == config.EmbedProviderLlamaServer && cfg.Embed.Model != "" {
		return cfg.Embed.Model
	}
	return embed.DefaultModelID
}

// modelListCmd shows the registry and what is on disk.
func modelListCmd() *cobra.Command {
	var jsonOutput bool
	var verify bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the pinned embedding models",
		Long: `Show every model Conduit can download, and which are present locally.

--verify re-hashes each local file against its pin. That reads several hundred
megabytes per model, so it is off by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			active := resolveModelID(cfg, nil)

			type modelRow struct {
				ID           string `json:"id"`
				Dimensions   int    `json:"dimensions"`
				Context      int    `json:"context_tokens"`
				Quantization string `json:"quantization"`
				License      string `json:"license"`
				SizeBytes    int64  `json:"size_bytes"`
				Path         string `json:"path"`
				Present      bool   `json:"present"`
				Verified     bool   `json:"verified,omitempty"`
				Active       bool   `json:"active"`
				Notes        string `json:"notes,omitempty"`
			}

			var rows []modelRow
			for _, spec := range embed.Models() {
				st := embed.StatModel(spec, cfg.DataDir, verify)
				rows = append(rows, modelRow{
					ID:           spec.ID,
					Dimensions:   spec.Dimensions,
					Context:      spec.ContextTokens,
					Quantization: spec.Quantization,
					License:      spec.License,
					SizeBytes:    spec.SizeBytes,
					Path:         st.Path,
					Present:      st.Present,
					Verified:     st.Verified,
					Active:       spec.ID == active,
					Notes:        spec.Notes,
				})
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]interface{}{
					"models":   rows,
					"active":   active,
					"data_dir": cfg.DataDir,
				})
			}

			fmt.Printf("Embedding models (%s)\n", cfg.ModelsDir())
			fmt.Println("═══════════════════════════════════════════════════════")
			for _, r := range rows {
				marker := " "
				if r.Active {
					marker = "*"
				}

				status := "not downloaded"
				switch {
				case r.Present && verify && r.Verified:
					status = "installed, verified"
				case r.Present && verify && !r.Verified:
					status = "installed, CHECKSUM MISMATCH"
				case r.Present:
					status = "installed"
				}

				fmt.Printf("\n%s %s\n", marker, r.ID)
				fmt.Printf("    %d dimensions, %d token context, %s, %s\n",
					r.Dimensions, r.Context, r.Quantization, r.License)
				fmt.Printf("    %s  (%s)\n", status, humanBytes(r.SizeBytes))
			}

			fmt.Println()
			fmt.Println("* = the model this machine is configured to use")
			fmt.Println("Download with: conduit model download [<id>]")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&verify, "verify", false, "Re-hash local files against their pins (slow)")
	return cmd
}

// modelDownloadCmd fetches and verifies a pinned model.
func modelDownloadCmd() *cobra.Command {
	var jsonOutput bool
	var force bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "download [model-id]",
		Short: "Download the embedding model and verify its checksum",
		Long: `Fetch a pinned embedding model into the data directory.

The file is downloaded to a temporary name and only renamed into place once its
SHA-256 matches the registry. A mismatch deletes the download and fails: there
is no flag to install an unverified model.

Re-running is safe. A model that is already present and correct is left alone,
which is why install scripts can call this unconditionally.

With no argument the configured model is used, or the default if none is set.

Examples:
  conduit model download
  conduit model download nomic-embed-text-v1.5
  conduit model download --force        # re-fetch even if already valid`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			modelID := resolveModelID(cfg, args)
			spec, err := embed.LookupModel(modelID)
			if err != nil {
				return err
			}

			if err := cfg.EnsureDirectories(); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}

			// --force means "fetch it again regardless"; the simplest honest
			// implementation is to drop the existing file first.
			if force {
				if err := os.Remove(spec.LocalPath(cfg.DataDir)); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove existing model: %w", err)
				}
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			d := &embed.Downloader{}
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Downloading %s (%s)\n", spec.ID, humanBytes(spec.SizeBytes))
				fmt.Fprintf(os.Stderr, "  from %s\n", spec.DownloadURL())
				fmt.Fprintf(os.Stderr, "  to   %s\n\n", spec.LocalPath(cfg.DataDir))
				d.Progress = newProgressPrinter(os.Stderr)
			}

			res, err := d.Download(ctx, spec, cfg.DataDir)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]interface{}{
					"model":           res.ModelID,
					"path":            res.Path,
					"bytes":           res.Bytes,
					"sha256":          res.SHA256,
					"already_present": res.AlreadyPresent,
					"verified":        true,
				})
			}

			if res.AlreadyPresent {
				fmt.Printf("✓ %s already present and verified\n", res.ModelID)
			} else {
				fmt.Printf("✓ %s downloaded and verified\n", res.ModelID)
			}
			fmt.Printf("  %s (%s)\n", res.Path, humanBytes(res.Bytes))
			fmt.Printf("  sha256 %s\n", res.SHA256)

			if cfg.Embed.Provider == config.EmbedProviderNone {
				fmt.Println()
				fmt.Println("Note: embed.provider is \"none\", so this model will not be used.")
				fmt.Println("Enable it with: conduit config set embed.provider llama-server")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&force, "force", false, "Re-download even if a valid copy exists")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Abort the download after this long (e.g. 30m)")
	return cmd
}

// modelVerifyCmd re-checks a local artifact against its pin.
func modelVerifyCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "verify [model-id]",
		Short: "Verify a downloaded model against its pinned checksum",
		Long: `Re-hash a local model file and compare it to the registry pin.

This reads the whole file. It answers one question: is the GGUF on this machine
the exact artifact Conduit expects, or has it been truncated, corrupted or
replaced.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			modelID := resolveModelID(cfg, args)
			spec, err := embed.LookupModel(modelID)
			if err != nil {
				return err
			}

			verr := embed.VerifyModel(spec, cfg.DataDir)

			if jsonOutput {
				out := map[string]interface{}{
					"model": spec.ID,
					"path":  spec.LocalPath(cfg.DataDir),
					"valid": verr == nil,
				}
				if verr != nil {
					out["error"] = verr.Error()
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
				if verr != nil {
					return exitWith(1)
				}
				return nil
			}

			if verr != nil {
				fmt.Printf("✗ %s\n", verr)
				fmt.Println()
				fmt.Printf("Re-fetch it with: conduit model download %s --force\n", spec.ID)
				return exitWith(1)
			}

			fmt.Printf("✓ %s verified\n", spec.ID)
			fmt.Printf("  %s\n", spec.LocalPath(cfg.DataDir))
			fmt.Printf("  sha256 %s\n", strings.ToLower(spec.SHA256))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

// modelPathCmd prints where a model belongs on this machine.
//
// It exists so scripts do not have to reimplement the data-dir and filename
// conventions, which is exactly the duplication that made v1's installer drift
// out of sync with the binary.
func modelPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path [model-id]",
		Short: "Print the local path of a model artifact",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			spec, err := embed.LookupModel(resolveModelID(cfg, args))
			if err != nil {
				return err
			}
			fmt.Println(spec.LocalPath(cfg.DataDir))
			return nil
		},
	}
	return cmd
}

// newProgressPrinter renders download progress.
//
// Output goes to the supplied writer, which callers point at stderr so that
// --json consumers reading stdout are unaffected. On a terminal the line is
// rewritten in place; when redirected it prints one line per update, because a
// log full of carriage returns is unreadable.
func newProgressPrinter(f *os.File) func(embed.ProgressEvent) {
	tty := isTerminal(f)
	var lastPct int = -1

	return func(ev embed.ProgressEvent) {
		pct := int(ev.Percent())

		if !tty {
			// Throttle hard when not a terminal: every 10% is enough to show a
			// stalled download in CI output without flooding it.
			if !ev.Done && (pct/10 == lastPct/10) {
				return
			}
			lastPct = pct
			fmt.Fprintf(f, "  %3d%%  %s / %s\n", pct, humanBytes(ev.Downloaded), humanBytes(ev.Total))
			return
		}

		fmt.Fprintf(f, "\r  [%s] %3d%%  %s / %s",
			progressBar(ev.Percent(), 28), pct, humanBytes(ev.Downloaded), humanBytes(ev.Total))
		if ev.Done {
			fmt.Fprintln(f)
		}
	}
}

// progressBar renders a fixed-width bar.
func progressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// humanBytes renders a byte count for humans.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
