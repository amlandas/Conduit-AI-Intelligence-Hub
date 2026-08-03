package kb

import (
	"context"
	"strings"
	"testing"
)

func TestPatternExtractorIsLLMProvider(t *testing.T) {
	var _ LLMProvider = NewPatternExtractor(PatternExtractorConfig{})
}

// TestPatternExtractorMakesNoNetworkCall is the property that justifies making
// this the default: it must be usable with no daemon, no model and no context
// deadline slack. A cancelled context proves nothing is waiting on I/O -- the
// call returns immediately rather than blocking on a dial.
func TestPatternExtractorMakesNoNetworkCall(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})

	if !p.IsAvailable(context.Background()) {
		t.Error("pattern extractor reports unavailable; it has nothing to depend on")
	}
	if p.Name() != "pattern" {
		t.Errorf("name = %q, want pattern", p.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.ExtractEntities(ctx, &ExtractionRequest{Content: "Kubernetes and Docker."}); err != context.Canceled {
		t.Errorf("cancelled context = %v, want context.Canceled", err)
	}
}

func TestPatternExtractorExtractsEntities(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})

	content := `# Container Orchestration

Kubernetes manages containers across a cluster. Kubernetes uses etcd for state.
Docker builds container images. The HTTP API is exposed by Kubernetes.`

	resp, err := p.ExtractEntities(context.Background(), &ExtractionRequest{
		ChunkID:    "chunk1",
		DocumentID: "doc1",
		Content:    content,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	names := make(map[string]ExtractedEntity)
	for _, e := range resp.Entities {
		names[e.Name] = e
	}

	for _, want := range []string{"Kubernetes", "Docker"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing entity %q; got %v", want, entityNames(resp.Entities))
		}
	}

	// The heading becomes a section entity.
	if e, ok := names["Container Orchestration"]; !ok {
		t.Errorf("heading not extracted; got %v", entityNames(resp.Entities))
	} else if e.Type != string(EntityTypeSection) {
		t.Errorf("heading type = %q, want section", e.Type)
	}

	// Repeated mentions must outrank single mentions.
	k := names["Kubernetes"]
	d := names["Docker"]
	if k.Confidence <= d.Confidence {
		t.Errorf("Kubernetes (3 mentions, %.2f) should outrank Docker (1 mention, %.2f)",
			k.Confidence, d.Confidence)
	}

	// Pattern matching is never fully certain.
	for _, e := range resp.Entities {
		if e.Confidence > 0.9 {
			t.Errorf("entity %q scored %.2f; pattern matching must stay below 1.0", e.Name, e.Confidence)
		}
	}

	if resp.Model != "pattern" {
		t.Errorf("model = %q, want pattern", resp.Model)
	}
}

// TestPatternExtractorEmitsOnlyJustifiablePredicates locks in the honesty
// constraint: co-occurrence cannot justify `implements` or `depends_on`.
func TestPatternExtractorEmitsOnlyJustifiablePredicates(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})

	resp, err := p.ExtractEntities(context.Background(), &ExtractionRequest{
		Content: `## Deployment

Kubernetes implements the Container Runtime Interface for Docker.`,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	allowed := map[string]bool{
		string(RelationRelatesTo): true,
		string(RelationContains):  true,
	}
	for _, r := range resp.Relations {
		if !allowed[r.Predicate] {
			t.Errorf("predicate %q is not justifiable from co-occurrence", r.Predicate)
		}
	}
	if len(resp.Relations) == 0 {
		t.Error("expected at least one relation from co-occurring entities")
	}
}

func TestPatternExtractorIsDeterministic(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})
	content := `# Security

TLS protects traffic. TLS and OAuth secure the API. OAuth issues tokens.`

	req := func() *ExtractionRequest {
		return &ExtractionRequest{ChunkID: "c1", DocumentID: "d1", Content: content}
	}

	first, err := p.ExtractEntities(context.Background(), req())
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := p.ExtractEntities(context.Background(), req())
		if err != nil {
			t.Fatalf("extract %d: %v", i, err)
		}
		if strings.Join(entityNames(first.Entities), ",") != strings.Join(entityNames(next.Entities), ",") {
			t.Fatalf("entity order changed between runs:\n%v\n%v",
				entityNames(first.Entities), entityNames(next.Entities))
		}
		if len(first.Relations) != len(next.Relations) {
			t.Fatalf("relation count changed between runs: %d vs %d",
				len(first.Relations), len(next.Relations))
		}
		for j := range first.Relations {
			if first.Relations[j] != next.Relations[j] {
				t.Fatalf("relation %d changed between runs:\n%+v\n%+v",
					j, first.Relations[j], next.Relations[j])
			}
		}
	}
}

func TestPatternExtractorRespectsLimits(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})

	var sb strings.Builder
	sb.WriteString("# Big\n\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("Alpha")
		sb.WriteByte(byte('A' + i%26))
		sb.WriteString(" relates to Beta")
		sb.WriteByte(byte('A' + i%26))
		sb.WriteString(". ")
	}

	resp, err := p.ExtractEntities(context.Background(), &ExtractionRequest{
		Content:      sb.String(),
		MaxEntities:  5,
		MaxRelations: 3,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(resp.Entities) > 5 {
		t.Errorf("got %d entities, want at most 5", len(resp.Entities))
	}
	if len(resp.Relations) > 3 {
		t.Errorf("got %d relations, want at most 3", len(resp.Relations))
	}
}

func TestPatternExtractorRejectsEmptyContent(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})
	if _, err := p.ExtractEntities(context.Background(), &ExtractionRequest{Content: ""}); err == nil {
		t.Error("empty content accepted")
	}
}

// TestPatternExtractorConfidenceThreshold verifies the caller's floor is honored
// the same way an LLM provider honors it.
func TestPatternExtractorConfidenceThreshold(t *testing.T) {
	p := NewPatternExtractor(PatternExtractorConfig{})

	resp, err := p.ExtractEntities(context.Background(), &ExtractionRequest{
		Content:             "Kubernetes and Docker are mentioned once each.",
		ConfidenceThreshold: 0.95,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, e := range resp.Entities {
		if e.Confidence < 0.95 {
			t.Errorf("entity %q at %.2f survived a 0.95 threshold", e.Name, e.Confidence)
		}
	}
}

// TestProviderFactoryDefaultsToPattern proves the default enabled path never
// constructs an LLM client.
func TestProviderFactoryDefaultsToPattern(t *testing.T) {
	f := NewProviderFactory()

	cfg := DefaultKAGConfig()
	p, err := f.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("create default provider: %v", err)
	}
	defer p.Close()

	if p.Name() != "pattern" {
		t.Errorf("default provider = %q, want pattern", p.Name())
	}

	// An unset provider must also land on the pattern extractor rather than
	// erroring or silently picking a model.
	cfg.Provider = ""
	p2, err := f.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("create provider for empty config: %v", err)
	}
	defer p2.Close()
	if p2.Name() != "pattern" {
		t.Errorf("empty provider = %q, want pattern", p2.Name())
	}
}

func entityNames(entities []ExtractedEntity) []string {
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.Name)
	}
	return out
}
