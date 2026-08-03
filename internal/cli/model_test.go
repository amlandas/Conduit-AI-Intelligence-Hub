package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/embed"
)

// None of these tests download anything. The transfer path is covered
// hermetically in internal/embed; what matters here is that the commands agree
// with the registry about names, paths and status, because those are the
// contracts scripts/install.sh depends on.

func TestModelList(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "model", "list")

	for _, id := range embed.ModelIDs() {
		if !strings.Contains(out, id) {
			t.Errorf("model list omits %q\n%s", id, out)
		}
	}
	if !strings.Contains(out, "not downloaded") {
		t.Errorf("a fresh HOME should report models as absent\n%s", out)
	}
}

func TestModelListJSON(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "model", "list", "--json")

	var payload struct {
		Active  string `json:"active"`
		DataDir string `json:"data_dir"`
		Models  []struct {
			ID        string `json:"id"`
			Present   bool   `json:"present"`
			Active    bool   `json:"active"`
			Path      string `json:"path"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse --json output: %v\n%s", err, out)
	}

	if len(payload.Models) != len(embed.ModelIDs()) {
		t.Fatalf("got %d models, want %d", len(payload.Models), len(embed.ModelIDs()))
	}
	if payload.Active != embed.DefaultModelID {
		t.Errorf("active = %q, want the registry default %q", payload.Active, embed.DefaultModelID)
	}

	var activeCount int
	for _, m := range payload.Models {
		if m.Active {
			activeCount++
		}
		if m.Present {
			t.Errorf("%s reported present in a fresh HOME", m.ID)
		}
		if m.SizeBytes <= 0 {
			t.Errorf("%s has no pinned size", m.ID)
		}
		if !strings.HasPrefix(m.Path, payload.DataDir) {
			t.Errorf("%s path %q is outside the data dir %q", m.ID, m.Path, payload.DataDir)
		}
	}
	if activeCount != 1 {
		t.Errorf("%d models marked active, want exactly 1", activeCount)
	}
}

// The path the CLI reports must be the path the downloader writes and the
// sidecar reads. If these ever disagree, install scripts silently break.
func TestModelPathMatchesRegistry(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "model", "path")
	got := strings.TrimSpace(out)

	spec, err := embed.LookupModel(embed.DefaultModelID)
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}
	want := spec.LocalPath(filepath.Join(env.home, ".conduit"))
	if got != want {
		t.Errorf("model path = %q, want %q", got, want)
	}

	// And it must resolve per-model, not just for the default.
	out = env.mustRun(t, "model", "path", embed.ModelMxbaiEmbedLargeV1)
	got = strings.TrimSpace(out)
	if !strings.HasSuffix(got, ".gguf") {
		t.Errorf("model path for an explicit id = %q, want a .gguf", got)
	}
	if strings.Contains(got, spec.HFFile) {
		t.Errorf("explicit id resolved to the default model's file: %q", got)
	}
}

func TestModelPathRejectsUnknownModel(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "model", "path", "definitely-not-a-model")
	if code == 0 {
		t.Fatalf("an unknown model id should fail\n%s", out)
	}
	if !strings.Contains(out, "unknown model") {
		t.Errorf("error should say the model is unknown:\n%s", out)
	}
}

func TestModelVerifyMissingModelExitsNonZero(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "model", "verify")
	if code != 1 {
		t.Fatalf("verify with no model exited %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "conduit model download") {
		t.Errorf("verify should name the command that fixes it:\n%s", out)
	}
}

// A file that is present but wrong must fail verification, not pass because
// something exists at the path.
func TestModelVerifyDetectsCorruptFile(t *testing.T) {
	env := newTestEnv(t)

	spec, err := embed.LookupModel(embed.DefaultModelID)
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}
	dest := spec.LocalPath(filepath.Join(env.home, ".conduit"))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, []byte("not a real gguf"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, code := env.run(t, "model", "verify", "--json")
	if code != 1 {
		t.Fatalf("verify of a corrupt file exited %d, want 1\n%s", code, out)
	}

	var payload struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse --json: %v\n%s", err, out)
	}
	if payload.Valid {
		t.Error("a corrupt file was reported valid")
	}
	if payload.Error == "" {
		t.Error("no error reported for a corrupt file")
	}
}

func TestModelDownloadRejectsUnknownModel(t *testing.T) {
	env := newTestEnv(t)

	// No network is touched: the registry lookup fails first.
	out, code := env.run(t, "model", "download", "not-in-the-registry")
	if code == 0 {
		t.Fatalf("downloading an unpinned model should fail\n%s", out)
	}
	if !strings.Contains(out, "unknown model") {
		t.Errorf("error should say the model is unknown:\n%s", out)
	}
}

func TestResolveModelID(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		args []string
		want string
	}{
		{
			name: "explicit argument wins",
			cfg: &config.Config{Embed: config.EmbedConfig{
				Provider: config.EmbedProviderLlamaServer,
				Model:    embed.ModelMxbaiEmbedLargeV1,
			}},
			args: []string{embed.ModelQwen3Embedding06B},
			want: embed.ModelQwen3Embedding06B,
		},
		{
			name: "configured model is used",
			cfg: &config.Config{Embed: config.EmbedConfig{
				Provider: config.EmbedProviderLlamaServer,
				Model:    embed.ModelMxbaiEmbedLargeV1,
			}},
			want: embed.ModelMxbaiEmbedLargeV1,
		},
		{
			name: "unset falls back to the registry default",
			cfg: &config.Config{Embed: config.EmbedConfig{
				Provider: config.EmbedProviderLlamaServer,
			}},
			want: embed.DefaultModelID,
		},
		{
			// An Ollama tag is not a GGUF registry key, so it must not be
			// looked up as one.
			name: "ollama tags are not registry keys",
			cfg: &config.Config{Embed: config.EmbedConfig{
				Provider: config.EmbedProviderOllama,
				Model:    "nomic-embed-text:latest",
			}},
			want: embed.DefaultModelID,
		},
		{
			name: "nil config falls back to the default",
			want: embed.DefaultModelID,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveModelID(c.cfg, c.args); got != c.want {
				t.Errorf("resolveModelID = %q, want %q", got, c.want)
			}
		})
	}
}

// doctor must tell a fresh machine that the model is missing, since that is the
// single most likely reason semantic search does not work.
func TestDoctorReportsMissingModel(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("CONDUIT_EMBED_PROVIDER", "llama-server")

	out, _ := env.run(t, "doctor")
	if !strings.Contains(out, "embedding model") {
		t.Errorf("doctor has no embedding model check:\n%s", out)
	}
	if !strings.Contains(out, "conduit model download") {
		t.Errorf("doctor does not name the download command:\n%s", out)
	}
}

// With embeddings off, the model check must be a skip rather than a failure:
// lexical-only is a supported configuration, not a broken one.
func TestDoctorSkipsModelCheckWhenEmbeddingsDisabled(t *testing.T) {
	env := newTestEnv(t) // sets CONDUIT_EMBED_PROVIDER=none

	out, _ := env.run(t, "doctor", "--json")

	var payload struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse doctor --json: %v\n%s", err, out)
	}

	for _, c := range payload.Checks {
		if c.Name == "embedding model" {
			if c.Status != "skip" {
				t.Errorf("embedding model check status = %q, want skip", c.Status)
			}
			return
		}
	}
	t.Error("doctor --json has no embedding model check")
}

// --prefix removes one install; --all removes the shared data directory, which
// is outside every prefix. Accepting both would mean guessing which half the
// user meant, with a knowledge base riding on the guess.
func TestUninstallRejectsPrefixWithAll(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "uninstall", "--all", "--prefix", t.TempDir(), "--force")
	if code == 0 {
		t.Fatalf("--all --prefix was accepted\n%s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("error should say the flags are mutually exclusive:\n%s", out)
	}

	// Each flag alone must still work, or the guard has broken the feature it
	// was meant to protect.
	if _, code := env.run(t, "uninstall", "--keep-data", "--prefix", t.TempDir(), "--force"); code != 0 {
		t.Errorf("--prefix alone exited %d", code)
	}
}

func TestProgressBar(t *testing.T) {
	cases := []struct {
		pct   float64
		width int
	}{{0, 10}, {50, 10}, {100, 10}, {150, 10}, {-5, 10}}
	for _, c := range cases {
		got := progressBar(c.pct, c.width)
		if n := len([]rune(got)); n != c.width {
			t.Errorf("progressBar(%v, %d) has %d runes, want %d", c.pct, c.width, n, c.width)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{274290560, "261.6 MB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
