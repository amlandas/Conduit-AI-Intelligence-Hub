# Knowledge-Augmented Generation (KAG) - Low-Level Design

**Version**: 1.0
**Date**: January 2026
**Status**: Implemented

---

## 1. Module Overview

This document details the implementation of KAG components in the `internal/kb/` package.

### 1.1 File Structure

```
internal/kb/
├── kag_config.go           # Configuration types and defaults
├── graph_schema.go         # Entity/relation type definitions
├── llm_provider.go         # LLM provider interface
├── provider_ollama.go      # Ollama implementation
├── provider_openai.go      # OpenAI implementation
├── provider_anthropic.go   # Anthropic implementation
├── entity_extractor.go     # Extraction orchestration
├── extraction_validator.go # Validation and sanitization
├── falkordb_store.go       # FalkorDB graph store
├── kag_search.go           # Graph search engine
├── kag_test.go             # Unit tests
└── mcp_server.go           # MCP tool (kag_query added)
```

---

## 2. Configuration Module

### 2.1 KAGConfig Structure

**File**: `internal/kb/kag_config.go`

```go
type KAGConfig struct {
    Enabled    bool              `yaml:"enabled" json:"enabled"`
    Provider   string            `yaml:"provider" json:"provider"`
    Ollama     OllamaConfig      `yaml:"ollama" json:"ollama"`
    OpenAI     OpenAIConfig      `yaml:"openai" json:"openai"`
    Anthropic  AnthropicConfig   `yaml:"anthropic" json:"anthropic"`
    Extraction ExtractionConfig  `yaml:"extraction" json:"extraction"`
    Graph      GraphConfig       `yaml:"graph" json:"graph"`
}

type ExtractionConfig struct {
    ConfidenceThreshold  float64 `yaml:"confidence_threshold" json:"confidence_threshold"`
    MaxEntitiesPerChunk  int     `yaml:"max_entities_per_chunk" json:"max_entities_per_chunk"`
    MaxRelationsPerChunk int     `yaml:"max_relations_per_chunk" json:"max_relations_per_chunk"`
    BatchSize            int     `yaml:"batch_size" json:"batch_size"`
    EnableBackground     bool    `yaml:"enable_background" json:"enable_background"`
    BackgroundWorkers    int     `yaml:"background_workers" json:"background_workers"`
    QueueSize            int     `yaml:"queue_size" json:"queue_size"`
}
```

### 2.2 Default Values

```go
func DefaultKAGConfig() KAGConfig {
    return KAGConfig{
        Enabled:  false,  // Security: opt-in only
        Provider: "ollama",
        Ollama: OllamaConfig{
            Host:  "http://localhost:11434",
            Model: "mistral:7b-instruct-q4_K_M",
        },
        Extraction: ExtractionConfig{
            ConfidenceThreshold:  0.6,
            MaxEntitiesPerChunk:  20,
            MaxRelationsPerChunk: 30,
            BatchSize:            10,
            EnableBackground:     true,
            BackgroundWorkers:    2,
            QueueSize:            1000,
        },
        Graph: GraphConfig{
            Backend: "sqlite",
            FalkorDB: FalkorDBConfig{
                Host:      "localhost",
                Port:      6379,
                GraphName: "conduit_kg",
            },
        },
    }
}
```

---

## 3. Graph Schema Module

### 3.1 Entity Types

**File**: `internal/kb/graph_schema.go`

