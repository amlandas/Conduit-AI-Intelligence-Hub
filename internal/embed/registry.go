package embed

import (
	"fmt"
	"path/filepath"
	"sort"
)

// ModelSpec pins one embedding model to an exact, verifiable artifact.
//
// SHA256 is the content hash of the GGUF file. It is taken from the
// HuggingFace LFS object id, which for these repos IS the SHA-256 of the file,
// so it can be verified after download without trusting the transport. WP-3.3's
// downloader must refuse any file whose hash does not match.
//
// Pooling and the prefix fields are not cosmetic: llama.cpp applies whatever
// pooling it is told to, and using the wrong mode (or omitting a model's
// required instruction prefix) degrades retrieval quality silently, with no
// error and no obvious signal in the vectors themselves.
type ModelSpec struct {
	// ID is the registry key, e.g. "nomic-embed-text-v1.5".
	ID string

	// Dimensions is the vector width the model produces.
	Dimensions int

	// ContextTokens is the model's maximum sequence length.
	ContextTokens int

	// Pooling is the llama.cpp pooling mode: "mean", "cls" or "last".
	Pooling string

	// DocPrefix is prepended to documents at index time. May be empty.
	DocPrefix string

	// QueryPrefix is prepended to queries at search time. May be empty.
	QueryPrefix string

	// InputSuffix is appended to EVERY input, document and query alike.
	//
	// This exists for Qwen3-Embedding, whose published GGUF does not append an
	// EOS token. Because it pools the last token, the missing EOS silently
	// costs a large chunk of retrieval accuracy -- llama.cpp issue #14234
	// measured roughly 20% below reference. See BAKEOFF-WP-2.2.md.
	InputSuffix string

	// HFRepo is the HuggingFace repository, e.g. "nomic-ai/nomic-embed-text-v1.5-GGUF".
	HFRepo string

	// HFFile is the GGUF filename within the repository.
	HFFile string

	// SHA256 is the lowercase hex SHA-256 of the GGUF file.
	SHA256 string

	// SizeBytes is the exact file size, used for progress reporting and as a
	// cheap pre-check before hashing.
	SizeBytes int64

	// Quantization labels the artifact, e.g. "F16" or "Q8_0".
	Quantization string

	// License is the model's SPDX-ish license identifier.
	License string

	// Notes records model-specific caveats worth surfacing to operators.
	Notes string
}

// DownloadURL returns the direct HuggingFace download URL for the artifact.
func (s ModelSpec) DownloadURL() string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", s.HFRepo, s.HFFile)
}

// Filename returns the local filename for the artifact.
func (s ModelSpec) Filename() string { return s.HFFile }

// LocalPath returns where the artifact belongs under the conduit data dir.
func (s ModelSpec) LocalPath(dataDir string) string {
	return filepath.Join(dataDir, "models", s.HFFile)
}

// Validate checks that the spec is internally complete.
func (s ModelSpec) Validate() error {
	switch {
	case s.ID == "":
		return fmt.Errorf("embed: model spec missing ID")
	case s.Dimensions <= 0:
		return fmt.Errorf("embed: model %s has invalid dimensions %d", s.ID, s.Dimensions)
	case s.HFRepo == "" || s.HFFile == "":
		return fmt.Errorf("embed: model %s missing HuggingFace coordinates", s.ID)
	case len(s.SHA256) != 64:
		return fmt.Errorf("embed: model %s has invalid SHA256 %q (want 64 hex chars)", s.ID, s.SHA256)
	case s.SizeBytes <= 0:
		return fmt.Errorf("embed: model %s has invalid size %d", s.ID, s.SizeBytes)
	}
	switch s.Pooling {
	case "mean", "cls", "last":
	default:
		return fmt.Errorf("embed: model %s has invalid pooling %q", s.ID, s.Pooling)
	}
	return nil
}

// Model registry keys.
const (
	// ModelNomicEmbedTextV15 is the recommended default: smallest artifact,
	// smallest RAM footprint, and the only candidate with no open llama.cpp
	// quality defect against it.
	ModelNomicEmbedTextV15 = "nomic-embed-text-v1.5"

	// ModelQwen3Embedding06B is a higher-ceiling but higher-risk candidate.
	ModelQwen3Embedding06B = "qwen3-embedding-0.6b"

	// ModelMxbaiEmbedLargeV1 is the bake-off control.
	ModelMxbaiEmbedLargeV1 = "mxbai-embed-large-v1"
)

// DefaultModelID is the model Conduit uses when none is configured.
const DefaultModelID = ModelNomicEmbedTextV15

