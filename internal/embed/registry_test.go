package embed

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistry_AllSpecsAreValid is the guard against adding a model without a
// verified hash, a real size, or a valid pooling mode.
func TestRegistry_AllSpecsAreValid(t *testing.T) {
	t.Parallel()

	specs := Models()
	if len(specs) == 0 {
		t.Fatal("registry is empty")
	}

	for _, spec := range specs {
		spec := spec
		t.Run(spec.ID, func(t *testing.T) {
			t.Parallel()
			if err := spec.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if _, err := hex.DecodeString(spec.SHA256); err != nil {
				t.Errorf("SHA256 %q is not valid hex: %v", spec.SHA256, err)
			}
			if strings.ToLower(spec.SHA256) != spec.SHA256 {
				t.Errorf("SHA256 %q should be lowercase", spec.SHA256)
			}
			if spec.License == "" {
				t.Error("spec has no license")
			}
			if !strings.HasSuffix(spec.HFFile, ".gguf") {
				t.Errorf("HFFile %q is not a .gguf", spec.HFFile)
			}
			if spec.ContextTokens <= 0 {
				t.Errorf("ContextTokens = %d", spec.ContextTokens)
			}
		})
	}
}

func TestRegistry_DownloadURL(t *testing.T) {
	t.Parallel()

	spec, err := LookupModel(ModelNomicEmbedTextV15)
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}
	want := "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.f16.gguf"
	if got := spec.DownloadURL(); got != want {
		t.Errorf("DownloadURL() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(spec.DownloadURL(), "https://") {
		t.Error("download URL must be https")
	}
}

func TestRegistry_LocalPath(t *testing.T) {
	t.Parallel()

	spec, _ := LookupModel(ModelNomicEmbedTextV15)
	got := spec.LocalPath("/data/conduit")
	want := filepath.Join("/data/conduit", "models", "nomic-embed-text-v1.5.f16.gguf")
	if got != want {
		t.Errorf("LocalPath = %q, want %q", got, want)
	}
	if spec.Filename() != "nomic-embed-text-v1.5.f16.gguf" {
		t.Errorf("Filename = %q", spec.Filename())
	}
}

func TestRegistry_LookupUnknownModel(t *testing.T) {
	t.Parallel()

	_, err := LookupModel("no-such-model")
	if err == nil {
		t.Fatal("expected an error for an unknown model")
	}
	// The error should help the operator by listing what is available.
	if !strings.Contains(err.Error(), ModelNomicEmbedTextV15) {
		t.Errorf("error does not list known models: %v", err)
	}
}

func TestRegistry_ModelIDsAreSorted(t *testing.T) {
	t.Parallel()

	ids := ModelIDs()
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("ModelIDs not sorted: %v", ids)
		}
	}
	if len(ids) != len(Models()) {
		t.Error("ModelIDs and Models disagree on length")
	}
}

func TestRegistry_DefaultModelExists(t *testing.T) {
	t.Parallel()

	if _, err := LookupModel(DefaultModelID); err != nil {
		t.Fatalf("DefaultModelID %q is not in the registry: %v", DefaultModelID, err)
	}
}

// TestRegistry_KnownQualityFootguns pins the model-specific settings that
// silently wreck retrieval if they drift. Each assertion maps to a documented
// llama.cpp issue; see BAKEOFF-WP-2.2.md.
func TestRegistry_KnownQualityFootguns(t *testing.T) {
	t.Parallel()

	nomic, _ := LookupModel(ModelNomicEmbedTextV15)
	if nomic.DocPrefix != "search_document: " || nomic.QueryPrefix != "search_query: " {
		t.Errorf("nomic prefixes are wrong: doc=%q query=%q", nomic.DocPrefix, nomic.QueryPrefix)
	}
	if nomic.Pooling != "mean" {
		t.Errorf("nomic pooling = %q, want mean", nomic.Pooling)
	}
	if nomic.Dimensions != 768 {
		t.Errorf("nomic dimensions = %d, want 768", nomic.Dimensions)
	}

	qwen, _ := LookupModel(ModelQwen3Embedding06B)
	if qwen.Pooling != "last" {
		t.Errorf("qwen pooling = %q, want last (it pools the final token)", qwen.Pooling)
	}
	// llama.cpp #14234: without the EOS suffix, retrieval accuracy collapses.
	if qwen.InputSuffix != "<|endoftext|>" {
		t.Errorf("qwen InputSuffix = %q, want the EOS token", qwen.InputSuffix)
	}
	if !strings.HasPrefix(qwen.QueryPrefix, "Instruct:") {
		t.Errorf("qwen query prefix should use the Instruct format, got %q", qwen.QueryPrefix)
	}

	mxbai, _ := LookupModel(ModelMxbaiEmbedLargeV1)
	if mxbai.Pooling != "cls" {
		t.Errorf("mxbai pooling = %q, want cls", mxbai.Pooling)
	}
	if mxbai.ContextTokens != 512 {
		t.Errorf("mxbai context = %d, want 512 (hard model limit)", mxbai.ContextTokens)
	}
}