```go
type EntityType string

const (
    EntityTypeConcept      EntityType = "concept"
    EntityTypePerson       EntityType = "person"
    EntityTypeOrganization EntityType = "organization"
    EntityTypeTechnology   EntityType = "technology"
    EntityTypeLocation     EntityType = "location"
    EntityTypeSection      EntityType = "section"
)

// normalizeEntityType converts aliases to canonical types
func normalizeEntityType(t string) string {
    t = strings.ToLower(strings.TrimSpace(t))
    switch t {
    case "concept", "idea", "topic", "term":
        return string(EntityTypeConcept)
    case "person", "individual", "human":
        return string(EntityTypePerson)
    case "organization", "company", "institution", "org":
        return string(EntityTypeOrganization)
    case "technology", "tool", "framework", "language", "tech":
        return string(EntityTypeTechnology)
    case "location", "place", "geo", "geographic":
        return string(EntityTypeLocation)
    case "section", "chapter", "heading":
        return string(EntityTypeSection)
    default:
        return string(EntityTypeConcept)
    }
}
```

### 3.2 Relation Types

```go
type RelationType string

const (
    RelationMentions  RelationType = "mentions"
    RelationDefines   RelationType = "defines"
    RelationRelatesTo RelationType = "relates_to"
    RelationContains  RelationType = "contains"
    RelationPartOf    RelationType = "part_of"
    RelationUses      RelationType = "uses"
)

// normalizeRelationType converts aliases to canonical types
func normalizeRelationType(r string) string {
    r = strings.ToLower(strings.TrimSpace(r))
    switch r {
    case "mentions", "reference", "cites", "refers_to":
        return string(RelationMentions)
    case "defines", "describes", "explains":
        return string(RelationDefines)
    case "relates_to", "related", "associated":
        return string(RelationRelatesTo)
    case "contains", "includes", "has":
        return string(RelationContains)
    case "part_of", "member_of", "belongs_to":
        return string(RelationPartOf)
    case "uses", "utilizes", "employs":
        return string(RelationUses)
    default:
        return string(RelationRelatesTo)
    }
}
```

### 3.3 Data Structures

```go
// Entity represents a knowledge graph entity
type Entity struct {
    EntityID         string            `json:"entity_id"`
    Name             string            `json:"name"`
    Type             EntityType        `json:"type"`
    Description      string            `json:"description,omitempty"`
    SourceChunkID    string            `json:"source_chunk_id,omitempty"`
    SourceDocumentID string            `json:"source_document_id,omitempty"`
    Confidence       float64           `json:"confidence"`
    Metadata         map[string]string `json:"metadata,omitempty"`
    CreatedAt        time.Time         `json:"created_at"`
    UpdatedAt        time.Time         `json:"updated_at"`
}

// Relation represents a relationship between entities
type Relation struct {
    RelationID    string            `json:"relation_id"`
    SubjectID     string            `json:"subject_id"`
    Predicate     RelationType      `json:"predicate"`
    ObjectID      string            `json:"object_id"`
    SourceChunkID string            `json:"source_chunk_id,omitempty"`
    Confidence    float64           `json:"confidence"`
    Metadata      map[string]string `json:"metadata,omitempty"`
    CreatedAt     time.Time         `json:"created_at"`
}
```

### 3.4 Deterministic ID Generation

```go
// GenerateEntityID creates a deterministic ID from entity attributes
func GenerateEntityID(name, entityType, documentID string) string {
    data := fmt.Sprintf("%s:%s:%s",
        strings.ToLower(name),
        strings.ToLower(entityType),
        documentID)
    h := sha256.Sum256([]byte(data))
    return "ent_" + hex.EncodeToString(h[:8])
}

// GenerateRelationID creates a deterministic ID from relation attributes
func GenerateRelationID(subjectID, predicate, objectID string) string {
    data := fmt.Sprintf("%s:%s:%s", subjectID, predicate, objectID)
    h := sha256.Sum256([]byte(data))
    return "rel_" + hex.EncodeToString(h[:8])
}
```

---

## 4. LLM Provider Module

### 4.1 Provider Interface

**File**: `internal/kb/llm_provider.go`