// registry is the pinned set of embedding models Conduit will download and run.
//
// Hashes and sizes were read from the HuggingFace model tree API on 2026-08-02
// and are exact. Adding a model here without a verified hash is a bug.
var registry = map[string]ModelSpec{
	ModelNomicEmbedTextV15: {
		ID:            ModelNomicEmbedTextV15,
		Dimensions:    768,
		ContextTokens: 2048,
		Pooling:       "mean",
		// nomic-embed-text is trained with asymmetric task prefixes. Omitting
		// them costs measurable retrieval accuracy, so they are part of the spec.
		DocPrefix:    "search_document: ",
		QueryPrefix:  "search_query: ",
		HFRepo:       "nomic-ai/nomic-embed-text-v1.5-GGUF",
		HFFile:       "nomic-embed-text-v1.5.f16.gguf",
		SHA256:       "f7af6f66802f4df86eda10fe9bbcfc75c39562bed48ef6ace719a251cf1c2fdb",
		SizeBytes:    274290560,
		Quantization: "F16",
		License:      "Apache-2.0",
		Notes: "137M params, 768-dim, Matryoshka-capable. Requires search_document:/search_query: prefixes. " +
			"F16 chosen over Q8_0 because the file is only 274MB and quantisation noise is proportionally " +
			"larger on a model this small.",
	},
	ModelQwen3Embedding06B: {
		ID:            ModelQwen3Embedding06B,
		Dimensions:    1024,
		ContextTokens: 32768,
		// Qwen3-Embedding is a causal LM that pools the FINAL token. Mean
		// pooling on this model produces plausible-looking but materially
		// worse vectors -- this is the documented llama.cpp footgun.
		Pooling:     "last",
		DocPrefix:   "",
		QueryPrefix: "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: ",
		// llama.cpp #14234: the published GGUF does not append EOS, and this
		// model pools the last token. Without the explicit suffix, retrieval
		// accuracy drops sharply. Verified still required as of Aug 2026.
		InputSuffix:  "<|endoftext|>",
		HFRepo:       "Qwen/Qwen3-Embedding-0.6B-GGUF",
		HFFile:       "Qwen3-Embedding-0.6B-Q8_0.gguf",
		SHA256:       "06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439",
		SizeBytes:    639150592,
		Quantization: "Q8_0",
		License:      "Apache-2.0",
		Notes: "0.6B params, 1024-dim. Best raw MTEB of the three, but the highest llama.cpp risk: the " +
			"official GGUF is a stale 2025-07-14 conversion, requires --pooling last, and needs an EOS " +
			"token appended to every input. See BAKEOFF-WP-2.2.md before promoting.",
	},
	ModelMxbaiEmbedLargeV1: {
		ID:            ModelMxbaiEmbedLargeV1,
		Dimensions:    1024,
		ContextTokens: 512,
		Pooling:       "cls",
		DocPrefix:     "",
		QueryPrefix:   "Represent this sentence for searching relevant passages: ",
		HFRepo:        "ChristianAzinn/mxbai-embed-large-v1-gguf",
		HFFile:        "mxbai-embed-large-v1_fp16.gguf",
		SHA256:        "819c2adf5ce6df2b6bd2ae4ca90d2a69f060afeb438d0c171db57daa02e39c3d",
		SizeBytes:     669603712,
		Quantization:  "F16",
		License:       "Apache-2.0",
		Notes: "335M params, 1024-dim BERT-style encoder, CLS pooling. Only a 512-token context, which is " +
			"restrictive for Conduit's ~1000-char chunks. Retained as the bake-off control.",
	},
}

// LookupModel returns the pinned spec for id.
func LookupModel(id string) (ModelSpec, error) {
	spec, ok := registry[id]
	if !ok {
		return ModelSpec{}, fmt.Errorf("embed: unknown model %q (known: %v)", id, ModelIDs())
	}
	return spec, nil
}

// ModelIDs returns the registered model ids in stable sorted order.
func ModelIDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Models returns every pinned spec in stable sorted order.
func Models() []ModelSpec {
	ids := ModelIDs()
	out := make([]ModelSpec, 0, len(ids))
	for _, id := range ids {
		out = append(out, registry[id])
	}
	return out
}

// ManagerConfigForModel builds a ManagerConfig pre-filled from the pinned spec,
// so callers cannot accidentally run a model with the wrong pooling or prefixes.
//
// modelPath may be empty, in which case the conventional location under
// dataDir is used.
func ManagerConfigForModel(dataDir, modelID, modelPath string) (ManagerConfig, error) {
	spec, err := LookupModel(modelID)
	if err != nil {
		return ManagerConfig{}, err
	}
	if modelPath == "" {
		modelPath = spec.LocalPath(dataDir)
	}

	// Cap the sidecar context at the model's real limit; asking llama-server
	// for more than the model supports fails at load time.
	ctxTokens := spec.ContextTokens
	if ctxTokens > 8192 {
		// Conduit chunks are ~1000 characters; a huge context only wastes RAM
		// because -b/-ub are raised to match it.
		ctxTokens = 8192
	}

	return ManagerConfig{
		DataDir:     dataDir,
		ModelPath:   modelPath,
		ModelID:     spec.ID,
		Dimensions:  spec.Dimensions,
		ContextSize: ctxTokens,
		Pooling:     spec.Pooling,
		QueryPrefix: spec.QueryPrefix,
		DocPrefix:   spec.DocPrefix,
		InputSuffix: spec.InputSuffix,
	}, nil
}
