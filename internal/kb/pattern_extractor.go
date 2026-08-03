// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// pattern_extractor.go implements entity/relation extraction without an LLM.
//
// This is the default extraction backend when the knowledge graph is enabled.
// It makes no network calls, loads no model, and runs in microseconds per chunk,
// which is what makes an opt-in graph cheap enough to be worth trying.
//
// What it keeps from the LLM extraction path:
//   - the ExtractedEntity / ExtractedRelation contract, so it plugs into the
//     existing EntityExtractor, ExtractionValidator and storage code unchanged;
//   - canonical entity IDs (normalized name + type), so entities dedupe across
//     documents exactly as before;
//   - the validator's confidence threshold and prompt-injection screening, which
//     still runs over extracted names and descriptions.
//
// What it drops relative to LLM extraction:
//   - semantic entity typing. An LLM could tell "Apple the company" from "apple
//     the fruit"; pattern matching cannot, so almost everything is typed
//     `concept`, with `technology` reserved for shapes only code produces
//     (ALLCAPS acronyms, CamelCase identifiers, dotted/slashed names).
//   - semantic predicates. An LLM emitted `implements`, `depends_on`,
//     `created_by`; co-occurrence cannot justify those, so this extractor emits
//     only `relates_to` (same sentence) and `contains` (heading to entity).
//   - free-text descriptions. Descriptions here are the sentence the entity was
//     found in, truncated -- provenance, not summary.
//
// The honest framing: this produces a *co-occurrence graph with headings*, not a
// semantic knowledge graph. That is the LazyGraphRAG bet -- most of the value of
// a graph in retrieval comes from "these things are discussed together, here",
// which is cheap, and not from typed semantic edges, which are expensive.
package kb

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// PatternExtractorConfig tunes the pattern extractor.
type PatternExtractorConfig struct {
	// MinNameLength is the shortest accepted entity name (runes).
	// Default: 3. Shorter strings are almost always noise.
	MinNameLength int

	// MinOccurrences is how many times a candidate must appear in a chunk
	// before it is emitted. Default: 1.
	MinOccurrences int
}

// PatternExtractor extracts entities and relations using lexical patterns only.
// It satisfies LLMProvider so it can be swapped in wherever an LLM provider is
// expected, but it never talks to a model.
type PatternExtractor struct {
	cfg PatternExtractorConfig
}

// NewPatternExtractor creates a pattern-based extractor.
func NewPatternExtractor(cfg PatternExtractorConfig) *PatternExtractor {
	if cfg.MinNameLength <= 0 {
		cfg.MinNameLength = 3
	}
	if cfg.MinOccurrences <= 0 {
		cfg.MinOccurrences = 1
	}
	return &PatternExtractor{cfg: cfg}
}

// Name returns the provider name.
func (p *PatternExtractor) Name() string { return "pattern" }

// IsAvailable always returns true: there is nothing to be unavailable.
func (p *PatternExtractor) IsAvailable(context.Context) bool { return true }

// Close is a no-op.
func (p *PatternExtractor) Close() error { return nil }

var (
	// headingRe matches a Markdown ATX heading.
	headingRe = regexp.MustCompile(`(?m)^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$`)

	// properNounRe matches runs of capitalized words, allowing internal
	// connectors ("Department of Energy", "Model Context Protocol").
	properNounRe = regexp.MustCompile(`\b[A-Z][\p{L}\p{Nd}'’\-]*(?:\s+(?:of|for|and|the|de|van|von)?\s*[A-Z][\p{L}\p{Nd}'’\-]*)*`)

	// technicalRe matches identifier-shaped tokens: acronyms, CamelCase,
	// dotted or slashed paths, snake_case.
	technicalRe = regexp.MustCompile(`\b(?:[A-Z]{2,}[0-9]*|[a-z]+[A-Z][\p{L}\p{Nd}]*|[A-Za-z][\p{L}\p{Nd}]*(?:[./_][A-Za-z][\p{L}\p{Nd}]*)+)\b`)

	// sentenceSplitRe splits on sentence terminators and hard line breaks.
	sentenceSplitRe = regexp.MustCompile(`(?:[.!?]+\s+)|(?:\n{2,})|(?:\n)`)
)

// entityCandidate is an entity under consideration inside one chunk.
type entityCandidate struct {
	name        string
	entityType  EntityType
	occurrences int
	firstSeen   int
	description string
}