```go
// LLMProvider defines the interface for entity extraction providers
type LLMProvider interface {
    // ExtractEntities extracts entities and relations from text
    ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error)

    // Name returns the provider name
    Name() string

    // IsAvailable checks if the provider is ready
    IsAvailable(ctx context.Context) bool
}

// ExtractionRequest represents a request to extract entities
type ExtractionRequest struct {
    Content             string  `json:"content"`
    DocumentTitle       string  `json:"document_title,omitempty"`
    ChunkID             string  `json:"chunk_id,omitempty"`
    MaxEntities         int     `json:"max_entities,omitempty"`
    MaxRelations        int     `json:"max_relations,omitempty"`
    ConfidenceThreshold float64 `json:"confidence_threshold,omitempty"`
}

// ExtractionResponse contains extracted entities and relations
type ExtractionResponse struct {
    Entities   []ExtractedEntity   `json:"entities"`
    Relations  []ExtractedRelation `json:"relations"`
    TokensUsed int                 `json:"tokens_used,omitempty"`
    Model      string              `json:"model,omitempty"`
}
```

### 4.2 Extraction Prompt Template

```go
const extractionPromptTemplate = `Extract entities and relationships from the following text.

<document_title>%s</document_title>

<text_to_analyze>
%s
</text_to_analyze>

Output a JSON object with this structure:
{
  "entities": [
    {
      "name": "Entity Name",
      "type": "concept|person|organization|technology|location|section",
      "description": "Brief description of the entity",
      "confidence": 0.0-1.0
    }
  ],
  "relations": [
    {
      "subject": "Subject Entity Name",
      "predicate": "mentions|defines|relates_to|contains|part_of|uses",
      "object": "Object Entity Name",
      "confidence": 0.0-1.0
    }
  ]
}

Rules:
- Only extract entities explicitly mentioned in the text
- Use confidence 0.9+ for clear, unambiguous entities
- Use confidence 0.6-0.8 for inferred or contextual entities
- Do not extract generic terms like "the", "this", "it"
- Limit to %d entities and %d relations maximum

Respond ONLY with valid JSON, no other text.`
```

### 4.3 Ollama Provider Implementation

**File**: `internal/kb/provider_ollama.go`

```go
type OllamaProvider struct {
    host   string
    model  string
    client *http.Client
    logger zerolog.Logger
}

func NewOllamaProvider(config OllamaConfig) *OllamaProvider {
    return &OllamaProvider{
        host:   config.Host,
        model:  config.Model,
        client: &http.Client{Timeout: 120 * time.Second},
        logger: observability.Logger("kb.ollama"),
    }
}

func (p *OllamaProvider) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
    // Sanitize input to prevent prompt injection
    content := sanitizePromptInput(req.Content)
    title := sanitizePromptInput(req.DocumentTitle)

    // Build prompt
    prompt := fmt.Sprintf(extractionPromptTemplate,
        title, content, req.MaxEntities, req.MaxRelations)

    // Call Ollama API
    ollamaReq := ollamaRequest{
        Model:  p.model,
        Prompt: prompt,
        Stream: false,
        Options: map[string]interface{}{
            "temperature": 0.1,  // Low temperature for consistent extraction
            "num_predict": 4096, // Allow long responses
        },
    }

    // Make HTTP request to Ollama
    // Parse JSON response
    // Return validated entities and relations
}
```

### 4.4 OpenAI Provider Implementation

**File**: `internal/kb/provider_openai.go`

```go
type OpenAIProvider struct {
    apiKey string
    model  string
    client *http.Client
    logger zerolog.Logger
}

func NewOpenAIProvider(config OpenAIConfig) *OpenAIProvider {
    apiKey := config.APIKey
    if apiKey == "" {
        apiKey = os.Getenv("OPENAI_API_KEY")
    }
    return &OpenAIProvider{
        apiKey: apiKey,
        model:  config.Model,
        client: &http.Client{Timeout: 60 * time.Second},
        logger: observability.Logger("kb.openai"),
    }
}

func (p *OpenAIProvider) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
    // Use Chat Completions API with JSON mode
    // response_format: {"type": "json_object"}
}
```

---

## 5. Extraction Validator Module

