# Conduit Performance Analysis: Timeout & Latency Configuration

**Version**: 1.0
**Date**: January 2026
**Status**: Analysis Complete, Implementation Pending

---

## 1. Executive Summary

This document analyzes Conduit's timeout configurations with focus on MCP server integration. The goal is to ensure KB search operations complete well within AI model tool call timeouts to prevent failures.

**Key Findings**:
- **Critical Gap**: Ollama and Qdrant clients have NO explicit timeouts configured
- **AI Model Constraints**: Most models have 60-120s tool call timeouts
- **Target**: KB search must complete in <10 seconds to be reliably useful
- **Recommendation**: Implement cascading timeouts with fail-fast behavior

---

## 2. AI Model Tool Call Timeout Research

### 2.1 Claude / Anthropic

| Setting | Value | Source |
|---------|-------|--------|
| MCP Server Startup | Configurable via `MCP_TIMEOUT` env var | [Claude Code Docs](https://code.claude.com/docs/en/mcp) |
| Bash Command Default | 2 minutes (120s) | [GitHub Issue #5615](https://github.com/anthropics/claude-code/issues/5615) |
| AWS Bedrock Claude | 60 minutes max | [AWS Bedrock Docs](https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages.html) |
| MCP Tool Token Limit | ~25,000 tokens | [GitHub Issue #6158](https://github.com/anthropics/claude-code/issues/6158) |

**Configurable Timeouts** (in `~/.claude/settings.json`):
```json
{
  "env": {
    "BASH_DEFAULT_TIMEOUT_MS": "1800000",
    "BASH_MAX_TIMEOUT_MS": "7200000"
  }
}
```

### 2.2 OpenAI / GPT

| Setting | Value | Source |
|---------|-------|--------|
| Default Response Timeout | 2 minutes (120s) | [OpenAI Community](https://community.openai.com/t/gpt-4-api-gateway-timeout-for-long-requests-but-billed-anyway/177214) |
| Max Configurable | 10 minutes (600s) | Client-side configuration |
| Azure OpenAI GPT-4o | 2 minutes hard limit | [Microsoft Q&A](https://learn.microsoft.com/en-us/answers/questions/1690782/azure-openai-gpt-4o-deployment-has-a-2-minute-hard) |

**Recommendation**: Use streaming to avoid gateway timeouts.

### 2.3 Google / Gemini

| Setting | Value | Source |
|---------|-------|--------|
| Explicit Tool Timeout | Not documented | SDK-dependent |
| Rate Limits | RPM/TPM based | [Gemini API Docs](https://ai.google.dev/gemini-api/docs/rate-limits) |
| Streaming | Counts as single request | [Rate Limits Guide](https://www.aifreeapi.com/en/posts/gemini-api-rate-limit) |

**Note**: Gemini focuses on rate limits rather than explicit timeouts. Client-side timeout configuration required.

### 2.4 Summary: Safe Operating Window

```
┌─────────────────────────────────────────────────────────────────┐
│                    Tool Call Timeline                           │
├─────────────────────────────────────────────────────────────────┤
│  0s     10s     30s     60s     90s     120s                   │
│  │       │       │       │       │       │                      │
│  ├───────┤       │       │       │       │                      │
│  │ SAFE  │       │       │       │       │ ← Target Zone        │
│  │       ├───────┼───────┤       │       │                      │
│  │       │ RISKY │       │       │       │ ← May timeout Gemini │
│  │       │       │       ├───────┼───────┤                      │
│  │       │       │       │ GPT/Claude    │ ← Default timeout    │
│  │       │       │       │       │       │                      │
└─────────────────────────────────────────────────────────────────┘
```

**Safe Zone**: 0-10 seconds (all models)
**Risky Zone**: 10-60 seconds (may fail on aggressive timeouts)
**Danger Zone**: >60 seconds (likely to fail)

---

## 3. Current Conduit Timeout Configuration

### 3.1 Configured Timeouts

| Component | Setting | Current Value | Location |
|-----------|---------|---------------|----------|
| HTTP Server Read | `ReadTimeout` | 30s | `internal/config/config.go:130` |
| HTTP Server Write | `WriteTimeout` | 30s | `internal/config/config.go:131` |
| HTTP Server Idle | `IdleTimeout` | 120s | `internal/config/config.go:132` |
| SQLite Busy | `busy_timeout` | 5000ms | `internal/store/store.go:25` |
| CLI Client | Default | 30s | `cmd/conduit/main.go:48` |
| CLI Sync | Extended | 5 minutes | `cmd/conduit/main.go:1813` |
| CLI Migrate | Extended | 10 minutes | `cmd/conduit/main.go:1942` |
| AI Provider | `TimeoutSeconds` | 120s | `internal/config/config.go:175` |
| Container Pull | `PullTimeout` | 10 minutes | `internal/config/config.go:137` |
| Container Start | `StartTimeout` | 30s | `internal/config/config.go:138` |
| Container Stop | `StopTimeout` | 10s | `internal/config/config.go:139` |

### 3.2 MISSING Timeouts (Critical Gaps)

| Component | Issue | Risk | Priority |
|-----------|-------|------|----------|
| Ollama Embedding Client | Uses `http.DefaultClient` | Unbounded wait on first call | **CRITICAL** |
| Qdrant Vector Client | Default gRPC settings | Unbounded wait on connection | **HIGH** |
| Search Handler | No per-operation timeout | Relies only on HTTP timeout | **HIGH** |
| Embedding Generation | No context timeout | Cold start can take 10+ seconds | **HIGH** |

---

## 4. Latency Analysis by Component

### 4.1 Measured Latencies (Typical)

| Operation | Cold Start | Warm | Notes |
|-----------|------------|------|-------|
| Query Parsing | <1ms | <1ms | In-memory string ops |
| FTS5 Search | 5-20ms | 5-15ms | SQLite, local disk |
| Ollama Embedding (cold) | 2-10s | 50-200ms | Model loading on first call |
| Ollama Embedding (warm) | 50-200ms | 50-150ms | GPU/CPU dependent |
| Qdrant Search | 10-50ms | 5-30ms | Vector similarity |
| RRF Fusion | 1-5ms | 1-3ms | In-memory sorting |
| MMR Diversity | 5-30ms | 5-20ms | O(n²) text comparisons |
| Reranking | 5-20ms | 5-15ms | Score recalculation |

### 4.2 Worst-Case Scenario

```
Cold start search with semantic + FTS5:

┌─────────────────────────────────────────────────────────────────┐
│ Query Parse │ Ollama Embed │ Qdrant Search │ FTS5 │ MMR │ Total │
│    1ms      │   10,000ms   │    50ms       │ 20ms │ 30ms│       │
├─────────────────────────────────────────────────────────────────┤
│                                                         ~10.1s  │
└─────────────────────────────────────────────────────────────────┘
```

**Problem**: First search after cold start can take 10+ seconds due to Ollama model loading.

### 4.3 Typical Warm Scenario

```
Warm search with semantic + FTS5:

┌─────────────────────────────────────────────────────────────────┐
│ Query Parse │ Ollama Embed │ Qdrant Search │ FTS5 │ MMR │ Total │
│    1ms      │    150ms     │    30ms       │ 15ms │ 20ms│       │
├─────────────────────────────────────────────────────────────────┤
│                                                         ~216ms  │
└─────────────────────────────────────────────────────────────────┘
```

**This is acceptable** for MCP tool calls.

---

## 5. Proposed Timeout Configuration

### 5.1 Recommended Values

| Component | Timeout | Rationale |
|-----------|---------|-----------|
| **Ollama HTTP Client** | 15s | Allow for cold start model loading |
| **Qdrant gRPC** | 5s | Vector search should be fast |
| **Search Handler Context** | 20s | Total search operation budget |
| **Embedding Operation** | 10s | Per-embedding timeout |
| **FTS5 Query** | 5s | SQLite should be very fast |
| **MCP Tool Response** | 25s | Below all AI model timeouts |

### 5.2 Cascading Timeout Architecture

```
MCP Tool Request (25s budget)
│
├── Search Handler (20s)
│   │
│   ├── Query Analysis (1s)
│   │
│   ├── Parallel Execution (15s)
│   │   │
│   │   ├── FTS5 Search (5s)
│   │   │
│   │   └── Semantic Search (15s)
│   │       │
│   │       ├── Ollama Embedding (10s)
│   │       │
│   │       └── Qdrant Query (5s)
│   │
│   └── Post-Processing (4s)
│       │
│       ├── RRF Fusion (1s)
│       ├── MMR Diversity (2s)
│       └── Reranking (1s)
│
└── Response Serialization (5s buffer)
```

### 5.3 Fail-Fast Behavior

When a component times out:

1. **Ollama Timeout**: Fall back to FTS5-only mode
2. **Qdrant Timeout**: Fall back to FTS5-only mode
3. **FTS5 Timeout**: Return error (critical failure)
4. **Handler Timeout**: Return partial results with warning

```go
// Proposed implementation pattern
ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
defer cancel()

// Run searches in parallel with individual timeouts
semanticCtx, _ := context.WithTimeout(ctx, 15*time.Second)
ftsCtx, _ := context.WithTimeout(ctx, 5*time.Second)

// If semantic times out, results come from FTS5 only
results, degraded := hs.searchWithFallback(semanticCtx, ftsCtx, query)
```

---

## 6. Configuration Implementation

### 6.1 New Configuration Structure

```go
// Add to internal/config/config.go

// KBConfig contains knowledge base settings
type KBConfig struct {
    // Search timeouts
    SearchTimeout     time.Duration `mapstructure:"search_timeout"`      // Total search budget
    EmbeddingTimeout  time.Duration `mapstructure:"embedding_timeout"`   // Per-embedding timeout
    VectorQueryTimeout time.Duration `mapstructure:"vector_query_timeout"` // Qdrant query timeout
    FTSTimeout        time.Duration `mapstructure:"fts_timeout"`         // SQLite FTS5 timeout

    // Client timeouts
    OllamaClientTimeout time.Duration `mapstructure:"ollama_client_timeout"` // HTTP client
    QdrantClientTimeout time.Duration `mapstructure:"qdrant_client_timeout"` // gRPC client
}

// Defaults
KB: KBConfig{
    SearchTimeout:       20 * time.Second,
    EmbeddingTimeout:    10 * time.Second,
    VectorQueryTimeout:  5 * time.Second,
    FTSTimeout:          5 * time.Second,
    OllamaClientTimeout: 15 * time.Second,
    QdrantClientTimeout: 5 * time.Second,
},
```

### 6.2 Ollama Client Fix

```go
// Fix in internal/kb/embeddings.go

func NewEmbeddingService(cfg EmbeddingConfig) (*EmbeddingService, error) {
    // ...

    // Create HTTP client with timeout (FIX: currently uses http.DefaultClient)
    httpClient := &http.Client{
        Timeout: cfg.ClientTimeout, // Default: 15s
        Transport: &http.Transport{
            MaxIdleConns:        10,
            IdleConnTimeout:     90 * time.Second,
            DisableCompression:  true,
        },
    }

    client := api.NewClient(ollamaURL, httpClient)
    // ...
}
```

### 6.3 Qdrant Client Fix

```go
// Fix in internal/kb/vectorstore.go

func NewVectorStore(cfg VectorStoreConfig) (*VectorStore, error) {
    // ...

    // Create Qdrant client with timeout (FIX: currently uses defaults)
    client, err := qdrant.NewClient(&qdrant.Config{
        Host:    cfg.Host,
        Port:    cfg.Port,
        Timeout: cfg.ClientTimeout, // Default: 5s
    })
    // ...
}
```

### 6.4 Search Handler Context

```go
// Fix in internal/daemon/handlers.go

func (d *Daemon) handleKBSearch(w http.ResponseWriter, r *http.Request) {
    // Create timeout context for search operation
    ctx, cancel := context.WithTimeout(r.Context(), d.cfg.KB.SearchTimeout)
    defer cancel()

    // ... rest of handler uses ctx ...
}
```

---

## 7. MCP Server Recommendations

### 7.1 Server-Side Configuration

For Conduit's MCP server (when exposed as MCP tool):

| Setting | Value | Rationale |
|---------|-------|-----------|
| Tool Response Timeout | 25s | Well under AI model limits |
| Max Response Tokens | 10,000 | Prevent MCPContentTooLargeError |
| Result Limit Default | 10 | Balance context vs latency |
| Snippet Length | 300 chars | Manageable context size |

### 7.2 Client-Side Recommendations

For AI clients connecting to Conduit:

| Client | Recommended Timeout | Notes |
|--------|---------------------|-------|
| Claude Code | Default (2 min) | Sufficient |
| Claude Desktop | `MCP_TIMEOUT=30000` | 30s for startup |
| OpenAI/GPT | Client-side 60s | SDK configuration |
| Gemini | Client-side 30s | Conservative |

### 7.3 MCP Tool Definition

```json
{
  "name": "kb_search",
  "description": "Search knowledge base. Returns in <10s typically, <25s max.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": { "type": "string" },
      "limit": { "type": "integer", "default": 10, "maximum": 50 },
      "mode": { "type": "string", "enum": ["auto", "hybrid", "semantic", "fts5"] }
    },
    "required": ["query"]
  }
}
```

---

## 8. Monitoring & Observability

### 8.1 Metrics to Track

| Metric | Purpose | Alert Threshold |
|--------|---------|-----------------|
| `kb_search_duration_ms` | Total search time | >10s warn, >20s critical |
| `kb_embedding_duration_ms` | Embedding latency | >5s warn |
| `kb_vector_search_duration_ms` | Qdrant latency | >2s warn |
| `kb_fts5_duration_ms` | FTS5 latency | >1s warn |
| `kb_search_degraded_count` | Fallback activations | Any > 0 |
| `kb_timeout_count` | Timeout occurrences | Any > 0 |

### 8.2 Health Check Enhancement

```go
// Add to health endpoint
type KBHealthStatus struct {
    OllamaReady     bool          `json:"ollama_ready"`
    OllamaLatency   time.Duration `json:"ollama_latency_ms"`
    QdrantReady     bool          `json:"qdrant_ready"`
    QdrantLatency   time.Duration `json:"qdrant_latency_ms"`
    FTS5Ready       bool          `json:"fts5_ready"`
    LastSearchTime  time.Duration `json:"last_search_time_ms"`
    DegradedMode    bool          `json:"degraded_mode"`
}
```

---

## 9. Cold Start Mitigation

### 9.1 Problem

First search after daemon start can take 10+ seconds due to:
1. Ollama loading embedding model into memory
2. Qdrant connection establishment
3. SQLite WAL recovery

### 9.2 Solutions

| Strategy | Implementation | Impact |
|----------|----------------|--------|
| **Warm-up on startup** | Pre-embed dummy query | Adds 2-5s to daemon start |
| **Keep-alive connections** | Connection pooling | Reduces reconnection overhead |
| **Model preloading** | `ollama pull` in install | Faster first embed |
| **Lazy initialization** | Current approach | Cold start penalty |

### 9.3 Recommended: Warm-up on First Request

```go
// internal/daemon/daemon.go

func (d *Daemon) warmupKB(ctx context.Context) {
    // Pre-warm embedding model with dummy query
    if d.kbSemantic != nil {
        go func() {
            warmupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
            defer cancel()
            _, _ = d.kbSemantic.Search(warmupCtx, "warmup", SemanticSearchOptions{Limit: 1})
            d.logger.Info().Msg("KB semantic search warmed up")
        }()
    }
}
```

---

## 10. Implementation Priority

### 10.1 Immediate (Critical)

1. **Add timeout to Ollama HTTP client** - Prevent unbounded waits
2. **Add timeout to Qdrant client** - Prevent connection hangs
3. **Add context timeout to search handler** - Enforce budget

### 10.2 Short-Term (High)

4. **Add KB configuration section** - Centralize timeout settings
5. **Implement graceful degradation** - Fall back to FTS5 on timeout
6. **Add search latency logging** - Visibility into performance

### 10.3 Medium-Term (Medium)

7. **Warm-up on startup** - Mitigate cold start
8. **Add metrics** - Production monitoring
9. **Health check enhancement** - Report KB component status

---

## 11. Summary

### 11.1 Key Recommendations

| Area | Current | Recommended | Risk if Not Fixed |
|------|---------|-------------|-------------------|
| Ollama Client | No timeout | 15s | Unbounded hang |
| Qdrant Client | Default | 5s | Unbounded hang |
| Search Handler | 30s (HTTP) | 20s (context) | Late timeout |
| MCP Response | Not set | 25s | AI model timeout |

### 11.2 Expected Outcomes

After implementing recommendations:

| Scenario | Current | Expected |
|----------|---------|----------|
| Warm search | ~200ms | ~200ms (no change) |
| Cold start search | 10+ seconds, may hang | <15s with graceful fallback |
| Ollama unavailable | Hang indefinitely | 15s timeout, FTS5 fallback |
| Qdrant unavailable | Hang indefinitely | 5s timeout, FTS5 fallback |

---

## 12. References

- [Claude Code Timeout Configuration](https://github.com/anthropics/claude-code/issues/5615)
- [MCP Timeout Issues](https://github.com/anthropics/claude-code/issues/424)
- [OpenAI Gateway Timeout](https://community.openai.com/t/gpt-4-api-gateway-timeout-for-long-requests-but-billed-anyway/177214)
- [Azure OpenAI Timeout](https://learn.microsoft.com/en-us/answers/questions/1690782/azure-openai-gpt-4o-deployment-has-a-2-minute-hard)
- [Gemini Rate Limits](https://ai.google.dev/gemini-api/docs/rate-limits)

---

**Document History**:
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | Jan 2026 | Conduit Team | Initial analysis |