// ExtractEntities extracts entities and relations from req.Content.
//
// The pipeline is: split into sentences; per sentence, collect proper-noun and
// identifier-shaped candidates; score by occurrence count; emit `relates_to`
// edges between candidates sharing a sentence and `contains` edges from the
// enclosing Markdown heading. Everything is bounded by req.MaxEntities and
// req.MaxRelations.
func (p *PatternExtractor) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	content := req.Content
	headings := extractHeadings(content)

	candidates := make(map[string]*entityCandidate)
	// coOccurrence counts how often two candidate keys share a sentence.
	coOccurrence := make(map[[2]string]int)

	order := 0
	for _, sentence := range sentenceSplitRe.Split(content, -1) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		found := p.candidatesInSentence(sentence)
		for _, c := range found {
			key := entityCandidateKey(c.name, c.entityType)
			existing, ok := candidates[key]
			if !ok {
				order++
				c.firstSeen = order
				c.description = truncateForDescription(sentence)
				candidates[key] = &c
				continue
			}
			existing.occurrences++
			if existing.description == "" {
				existing.description = truncateForDescription(sentence)
			}
		}

		// Pairwise co-occurrence within the sentence.
		keys := make([]string, 0, len(found))
		seen := make(map[string]bool, len(found))
		for _, c := range found {
			k := entityCandidateKey(c.name, c.entityType)
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				coOccurrence[[2]string{keys[i], keys[j]}]++
			}
		}
	}

	// Headings become `section` entities and contain everything in the chunk.
	for _, h := range headings {
		key := entityCandidateKey(h, EntityTypeSection)
		if _, ok := candidates[key]; !ok {
			order++
			candidates[key] = &entityCandidate{
				name:        h,
				entityType:  EntityTypeSection,
				occurrences: 1,
				firstSeen:   order,
				description: "Section heading",
			}
		}
	}

	entities := p.rankCandidates(candidates, req.MaxEntities)

	// Only emit relations between entities that survived ranking.
	kept := make(map[string]ExtractedEntity, len(entities))
	for _, e := range entities {
		kept[entityCandidateKey(e.Name, EntityType(e.Type))] = e
	}

	relations := p.buildRelations(coOccurrence, headings, kept, req.MaxRelations)

	// Apply the caller's confidence floor here so the extractor honors the same
	// contract an LLM provider does.
	entities = filterEntitiesByConfidence(entities, req.ConfidenceThreshold)
	relations = filterRelationsByConfidence(relations, req.ConfidenceThreshold)

	return &ExtractionResponse{
		Entities:         entities,
		Relations:        relations,
		ProcessingTimeMs: time.Since(start).Milliseconds(),
		Model:            "pattern",
	}, nil
}

// candidatesInSentence returns entity candidates found in one sentence.
func (p *PatternExtractor) candidatesInSentence(sentence string) []entityCandidate {
	var out []entityCandidate
	seen := make(map[string]bool)

	add := func(raw string, t EntityType) {
		name := cleanCandidateName(raw)
		if !p.acceptName(name) {
			return
		}
		key := entityCandidateKey(name, t)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, entityCandidate{name: name, entityType: t, occurrences: 1})
	}

	for _, m := range technicalRe.FindAllString(sentence, -1) {
		add(m, EntityTypeTechnology)
	}
	for _, m := range properNounRe.FindAllString(sentence, -1) {
		// Skip anything already captured as an identifier shape.
		if technicalRe.MatchString(m) {
			continue
		}
		add(m, EntityTypeConcept)
	}

	return out
}

// acceptName screens out stopwords, too-short names and pure punctuation.
func (p *PatternExtractor) acceptName(name string) bool {
	if name == "" {
		return false
	}
	if len([]rune(name)) < p.cfg.MinNameLength {
		return false
	}
	if len(name) > MaxEntityNameLength {
		return false
	}
	lower := strings.ToLower(name)
	if stopwords[lower] {
		return false
	}
	// Require at least one letter.
	hasLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

// rankCandidates sorts candidates by occurrence count then first appearance and
// returns at most limit of them as ExtractedEntity values.
func (p *PatternExtractor) rankCandidates(candidates map[string]*entityCandidate, limit int) []ExtractedEntity {
	if limit <= 0 || limit > MaxEntitiesPerChunk {
		limit = MaxEntitiesPerChunk
	}

	list := make([]*entityCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.occurrences >= p.cfg.MinOccurrences {
			list = append(list, c)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].occurrences != list[j].occurrences {
			return list[i].occurrences > list[j].occurrences
		}
		return list[i].firstSeen < list[j].firstSeen
	})

	if len(list) > limit {
		list = list[:limit]
	}

	out := make([]ExtractedEntity, 0, len(list))
	for _, c := range list {
		out = append(out, ExtractedEntity{
			Name:        c.name,
			Type:        string(c.entityType),
			Description: c.description,
			Confidence:  patternConfidence(c),
		})
	}
	return out
}

