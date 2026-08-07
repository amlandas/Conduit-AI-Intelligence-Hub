package embed

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"sort"
	"strings"
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

	// Aliases lists the other strings this exact model is known by, so that the
	// SAME weights reached through a different provider resolve to the SAME
	// canonical identity.
	//
	// This is not cosmetic bookkeeping. Ollama serves nomic-embed-text-v1.5
	// under the tag "nomic-embed-text"; the pinned GGUF is a file called
	// "nomic-embed-text-v1.5.f16.gguf". A naive string comparison between the
	// model that built a knowledge base's vectors and the model configured now
	// would call a provider switch between those three spellings a model
	// change, and disabling semantic search on a perfectly healthy knowledge
	// base is a worse failure than the one the check exists to catch.
	//
	// Entries are matched after normalisation (see normalizeModelID): case,
	// surrounding whitespace, an Ollama ":latest" tag, a directory prefix and a
	// ".gguf" suffix are all insignificant. Every alias must name the same
	// weights at the same width -- an alias is an assertion that two vectors
	// produced through these two names are directly comparable.
	Aliases []string

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

	// HFFile is joined onto the data directory to form the download
	// destination, so a separator or a parent reference in it would write
	// outside <data-dir>/models. Nothing in the pinned registry does that
	// today; this is here so that adding an entry cannot make it possible.
	if s.HFFile != filepath.Base(s.HFFile) ||
		s.HFFile == "." || s.HFFile == ".." ||
		strings.ContainsAny(s.HFFile, `/\`) {
		return fmt.Errorf("embed: model %s has unsafe filename %q (must be a bare filename)", s.ID, s.HFFile)
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
		ID: ModelNomicEmbedTextV15,
		// "nomic-embed-text" is the Ollama library name, whose `latest` tag is
		// v1.5 -- the same 768-dim weights this GGUF holds. The bare filename is
		// listed so a model path can be canonicalised too.
		Aliases: []string{
			"nomic-embed-text",
			"nomic-embed-text:v1.5",
			"nomic-embed-text-v1.5.f16.gguf",
			"nomic-ai/nomic-embed-text-v1.5",
		},
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
		ID: ModelQwen3Embedding06B,
		Aliases: []string{
			"qwen3-embedding:0.6b",
			"Qwen3-Embedding-0.6B",
			"Qwen3-Embedding-0.6B-Q8_0.gguf",
			"Qwen/Qwen3-Embedding-0.6B",
		},
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
		ID: ModelMxbaiEmbedLargeV1,
		Aliases: []string{
			"mxbai-embed-large",
			"mxbai-embed-large-v1_fp16.gguf",
			"mixedbread-ai/mxbai-embed-large-v1",
		},
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

// aliasIndex maps every normalised spelling of a known model onto its registry
// key. It is built once at init so that canonicalisation is a map lookup rather
// than a scan, and so that a duplicate alias is a startup panic rather than a
// silently ambiguous resolution.
var aliasIndex = func() map[string]string {
	idx := make(map[string]string, len(registry)*3)
	add := func(name, canonical string) {
		key := normalizeModelID(name)
		if key == "" {
			return
		}
		if prev, dup := idx[key]; dup && prev != canonical {
			// Two models claiming the same spelling would make canonicalisation
			// depend on map iteration order, which is exactly the kind of
			// non-determinism that turns a correctness guard into a coin flip.
			panic(fmt.Sprintf("embed: model alias %q claimed by both %q and %q", name, prev, canonical))
		}
		idx[key] = canonical
	}
	for id, spec := range registry {
		add(id, id)
		for _, alias := range spec.Aliases {
			add(alias, id)
		}
	}
	return idx
}()

// normalizeModelID reduces a model identifier to the form aliases are matched
// in: lower case, no surrounding whitespace, no directory prefix, no ".gguf"
// suffix and no Ollama ":latest" tag.
//
// Only insignificant decoration is stripped. A meaningful version tag such as
// ":0.6b" or "-v1.5" survives, because two models that differ there are two
// different models and must not collapse onto one identity.
func normalizeModelID(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if s == "" {
		return ""
	}
	s = strings.TrimSuffix(s, ".gguf")
	s = strings.TrimSuffix(s, ":latest")
	return s
}

// CanonicalModelID resolves an observed embedding-model identifier onto the
// registry's canonical id.
//
// It returns (canonical, true) when the model is one Conduit knows, and
// (normalised, false) when it is not -- a user-supplied Ollama model, say. An
// unresolved identifier is still usable as an identity: it just cannot be
// PROVEN equal to a differently spelled one, and callers must treat a
// difference involving an unresolved id as "unknown", never as "changed".
func CanonicalModelID(observed string) (string, bool) {
	norm := normalizeModelID(observed)
	if norm == "" {
		return "", false
	}
	if canonical, ok := aliasIndex[norm]; ok {
		return canonical, true
	}
	// Fall back to the bare file/tag name, so that an absolute model_path such
	// as /models/nomic-embed-text-v1.5.f16.gguf resolves the same as the
	// filename alias does.
	if i := strings.LastIndexAny(norm, `/\`); i >= 0 && i < len(norm)-1 {
		if canonical, ok := aliasIndex[norm[i+1:]]; ok {
			return canonical, true
		}
	}
	return norm, false
}

// PrefixSchemeNone is the scheme identifier for "no decoration applied".
const PrefixSchemeNone = "none"

// PrefixSchemeID returns a short, stable identifier for the instruction
// decoration a provider applies to its inputs.
//
// The decoration is part of what a stored vector MEANS. nomic-embed-text is
// trained asymmetrically: documents carry "search_document: " and queries
// "search_query: ". Conduit's llama-server provider applies those prefixes from
// the registry; the Ollama provider is wired without them (see
// kbservice.newEmbedder). Vectors built one way and queried the other are still
// in the same space, so retrieval degrades rather than collapses -- which is
// precisely why it is worth recording and reporting instead of guessing.
func PrefixSchemeID(docPrefix, queryPrefix, inputSuffix string) string {
	if docPrefix == "" && queryPrefix == "" && inputSuffix == "" {
		return PrefixSchemeNone
	}
	h := fnv.New64a()
	// The separators keep ("ab", "", "") from colliding with ("a", "b", "").
	_, _ = h.Write([]byte(docPrefix))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(queryPrefix))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(inputSuffix))
	return fmt.Sprintf("%08x", uint32(h.Sum64()>>32))
}

// PrefixScheme returns the scheme identifier for this model's pinned
// decoration, i.e. what a provider configured from the registry will apply.
func (s ModelSpec) PrefixScheme() string {
	return PrefixSchemeID(s.DocPrefix, s.QueryPrefix, s.InputSuffix)
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