### 5.1 Validator Structure

**File**: `internal/kb/extraction_validator.go`

```go
type ExtractionValidator struct {
    minConfidence      float64
    maxNameLength      int
    maxDescLength      int
    suspiciousPatterns []*regexp.Regexp
    logger             zerolog.Logger
}

func NewExtractionValidator() *ExtractionValidator {
    return &ExtractionValidator{
        minConfidence:      DefaultConfidenceThreshold,
        maxNameLength:      200,
        maxDescLength:      1000,
        suspiciousPatterns: compileSuspiciousPatterns(),
        logger:             observability.Logger("kb.validator"),
    }
}
```

### 5.2 Suspicious Pattern Detection

```go
func compileSuspiciousPatterns() []*regexp.Regexp {
    patterns := []string{
        `(?i)ignore\s+(previous|all)\s+instructions`,
        `(?i)system\s*:\s*you\s+are`,
        `(?i)</?(text_to_analyze|document_title|system)>`,
        `(?i)as\s+an?\s+ai`,
        `(?i)reveal\s+(your|the)\s+(prompt|instructions)`,
        `(?i)forget\s+(everything|all)`,
        `(?i)new\s+instructions?\s*:`,
    }

    compiled := make([]*regexp.Regexp, 0, len(patterns))
    for _, p := range patterns {
        if re, err := regexp.Compile(p); err == nil {
            compiled = append(compiled, re)
        }
    }
    return compiled
}

func (v *ExtractionValidator) containsSuspiciousContent(text string) bool {
    for _, pattern := range v.suspiciousPatterns {
        if pattern.MatchString(text) {
            return true
        }
    }
    return false
}
```

### 5.3 Entity Validation

```go
func (v *ExtractionValidator) ValidateAndConvertEntity(
    extracted ExtractedEntity,
    chunkID, documentID string,
) *Entity {
    // Check confidence threshold
    if extracted.Confidence < v.minConfidence {
        v.logger.Debug().
            Str("name", extracted.Name).
            Float64("confidence", extracted.Confidence).
            Msg("entity below confidence threshold")
        return nil
    }

    // Validate name
    name := strings.TrimSpace(extracted.Name)
    if name == "" || len(name) > v.maxNameLength {
        return nil
    }

    // Check for suspicious content
    if v.containsSuspiciousContent(name) ||
       v.containsSuspiciousContent(extracted.Description) {
        v.logger.Warn().
            Str("name", name).
            Msg("filtered suspicious entity content")
        return nil
    }

    // Normalize type
    entityType := normalizeEntityType(extracted.Type)

    // Generate deterministic ID
    entityID := GenerateEntityID(name, entityType, documentID)

    return &Entity{
        EntityID:         entityID,
        Name:             name,
        Type:             EntityType(entityType),
        Description:      truncate(extracted.Description, v.maxDescLength),
        SourceChunkID:    chunkID,
        SourceDocumentID: documentID,
        Confidence:       extracted.Confidence,
        CreatedAt:        time.Now().UTC(),
        UpdatedAt:        time.Now().UTC(),
    }
}
```

---

## 6. Entity Extractor Module

### 6.1 Extractor Structure

**File**: `internal/kb/entity_extractor.go`

```go
type EntityExtractor struct {
    provider       LLMProvider
    db             *sql.DB
    graphStore     *FalkorDBStore
    config         KAGConfig
    validator      *ExtractionValidator
    logger         zerolog.Logger

    // Background processing
    mu             sync.Mutex
    extractionJobs chan extractionJob
    workerWg       sync.WaitGroup
    stopCh         chan struct{}
}

type extractionJob struct {
    chunkID      string
    documentID   string
    documentTitle string
    content      string
}
```

### 6.2 Background Worker