// patternConfidence scores a candidate.
//
// Pattern matching cannot be as certain as a model reading the sentence, so the
// ceiling is deliberately below 1.0: a single mention scores 0.7 (the historical
// default threshold, so single mentions are kept but never outrank repeats), and
// each repeat adds 0.05 up to 0.9. Section headings are structural rather than
// inferred, so they score 0.85 flat.
func patternConfidence(c *entityCandidate) float64 {
	if c.entityType == EntityTypeSection {
		return 0.85
	}
	score := 0.7 + 0.05*float64(c.occurrences-1)
	if score > 0.9 {
		score = 0.9
	}
	return score
}

// buildRelations turns co-occurrence counts and headings into typed edges.
func (p *PatternExtractor) buildRelations(
	coOccurrence map[[2]string]int,
	headings []string,
	kept map[string]ExtractedEntity,
	limit int,
) []ExtractedRelation {
	if limit <= 0 || limit > MaxRelationsPerChunk {
		limit = MaxRelationsPerChunk
	}

	type scored struct {
		rel   ExtractedRelation
		count int
	}
	var all []scored

	// relates_to from same-sentence co-occurrence.
	for pair, count := range coOccurrence {
		a, aOK := kept[pair[0]]
		b, bOK := kept[pair[1]]
		if !aOK || !bOK || a.Name == b.Name {
			continue
		}
		all = append(all, scored{
			rel: ExtractedRelation{
				Subject:     a.Name,
				Predicate:   string(RelationRelatesTo),
				Object:      b.Name,
				Confidence:  coOccurrenceConfidence(count),
				Description: "co-occurs in the same sentence",
			},
			count: count,
		})
	}

	// contains from heading to every entity in the chunk.
	for _, h := range headings {
		section, ok := kept[entityCandidateKey(h, EntityTypeSection)]
		if !ok {
			continue
		}
		for key, e := range kept {
			if key == entityCandidateKey(h, EntityTypeSection) || e.Type == string(EntityTypeSection) {
				continue
			}
			all = append(all, scored{
				rel: ExtractedRelation{
					Subject:     section.Name,
					Predicate:   string(RelationContains),
					Object:      e.Name,
					Confidence:  0.8,
					Description: "entity appears under this heading",
				},
				count: 1,
			})
		}
	}

	// Highest-support edges first, with a deterministic tiebreak so repeated
	// extraction of the same chunk yields the same graph.
	sort.Slice(all, func(i, j int) bool {
		if all[i].count != all[j].count {
			return all[i].count > all[j].count
		}
		if all[i].rel.Subject != all[j].rel.Subject {
			return all[i].rel.Subject < all[j].rel.Subject
		}
		return all[i].rel.Object < all[j].rel.Object
	})

	if len(all) > limit {
		all = all[:limit]
	}

	out := make([]ExtractedRelation, 0, len(all))
	for _, s := range all {
		out = append(out, s.rel)
	}
	return out
}

// coOccurrenceConfidence maps a co-occurrence count onto a confidence score.
// One shared sentence is weak evidence (0.7); repeated co-occurrence within one
// chunk is stronger, capped at 0.85 because it is still only co-occurrence.
func coOccurrenceConfidence(count int) float64 {
	score := 0.7 + 0.05*float64(count-1)
	if score > 0.85 {
		score = 0.85
	}
	return score
}

// extractHeadings returns the Markdown headings present in content.
func extractHeadings(content string) []string {
	matches := headingRe.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		h := cleanCandidateName(m[2])
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// cleanCandidateName trims punctuation and collapses whitespace.
func cleanCandidateName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".,;:!?()[]{}\"'`*_-")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > MaxEntityNameLength {
		s = s[:MaxEntityNameLength]
	}
	return s
}

// entityCandidateKey builds the dedupe key for a candidate within a chunk.
func entityCandidateKey(name string, t EntityType) string {
	return normalizeEntityName(name) + "|" + string(t)
}

// truncateForDescription keeps a short provenance snippet.
func truncateForDescription(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func filterEntitiesByConfidence(in []ExtractedEntity, threshold float64) []ExtractedEntity {
	if threshold <= 0 {
		return in
	}
	out := in[:0]
	for _, e := range in {
		if e.Confidence >= threshold {
			out = append(out, e)
		}
	}
	return out
}

func filterRelationsByConfidence(in []ExtractedRelation, threshold float64) []ExtractedRelation {
	if threshold <= 0 {
		return in
	}
	out := in[:0]
	for _, r := range in {
		if r.Confidence >= threshold {
			out = append(out, r)
		}
	}
	return out
}