func TestManagerConfigForModel(t *testing.T) {
	t.Parallel()

	cfg, err := ManagerConfigForModel("/data", ModelNomicEmbedTextV15, "")
	if err != nil {
		t.Fatalf("ManagerConfigForModel: %v", err)
	}
	if cfg.Pooling != "mean" {
		t.Errorf("Pooling = %q, want mean", cfg.Pooling)
	}
	if cfg.Dimensions != 768 {
		t.Errorf("Dimensions = %d, want 768", cfg.Dimensions)
	}
	if cfg.DocPrefix != "search_document: " {
		t.Errorf("DocPrefix = %q", cfg.DocPrefix)
	}
	wantPath := filepath.Join("/data", "models", "nomic-embed-text-v1.5.f16.gguf")
	if cfg.ModelPath != wantPath {
		t.Errorf("ModelPath = %q, want %q", cfg.ModelPath, wantPath)
	}

	// An explicit path must win over the conventional location.
	cfg2, err := ManagerConfigForModel("/data", ModelNomicEmbedTextV15, "/custom/model.gguf")
	if err != nil {
		t.Fatalf("ManagerConfigForModel: %v", err)
	}
	if cfg2.ModelPath != "/custom/model.gguf" {
		t.Errorf("explicit ModelPath ignored: %q", cfg2.ModelPath)
	}

	// Qwen declares a 32k context; the manager must cap it so -b/-ub, which
	// track the context size, do not balloon RAM.
	cfgQwen, err := ManagerConfigForModel("/data", ModelQwen3Embedding06B, "")
	if err != nil {
		t.Fatalf("ManagerConfigForModel: %v", err)
	}
	if cfgQwen.ContextSize > 8192 {
		t.Errorf("ContextSize = %d, want it capped at 8192", cfgQwen.ContextSize)
	}
	if cfgQwen.InputSuffix != "<|endoftext|>" {
		t.Errorf("InputSuffix not carried through: %q", cfgQwen.InputSuffix)
	}

	if _, err := ManagerConfigForModel("/data", "bogus", ""); err == nil {
		t.Error("expected an error for an unknown model")
	}
}

func TestModelSpec_ValidateRejectsBadSpecs(t *testing.T) {
	t.Parallel()

	good := ModelSpec{
		ID: "x", Dimensions: 8, HFRepo: "r", HFFile: "f.gguf",
		SHA256: strings.Repeat("a", 64), SizeBytes: 10, Pooling: "mean",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	tests := []struct {
		name  string
		morph func(*ModelSpec)
	}{
		{"no id", func(s *ModelSpec) { s.ID = "" }},
		{"bad dimensions", func(s *ModelSpec) { s.Dimensions = 0 }},
		{"no repo", func(s *ModelSpec) { s.HFRepo = "" }},
		{"no file", func(s *ModelSpec) { s.HFFile = "" }},
		{"short hash", func(s *ModelSpec) { s.SHA256 = "abc" }},
		{"no size", func(s *ModelSpec) { s.SizeBytes = 0 }},
		{"bad pooling", func(s *ModelSpec) { s.Pooling = "sum" }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := good
			tc.morph(&spec)
			if err := spec.Validate(); err == nil {
				t.Errorf("Validate accepted an invalid spec (%s)", tc.name)
			}
		})
	}
}