```go
func (e *EntityExtractor) startBackgroundWorkers() {
    for i := 0; i < e.config.Extraction.BackgroundWorkers; i++ {
        e.workerWg.Add(1)
        go e.worker(i)
    }
}

func (e *EntityExtractor) worker(id int) {
    defer e.workerWg.Done()

    for {
        select {
        case job := <-e.extractionJobs:
            ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
            _, err := e.ExtractFromChunk(ctx, job.chunkID, job.documentID,
                job.documentTitle, job.content)
            cancel()

            if err != nil {
                e.logger.Warn().
                    Err(err).
                    Str("chunk_id", job.chunkID).
                    Int("worker", id).
                    Msg("extraction failed")
            }

        case <-e.stopCh:
            return
        }
    }
}
```

### 6.3 Extraction Process

```go
func (e *EntityExtractor) ExtractFromChunk(
    ctx context.Context,
    chunkID, documentID, documentTitle, content string,
) (*ExtractionResult, error) {
    // 1. Check if already extracted
    status, err := e.getExtractionStatus(ctx, chunkID)
    if err == nil && status == "completed" {
        return nil, nil // Skip already processed
    }

    // 2. Update status to "extracting"
    e.updateExtractionStatus(ctx, chunkID, "extracting", 0, 0, "")

    // 3. Call LLM provider
    req := &ExtractionRequest{
        Content:             content,
        DocumentTitle:       documentTitle,
        ChunkID:             chunkID,
        MaxEntities:         e.config.Extraction.MaxEntitiesPerChunk,
        MaxRelations:        e.config.Extraction.MaxRelationsPerChunk,
        ConfidenceThreshold: e.config.Extraction.ConfidenceThreshold,
    }

    resp, err := e.provider.ExtractEntities(ctx, req)
    if err != nil {
        e.updateExtractionStatus(ctx, chunkID, "failed", 0, 0, err.Error())
        return nil, err
    }

    // 4. Validate and store entities
    var entities []*Entity
    for _, extracted := range resp.Entities {
        if entity := e.validator.ValidateAndConvertEntity(extracted, chunkID, documentID); entity != nil {
            entities = append(entities, entity)
        }
    }

    // 5. Store validated entities
    for _, entity := range entities {
        e.storeEntity(ctx, entity)
    }

    // 6. Validate and store relations
    var relations []*Relation
    for _, extracted := range resp.Relations {
        if relation := e.validator.ValidateAndConvertRelation(extracted, chunkID, entities); relation != nil {
            relations = append(relations, relation)
            e.storeRelation(ctx, relation)
        }
    }

    // 7. Update status to "completed"
    e.updateExtractionStatus(ctx, chunkID, "completed", len(entities), len(relations), "")

    return &ExtractionResult{
        Entities:  entities,
        Relations: relations,
    }, nil
}
```

---

## 7. KAG Search Module

### 7.1 Searcher Structure

**File**: `internal/kb/kag_search.go`

```go
type KAGSearcher struct {
    db         *sql.DB
    graphStore *FalkorDBStore
    logger     zerolog.Logger
}

type KAGSearchRequest struct {
    Query            string   `json:"query"`
    EntityHints      []string `json:"entities,omitempty"`
    MaxHops          int      `json:"max_hops,omitempty"`
    Limit            int      `json:"limit,omitempty"`
    IncludeRelations bool     `json:"include_relations,omitempty"`
    SourceFilter     string   `json:"source_id,omitempty"`
}

type KAGSearchResult struct {
    Entities      []EntityResult   `json:"entities"`
    Relations     []RelationResult `json:"relations,omitempty"`
    Context       string           `json:"context"`
    Query         string           `json:"query"`
    TotalEntities int              `json:"total_entities"`
}
```

### 7.2 Entity Search

```go
func (s *KAGSearcher) searchEntities(ctx context.Context, req *KAGSearchRequest) ([]EntityResult, error) {
    var query string
    var args []interface{}

    if len(req.EntityHints) > 0 {
        // Search by specific entity names
        placeholders := make([]string, len(req.EntityHints))
        for i, hint := range req.EntityHints {
            placeholders[i] = "e.name LIKE ?"
            args = append(args, "%"+hint+"%")
        }
        query = fmt.Sprintf(`
            SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
                   e.source_document_id, COALESCE(d.title, '') as doc_title
            FROM kb_entities e
            LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
            WHERE (%s)
        `, strings.Join(placeholders, " OR "))
    } else {
        // Free text search
        query = `
            SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
                   e.source_document_id, COALESCE(d.title, '') as doc_title
            FROM kb_entities e
            LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
            WHERE e.name LIKE ? OR e.description LIKE ?
        `
        args = append(args, "%"+req.Query+"%", "%"+req.Query+"%")
    }

    // Add source filter
    if req.SourceFilter != "" {
        query += " AND d.source_id = ?"
        args = append(args, req.SourceFilter)
    }

    // Order and limit
    query += " ORDER BY e.confidence DESC LIMIT ?"
    args = append(args, req.Limit)

    // Execute query and return results
}
```

### 7.3 Relation Lookup

```go
func (s *KAGSearcher) getRelations(ctx context.Context, entities []EntityResult, maxHops int) ([]RelationResult, error) {
    // Collect entity IDs
    entityIDs := make([]interface{}, len(entities))
    placeholders := make([]string, len(entities))
    for i, e := range entities {
        entityIDs[i] = e.ID
        placeholders[i] = "?"
    }

    // Query relations where entities are subject or object
    query := fmt.Sprintf(`
        SELECT DISTINCT
               COALESCE(se.name, r.subject_id) as subject_name,
               r.predicate,
               COALESCE(oe.name, r.object_id) as object_name,
               r.confidence
        FROM kb_relations r
        LEFT JOIN kb_entities se ON r.subject_id = se.entity_id
        LEFT JOIN kb_entities oe ON r.object_id = oe.entity_id
        WHERE r.subject_id IN (%s) OR r.object_id IN (%s)
        ORDER BY r.confidence DESC
        LIMIT 50
    `, strings.Join(placeholders, ","), strings.Join(placeholders, ","))

    args := append(entityIDs, entityIDs...)
    // Execute and return
}
```

### 7.4 Context Formatting

```go
func (s *KAGSearcher) formatContext(query string, entities []EntityResult, relations []RelationResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Knowledge Graph Results for: %s\n\n", query))

    // Format entities
    sb.WriteString("## Entities\n")
    for i, e := range entities {
        if i >= 10 {
            sb.WriteString(fmt.Sprintf("... and %d more entities\n", len(entities)-10))
            break
        }
        sb.WriteString(fmt.Sprintf("- **%s** (%s)", e.Name, e.Type))
        if e.Description != "" {
            desc := e.Description
            if len(desc) > 100 {
                desc = desc[:100] + "..."
            }
            sb.WriteString(fmt.Sprintf(": %s", desc))
        }
        sb.WriteString("\n")
    }

    // Format relations
    if len(relations) > 0 {
        sb.WriteString("\n## Relationships\n")
        for i, r := range relations {
            if i >= 15 {
                sb.WriteString(fmt.Sprintf("... and %d more relationships\n", len(relations)-15))
                break
            }
            sb.WriteString(fmt.Sprintf("- %s → %s → %s\n", r.SubjectName, r.Predicate, r.ObjectName))
        }
    }

    return sb.String()
}
```

---

## 8. MCP Tool Integration

### 8.1 Tool Registration

**File**: `internal/kb/mcp_server.go`

```go
func (s *MCPServer) getTools() []map[string]interface{} {
    tools := []map[string]interface{}{
        // ... existing tools ...

        // KAG query tool
        {
            "name":        "kag_query",
            "description": "Query the knowledge graph for entities and their relationships. Use this for questions about concepts, people, organizations, or how things relate to each other.",
            "inputSchema": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "query": map[string]interface{}{
                        "type":        "string",
                        "description": "Natural language question or search terms",
                    },
                    "entities": map[string]interface{}{
                        "type":        "array",
                        "items":       map[string]interface{}{"type": "string"},
                        "description": "Optional: specific entity names to search for",
                    },
                    "include_relations": map[string]interface{}{
                        "type":        "boolean",
                        "default":     true,
                        "description": "Include relationships between entities",
                    },
                    "max_hops": map[string]interface{}{
                        "type":        "integer",
                        "default":     2,
                        "maximum":     3,
                        "description": "Maximum relationship hops",
                    },
                    "limit": map[string]interface{}{
                        "type":        "integer",
                        "default":     20,
                        "maximum":     100,
                        "description": "Maximum entities to return",
                    },
                    "source_id": map[string]interface{}{
                        "type":        "string",
                        "description": "Optional: limit to specific KB source",
                    },
                },
                "required": []string{"query"},
            },
        },
    }
    return tools
}
```

### 8.2 Tool Handler

```go
func (s *MCPServer) toolKagQuery(ctx context.Context, args json.RawMessage) (interface{}, error) {
    var params struct {
        Query            string   `json:"query"`
        Entities         []string `json:"entities"`
        IncludeRelations bool     `json:"include_relations"`
        MaxHops          int      `json:"max_hops"`
        Limit            int      `json:"limit"`
        SourceID         string   `json:"source_id"`
    }

    if err := json.Unmarshal(args, &params); err != nil {
        return nil, fmt.Errorf("parse arguments: %w", err)
    }

    // Validate
    if params.Query == "" {
        return nil, fmt.Errorf("query is required")
    }

    // Search
    req := &KAGSearchRequest{
        Query:            params.Query,
        EntityHints:      params.Entities,
        MaxHops:          params.MaxHops,
        Limit:            params.Limit,
        IncludeRelations: params.IncludeRelations,
        SourceFilter:     params.SourceID,
    }

    result, err := s.kagSearcher.Search(ctx, req)
    if err != nil {
        return nil, err
    }

    return result, nil
}
```

---

## 9. Database Operations

### 9.1 Entity Storage

```go
func (e *EntityExtractor) storeEntity(ctx context.Context, entity *Entity) error {
    _, err := e.db.ExecContext(ctx, `
        INSERT INTO kb_entities (
            entity_id, name, type, description, source_chunk_id,
            source_document_id, confidence, metadata, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(entity_id) DO UPDATE SET
            description = COALESCE(excluded.description, description),
            confidence = MAX(confidence, excluded.confidence),
            updated_at = excluded.updated_at
    `, entity.EntityID, entity.Name, entity.Type, entity.Description,
       entity.SourceChunkID, entity.SourceDocumentID, entity.Confidence,
       "{}", entity.CreatedAt.Format(time.RFC3339), entity.UpdatedAt.Format(time.RFC3339))

    return err
}
```

### 9.2 Relation Storage

```go
func (e *EntityExtractor) storeRelation(ctx context.Context, relation *Relation) error {
    _, err := e.db.ExecContext(ctx, `
        INSERT INTO kb_relations (
            relation_id, subject_id, predicate, object_id,
            source_chunk_id, confidence, metadata, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(relation_id) DO UPDATE SET
            confidence = MAX(confidence, excluded.confidence)
    `, relation.RelationID, relation.SubjectID, relation.Predicate,
       relation.ObjectID, relation.SourceChunkID, relation.Confidence,
       "{}", relation.CreatedAt.Format(time.RFC3339))

    return err
}
```

### 9.3 Extraction Status

```go
func (e *EntityExtractor) updateExtractionStatus(
    ctx context.Context, chunkID, status string,
    entityCount, relationCount int, errorMsg string,
) error {
    now := time.Now().UTC().Format(time.RFC3339)
    _, err := e.db.ExecContext(ctx, `
        INSERT INTO kb_extraction_status (
            chunk_id, status, entity_count, relation_count,
            error_message, extracted_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(chunk_id) DO UPDATE SET
            status = excluded.status,
            entity_count = excluded.entity_count,
            relation_count = excluded.relation_count,
            error_message = excluded.error_message,
            updated_at = excluded.updated_at
    `, chunkID, status, entityCount, relationCount, errorMsg, now, now)

    return err
}
```

---

## 10. Prompt Injection Protection

### 10.1 Input Sanitization

```go
func sanitizePromptInput(input string) string {
    // Remove control characters
    input = strings.Map(func(r rune) rune {
        if r < 32 && r != '\n' && r != '\r' && r != '\t' {
            return -1
        }
        return r
    }, input)

    // Filter injection patterns
    patterns := []struct {
        regex       *regexp.Regexp
        replacement string
    }{
        {regexp.MustCompile(`(?i)ignore\s+(previous|all)\s+instructions`), "[FILTERED]"},
        {regexp.MustCompile(`(?i)</(text_to_analyze|document_title|system)>`), "[FILTERED]"},
        {regexp.MustCompile(`(?i)system\s*:\s*you\s+are`), "[FILTERED]"},
        {regexp.MustCompile(`(?i)reveal\s+(your|the)\s+(prompt|instructions)`), "[FILTERED]"},
    }

    for _, p := range patterns {
        input = p.regex.ReplaceAllString(input, p.replacement)
    }

    // Limit length
    if len(input) > MaxPromptInputLength {
        input = input[:MaxPromptInputLength]
    }

    return input
}
```

---

## 11. Testing

### 11.1 Unit Test Coverage

**File**: `internal/kb/kag_test.go`

| Test | Description |
|------|-------------|
| `TestEntityTypes` | Entity type normalization |
| `TestRelationTypes` | Relation type normalization |
| `TestExtractionValidator` | Validation rules |
| `TestKAGSearch` | Search functionality |
| `TestKAGConfig` | Security defaults |
| `TestGenerateEntityID` | Deterministic IDs |
| `TestGenerateRelationID` | Deterministic IDs |
| `TestPromptSanitization` | Injection protection |
| `BenchmarkEntitySearch` | Performance |

### 11.2 Test Database Setup

```go
func testDB(t *testing.T) *sql.DB {
    t.Helper()

    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")

    db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }

    // Create KAG tables
    tables := []string{
        `CREATE TABLE IF NOT EXISTS kb_documents (...)`,
        `CREATE TABLE IF NOT EXISTS kb_chunks (...)`,
        `CREATE TABLE IF NOT EXISTS kb_entities (...)`,
        `CREATE TABLE IF NOT EXISTS kb_relations (...)`,
        `CREATE TABLE IF NOT EXISTS kb_extraction_status (...)`,
    }

    for _, table := range tables {
        if _, err := db.Exec(table); err != nil {
            t.Fatalf("create table: %v", err)
        }
    }

    return db
}
```

---

## 12. Error Handling

### 12.1 Error Types

```go
var (
    ErrKAGDisabled         = errors.New("KAG is not enabled")
    ErrProviderUnavailable = errors.New("LLM provider is unavailable")
    ErrExtractionFailed    = errors.New("entity extraction failed")
    ErrInvalidEntity       = errors.New("invalid entity data")
    ErrInvalidRelation     = errors.New("invalid relation data")
)
```

### 12.2 Graceful Degradation

```go
func (s *MCPServer) toolKagQuery(ctx context.Context, args json.RawMessage) (interface{}, error) {
    // Check if KAG is enabled
    if s.kagSearcher == nil {
        return &KAGSearchResult{
            Entities:      []EntityResult{},
            Relations:     []RelationResult{},
            Context:       "KAG is not enabled. Enable it in configuration to use knowledge graph queries.",
            Query:         "",
            TotalEntities: 0,
        }, nil
    }

    // Proceed with search...
}
```

---

**Document History**:
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | Jan 2026 | Conduit Team | Initial LLD |
