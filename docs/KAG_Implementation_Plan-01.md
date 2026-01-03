 I see plan mode is restricting file edits. Let me exit plan mode first so I can write the documents as you requested:

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 Ready to code?

 Here is Claude's plan:
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 KAG + RAG Hybrid Architecture Assessment

 Status: Ready for Implementation
 Created: 2026-01-03
 Updated: 2026-01-03 (Root cause investigation complete)
 Proposal: Add Knowledge Graph (KAG) pipeline alongside existing RAG

 ---
 Context: Why We're Here

 The recall_mode: "high" fix was implemented and deployed, but did not improve recall. Testing showed:
 - Still returns 3 documents (not the expected 4 threat variants)
 - Adding recall_mode: "high" introduced more noise (related but off-topic content)
 - The fundamental issue: RAG retrieves text chunks, not structured knowledge

 The user proposes adding a KAG (Knowledge-Augmented Generation) pipeline that extracts entities and relationships into a knowledge graph, then fuses KG results with RAG results at the LLM level.

 ---
 Root Cause Investigation Findings (2026-01-03)

 Direct SQLite analysis of the ASL-3 document reveals the actual chunk structure:

 Chunk Distribution for Threat Variants

 | Chunk ID               | Content                                                                                          |
 |------------------------|--------------------------------------------------------------------------------------------------|
 | chunk_c05c23e5a78cad37 | Threat variant 1: publicly known universal jailbreak                                             |
 | chunk_aff0d47ef06998d2 | Threat variants 2+3: nonpublic jailbreak (self-found + black market)                             |
 | chunk_1132b947db5729e5 | Threat variants 4+5+6+7: trusted user exploit, weight theft, public availability, benign queries |

 Key Finding: There are 7 Threat Variants, Not 4

 The document contains 7 distinct threat variants, not 4 as originally assumed:
 1. Publicly known universal jailbreak
 2. Self-found nonpublic jailbreak
 3. Black market nonpublic jailbreak
 4. Exploiting trusted user exemption
 5. Stealing model weights directly
 6. Stealing weights and making model public
 7. Uplift from benign-seeming queries

 Chunk Quality Assessment: Good

 The chunking is actually working well:
 - Variants are grouped by logical similarity (variants 2-3 together, variants 4-7 together)
 - Total of 4 chunks containing threat variant content
 - The ASL-3 document has 63 chunks total with good distribution

 Actual Issue: Result Presentation, Not Retrieval

 The issue is likely in how results are returned, not in whether chunks are found:
 1. ResultProcessor merges chunks from same document, hiding individual variants
 2. Vector similarity scoring may prefer variant 1 over others (more unique terms)
 3. The "3 documents" returned may actually be 3 distinct chunks being merged or scored

 Implication: KAG may still be valuable for multi-hop reasoning and aggregation, but simpler fixes could address the immediate recall issue.

 Revised Path Forward

 Given these findings, I recommend a two-track approach:

 Track 1: Quick Win (1-2 days)
 - Add expand_chunks: true parameter to kb_search_with_context to return all chunks without merging
 - This would immediately surface all 4 threat variant chunks (containing 7 variants)
 - No architectural changes needed

 Track 2: KAG Implementation (5-6 weeks)
 - Proceed with full KAG as planned for multi-hop reasoning and aggregation use cases
 - KAG provides unique value for questions like:
   - "How do threat variants relate to mitigation strategies?"
   - "List all security controls mentioned across documents"
   - "What concepts are shared between ASL-3 and other frameworks?"

 The user has already chosen to proceed with KAG (Option C), and this remains the right choice for the long-term knowledge base vision. The quick win can be implemented as part of Phase 1.

 ---
 My Honest Assessment

 1. Problem Diagnosis: Is KAG the Right Solution?

 Partially correct, but the root cause may be simpler.

 The user's test query: "What is the threat model summary in Anthropic ASL-3 deployment safeguards?"

 Current behavior analysis:
 - Returns 3 documents, but chunks from the same document may be merged by ResultProcessor
 - The 4 threat variants likely exist in the same section, and chunk boundaries may split them
 - kb_search_with_context merges chunks from the same document, potentially losing variant granularity

 What KAG would help with:
 - Multi-hop reasoning ("How is X related to Y across documents?")
 - Entity disambiguation ("ASL-3" vs "ASL-4" as distinct concepts)
 - Aggregation queries ("List all threat models mentioned")

 What KAG won't help with:
 - The specific recall issue (4 variants in one document section)
 - This is fundamentally a chunking/merging problem, not a retrieval problem

 2. Research Findings on KAG/GraphRAG

 | Aspect        | Finding                                                          | Source                                                                                                                          |
 |---------------|------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|
 | Effectiveness | GraphRAG outperforms naive RAG for global/summarization queries  | https://www.microsoft.com/en-us/research/blog/graphrag-unlocking-llm-discovery-on-narrative-private-data/                       |
 | 3x Accuracy   | Data.world benchmark showed 3x improvement on business questions | https://graphrag.com/concepts/intro-to-graphrag/                                                                                |
 | Cost          | LLM calls for graph construction are the majority of cost        | https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/graphrag-costs-explained-what-you-need-to-know/4207978           |
 | Token Usage   | 45K words → 57K tokens → ~$2.26 for indexing                     | https://khaledalam.medium.com/how-i-added-token-llm-cost-estimation-to-the-indexing-pipeline-of-microsoft-graphrag-c310dd56cb0c |
 | Noise Problem | LLMs extract inaccurate/messy knowledge from domain docs         | https://neo4j.com/blog/developer/knowledge-graph-extraction-challenges/                                                         |
 | Hallucination | LLM-based KG construction can introduce hallucinated triples     | https://dl.acm.org/doi/10.1145/3701716.3715240                                                                                  |

 3. Implementation Complexity for Conduit

 | Component           | Effort | Challenge                                                    |
 |---------------------|--------|--------------------------------------------------------------|
 | Entity Extraction   | High   | Requires LLM calls per chunk, prone to errors/hallucinations |
 | Graph Database      | Medium | Add Neo4j/FalkorDB dependency, schema design                 |
 | Graph Construction  | High   | Entity disambiguation is notoriously difficult               |
 | Query Translation   | High   | Natural language → Cypher/graph traversal                    |
 | MCP Tools           | Medium | New kag_query tool, fusion logic in prompts                  |
 | Ongoing Maintenance | High   | Graph updates on document changes, consistency               |

 Estimated effort: 3-6 weeks for a solo developer, significant ongoing maintenance.

 4. Cost Analysis for Your Corpus

 Your current KB: 23 documents, 1803 chunks, 66.3 MB

 For KAG indexing (rough estimate):
 - ~1803 chunks × ~500 tokens/chunk = ~900K input tokens
 - Entity extraction prompts + output = ~1.8M total tokens
 - At GPT-4o pricing ($2.50/1M input, $10/1M output): ~$15-25 initial indexing
 - Re-indexing on document updates multiplies this cost

 For local LLM (Ollama):
 - Can reduce API costs to zero, but extraction quality suffers
 - Larger models (70B+) needed for good KG extraction
 - RAM requirement: 32-64GB for quality extraction

 ---
 Alternative Approaches (Simpler Solutions First)

 Option A: Fix the Actual Problem (Chunking/Merging)

 The 4 threat variants are in the same document. The issue is likely:
 1. Chunking splits variants: Smart chunking may not preserve list items together
 2. Result merging hides variants: ResultProcessor merges chunks from same doc

 Proposed fix:
 - Add preserve_chunks option to kb_search_with_context to return individual chunks without merging
 - Improve chunking for enumerated lists (detect "1.", "2.", etc.)
 - Increase chunk overlap for list contexts

 Effort: 1-2 days

 Option B: Structured Extraction at Ingestion (Lighter KG)

 Extract only section headings and key entities during ingestion, store as metadata:
 - No full graph database needed
 - Store in SQLite JSON columns
 - Filter searches by section/entity

 Effort: 1 week

 Option C: Full KAG Implementation (User's Proposal)

 Implement the full dual-pipeline architecture as proposed.

 Effort: 3-6 weeks + ongoing maintenance

 ---
 Recommendation

 Start with Option A, evaluate, then consider B or C.

 1. Immediate: Debug why 4 variants aren't returned
   - Read the actual document chunks in the database
   - Verify if variants are split across chunks or merged together
   - This is 1 hour of investigation
 2. If chunking is the issue: Fix chunking for enumerated content (Option A)
 3. If multi-hop reasoning is truly needed: Consider Option B first (lighter KG)
 4. If enterprise-grade KG is required: Implement Option C

 Why I Don't Recommend Jumping to Full KAG

 1. The current problem may be simpler - We haven't verified the chunks contain all 4 variants
 2. High cost for a solo developer - Both implementation time and API/compute costs
 3. KG extraction quality is uncertain - Research shows significant noise/hallucination issues
 4. Maintenance burden - Graph consistency on document updates is non-trivial
 5. Overkill for current corpus - 23 documents may not benefit from graph structure

 ---
 If We Proceed with KAG (Option C)

 Recommended Architecture

 ┌─────────────────────────────────────────────────────────────────┐
 │                      Document Ingestion                          │
 │  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────────┐  │
 │  │ Extract  │ → │  Clean   │ → │  Chunk   │ → │ Store Chunks │  │
 │  └──────────┘   └──────────┘   └──────────┘   └──────────────┘  │
 │                                      │                           │
 │                    ┌─────────────────┴─────────────────┐        │
 │                    ▼                                   ▼        │
 │  ┌────────────────────────────────┐  ┌─────────────────────────┐│
 │  │       RAG Pipeline             │  │     KAG Pipeline        ││
 │  │  ┌──────────┐  ┌───────────┐  │  │  ┌──────────────────┐   ││
 │  │  │ Embed    │→ │  Qdrant   │  │  │  │ Entity/Relation  │   ││
 │  │  │ (Ollama) │  │ (vectors) │  │  │  │ Extraction (LLM) │   ││
 │  │  └──────────┘  └───────────┘  │  │  └────────┬─────────┘   ││
 │  │                               │  │           ▼             ││
 │  │  ┌──────────┐                 │  │  ┌──────────────────┐   ││
 │  │  │ FTS5     │                 │  │  │ Graph DB         │   ││
 │  │  │ (SQLite) │                 │  │  │ (FalkorDB/SQLite)│   ││
 │  │  └──────────┘                 │  │  └──────────────────┘   ││
 │  └────────────────────────────────┘  └─────────────────────────┘│
 └─────────────────────────────────────────────────────────────────┘

 ┌─────────────────────────────────────────────────────────────────┐
 │                        MCP Tools                                 │
 │  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────┐ │
 │  │ kb_search        │  │ kb_search_with   │  │ kag_query      │ │
 │  │ (hybrid RAG)     │  │ _context (RAG)   │  │ (graph + text) │ │
 │  └──────────────────┘  └──────────────────┘  └────────────────┘ │
 └─────────────────────────────────────────────────────────────────┘

 ┌─────────────────────────────────────────────────────────────────┐
 │                     LLM Fusion Layer                             │
 │  "Call both tools, use KAG for logical skeleton,                │
 │   RAG for supporting evidence and quotes"                        │
 └─────────────────────────────────────────────────────────────────┘

 Files to Create/Modify

 | File                            | Purpose                                           |
 |---------------------------------|---------------------------------------------------|
 | internal/kb/graph_store.go      | Graph database abstraction (FalkorDB or embedded) |
 | internal/kb/entity_extractor.go | LLM-based entity/relation extraction              |
 | internal/kb/kag_search.go       | Graph query + text retrieval                      |
 | internal/kb/mcp_server.go       | Add kag_query MCP tool                            |
 | internal/kb/indexer.go          | Add graph indexing during ingestion               |

 Technology Choices

 | Component         | Recommendation                                | Rationale                                                   |
 |-------------------|-----------------------------------------------|-------------------------------------------------------------|
 | Graph DB          | Neo4j Community Edition                       | Mature, excellent Cypher support, MCP integration available |
 | Entity Extraction | Llama 3.1 8B (primary) or Mistral 7B (faster) | Best quality/size tradeoff for entity extraction            |
 | Schema            | Schema-lite (subject-predicate-object)        | Avoid complex ontology design                               |
 | Query Translation | Cypher via Neo4j                              | Native query language, well-documented                      |

 ---
 Hardware & Model Analysis (32GB MacBook Pro M4)

 Will It Run? Yes, comfortably.

 Based on research from https://localllm.in/blog/ollama-vram-requirements-for-local-llms and https://www.arsturn.com/blog/ollama-hardware-guide-what-you-need-to-run-llms-locally:

 | Component                | RAM Usage | Notes                                   |
 |--------------------------|-----------|-----------------------------------------|
 | Llama 3.1 8B (Q4_K_M)    | ~4.8 GB   | Recommended for entity extraction       |
 | Mistral 7B (Q4_K_M)      | ~4.1 GB   | Faster, 10-15% quicker on Apple Silicon |
 | Phi-3 Medium 14B         | ~8 GB     | Better reasoning, but slower            |
 | Neo4j Community          | ~2-4 GB   | Depends on graph size                   |
 | macOS + Apps             | ~8-10 GB  | Baseline system usage                   |
 | Total (Llama 8B + Neo4j) | ~15-18 GB | Leaves 14-17GB headroom                 |

 M4 Pro/Max Performance (https://github.com/ggml-org/llama.cpp/discussions/4167):
 - Llama 3.1 8B: 40+ tokens/second at Q4_K_M
 - Unified memory advantage: No VRAM bottleneck, GPU cores matter most
 - M4 Max reported at >100 t/s for MoE models

 Model Licensing Analysis

 | Model            | License     | Commercial Use       | Restrictions                               |
 |------------------|-------------|----------------------|--------------------------------------------|
 | Mistral 7B/Small | Apache 2.0  | ✅ Fully permissive  | None                                       |
 | Qwen2.5          | Apache 2.0  | ✅ Fully permissive  | None                                       |
 | Phi-3            | MIT         | ✅ Fully permissive  | None                                       |
 | Llama 3.1        | Meta Custom | ⚠️ With restrictions | 700M+ MAU threshold, acceptable use policy |

 Sources: https://huggingface.co/blog/daya-shankar/open-source-llms, https://www.machinetranslation.com/blog/mistral-vs-llama

 Your concern is valid: Llama 3.1's license, while permissive for most use cases, has the 700M MAU clause and requires reviewing Meta's acceptable use policies. For a truly open project like Conduit, Apache 2.0 models are preferable.

 Entity Extraction Quality Benchmarks

 Per https://arxiv.org/html/2407.05786v1:

 | Model      | F1 Score (NER) | Precision | Recall | Notes                           |
 |------------|----------------|-----------|--------|---------------------------------|
 | Mistral 7B | 0.6376         | 0.71      | 0.66   | Best overall balance            |
 | Gemma 7B   | 0.6353         | 0.71      | 0.65   | Close second                    |
 | LLaMA 8B   | 0.5917         | 0.74      | 0.63   | High precision, lower recall    |
 | Phi-3      | -              | -         | -      | Strong for long contexts (128K) |

 Recommended Local Models for KAG (Apache 2.0 Focus)

 | Use Case           | Model               | License    | Why                                                           |
 |--------------------|---------------------|------------|---------------------------------------------------------------|
 | Primary Extraction | Mistral 7B Instruct | Apache 2.0 | Best F1 for entity extraction, 10-15% faster on Apple Silicon |
 | High Quality       | Qwen2.5 7B Instruct | Apache 2.0 | Excellent structured output (JSON), strong multilingual       |
 | Long Documents     | Phi-3 Medium 14B    | MIT        | 128K context, good for large PDFs                             |
 | Fast Fallback      | Qwen2.5 3B          | Apache 2.0 | Already-ish installed ecosystem                               |

 Hybrid Model Strategy (Your Brainstorm)

 Your idea of using different models for different tasks is solid:

 kb:
   kag:
     models:
       # Entity extraction - needs precision
       extraction: mistral:7b-instruct-q4_K_M  # Apache 2.0, best F1

       # Relation reasoning - needs understanding
       reasoning: qwen2.5:7b-instruct-q4_K_M   # Apache 2.0, good reasoning

       # Query understanding - needs instruction following
       query: phi3:medium-128k-instruct-q4_K_M  # MIT, long context

 Advantages:
 - Each model optimized for its task
 - All Apache 2.0 or MIT licensed
 - Can swap models per task based on performance
 - Avoids Llama license complexity

 Tradeoff: Slightly more memory if running multiple models, but 32GB M4 can handle it.

 Per https://www.frontiersin.org/journals/big-data/articles/10.3389/fdata.2025.1505877/pdf, instruction-tuned models work well for KG construction, and Mistral showed the best results for entity recognition tasks.

 Graph Database Comparison (All Options)

 | Database        | License           | Query Language | Deployment                | Best For                       | Cons                                 |
 |-----------------|-------------------|----------------|---------------------------|--------------------------------|--------------------------------------|
 | Neo4j Community | GPL v3            | Cypher         | Docker/Native             | General purpose, mature        | GPL license concerns, 4-core limit   |
 | FalkorDB        | Apache 2.0        | Cypher         | Docker (Redis)            | Real-time AI, ultra-fast       | Requires Redis, smaller community    |
 | Memgraph        | BSL/Commercial    | Cypher         | Docker                    | Streaming, real-time analytics | BSL license, memory-only             |
 | ArangoDB        | Community License | AQL            | Docker/Native             | Multi-model (doc+graph)        | 100GB limit, commercial restrictions |
 | JanusGraph      | Apache 2.0        | Gremlin        | Complex (Cassandra/HBase) | Massive scale, distributed     | Complex setup, operational overhead  |
 | Dgraph          | Apache 2.0        | GraphQL/DQL    | Docker/Native             | GraphQL-native apps            | Smaller ecosystem                    |

 Sources: https://www.puppygraph.com/blog/open-source-graph-databases, https://www.geeksforgeeks.org/blogs/open-source-graph-databases/

 Recommendation: FalkorDB (revised from Neo4j)

 Why FalkorDB over Neo4j:
 - Apache 2.0 license vs Neo4j's GPL v3 (license compatibility with Conduit)
 - https://www.falkordb.com/blog/graph-database-performance-benchmarks-falkordb-vs-neo4j/ for graph operations
 - Cypher compatible - same query language as Neo4j, easy migration path
 - https://www.pulsemcp.com/servers/falkordb-graph already exists
 - Built specifically for https://www.falkordb.com/blog/falkordb-vs-neo4j-for-ai-applications/
 - Supports Bolt protocol - can use Neo4j drivers

 Redis dependency is manageable (we already use Docker for Qdrant).

 Alternative: If you prefer avoiding Redis, use Dgraph (Apache 2.0, GraphQL-native, v25 making all features open source)

 Configuration Updates (Revised)

 kb:
   kag:
     enabled: true
     provider: ollama  # ollama, openai, anthropic

     # Model configuration (all Apache 2.0 or MIT licensed)
     ollama:
       # Hybrid model strategy
       models:
         extraction: mistral:7b-instruct-q4_K_M    # Apache 2.0, best F1 for NER
         reasoning: qwen2.5:7b-instruct-q4_K_M     # Apache 2.0, good reasoning
         query: phi3:medium-128k-instruct-q4_K_M   # MIT, long context
       host: http://localhost:11434

     # Cloud API options (optional)
     openai:
       model: gpt-4o-mini  # Cost-effective for extraction
       # API key from env: OPENAI_API_KEY
     anthropic:
       model: claude-3-5-haiku  # Fast and cheap
       # API key from env: ANTHROPIC_API_KEY

     # Graph database (FalkorDB - Apache 2.0)
     graph:
       backend: falkordb  # falkordb or dgraph
       falkordb:
         host: localhost
         port: 6379  # Redis port
         graph_name: conduit_kg

     extraction:
       confidence_threshold: 0.7
       max_entities_per_chunk: 20
       batch_size: 10

 Memory Budget on 32GB M4 MacBook

 | Scenario   | Model              | Neo4j | OS/Apps | Available |
 |------------|--------------------|-------|---------|-----------|
 | Light      | Llama 8B (5GB)     | 2GB   | 8GB     | 17GB free |
 | Medium     | Phi-3 14B (8GB)    | 3GB   | 8GB     | 13GB free |
 | Concurrent | 2× Llama 8B (10GB) | 4GB   | 8GB     | 10GB free |

 Verdict: 32GB M4 Pro/Max is more than sufficient for:
 - Running Llama 3.1 8B for entity extraction
 - Running Neo4j Community Edition
 - Running Qdrant for vector search
 - Running Ollama for embeddings (nomic-embed-text)
 - Normal development workflow

 ---
 User Decision

 Chosen approach: Option C (Full KAG) with investigation first

 Compute strategy: Multi-provider support
 - Default to local Ollama
 - Allow user to choose cloud API (GPT-4o, Claude) or hybrid during setup
 - Configurable via CLI and settings
 - No migration support initially (clean re-index for provider changes)

 ---
 Implementation Plan

 Phase 1: Root Cause Investigation (1 hour)

 Before KAG implementation, verify the actual chunking issue:

 1. Query SQLite to find ASL-3 document chunks containing "threat model"
 2. Verify if 4 variants exist in chunks or are merged/split
 3. Document findings to inform both KAG design and potential chunking improvements

 Phase 2: KAG Foundation (Week 1)

 Goal: Core infrastructure without full graph query support

 | Task                    | Files                       | Description                                           |
 |-------------------------|-----------------------------|-------------------------------------------------------|
 | Graph schema design     | internal/kb/graph_schema.go | Define entity/relation types for documents            |
 | Graph store abstraction | internal/kb/graph_store.go  | Interface supporting SQLite (embedded) or FalkorDB    |
 | SQLite graph tables     | internal/store/store.go     | Add kb_entities, kb_relations tables                  |
 | Provider config         | internal/kb/kag_config.go   | Multi-provider LLM config (Ollama, OpenAI, Anthropic) |

 Schema Design (Schema-lite approach):
 -- Entities table
 CREATE TABLE kb_entities (
     entity_id TEXT PRIMARY KEY,
     name TEXT NOT NULL,
     type TEXT NOT NULL,  -- 'concept', 'organization', 'person', 'document_section'
     source_chunk_id TEXT,
     metadata JSON,
     embedding BLOB,  -- Optional for entity similarity
     FOREIGN KEY (source_chunk_id) REFERENCES kb_chunks(chunk_id)
 );

 -- Relations table
 CREATE TABLE kb_relations (
     relation_id TEXT PRIMARY KEY,
     subject_id TEXT NOT NULL,
     predicate TEXT NOT NULL,  -- 'mentions', 'defines', 'relates_to', 'contains'
     object_id TEXT NOT NULL,
     confidence REAL,
     source_chunk_id TEXT,
     metadata JSON,
     FOREIGN KEY (subject_id) REFERENCES kb_entities(entity_id),
     FOREIGN KEY (object_id) REFERENCES kb_entities(entity_id)
 );

 -- Indexes for graph traversal
 CREATE INDEX idx_relations_subject ON kb_relations(subject_id);
 CREATE INDEX idx_relations_object ON kb_relations(object_id);
 CREATE INDEX idx_entities_type ON kb_entities(type);

 Phase 3: Entity Extraction (Week 2)

 Goal: LLM-based extraction with multi-provider support

 | Task                   | Files                               | Description                                       |
 |------------------------|-------------------------------------|---------------------------------------------------|
 | Extraction prompts     | internal/kb/extraction_prompts.go   | Structured prompts for entity/relation extraction |
 | LLM provider interface | internal/kb/llm_provider.go         | Abstract interface for Ollama/OpenAI/Anthropic    |
 | Ollama provider        | internal/kb/provider_ollama.go      | Local extraction using qwen2.5-coder or mistral   |
 | OpenAI provider        | internal/kb/provider_openai.go      | GPT-4o for higher quality extraction              |
 | Anthropic provider     | internal/kb/provider_anthropic.go   | Claude for extraction (optional)                  |
 | Entity extractor       | internal/kb/entity_extractor.go     | Orchestrates extraction pipeline                  |
 | Extraction validator   | internal/kb/extraction_validator.go | Filter hallucinated/low-confidence triples        |

 Extraction Prompt Structure:
 Extract entities and relationships from this text chunk.

 Text: {chunk_content}
 Document: {document_title}
 Section: {section_heading}

 Output JSON:
 {
   "entities": [
     {"name": "...", "type": "concept|person|org|section", "description": "..."}
   ],
   "relations": [
     {"subject": "...", "predicate": "...", "object": "...", "confidence": 0.0-1.0}
   ]
 }

 Rules:
 - Only extract entities explicitly mentioned
 - Use standardized predicates: mentions, defines, relates_to, contains, part_of
 - Confidence < 0.7 will be filtered

 Phase 4: Graph Indexing Integration (Week 3)

 Goal: Integrate KAG into existing ingestion pipeline

 | Task                  | Files                               | Description                                |
 |-----------------------|-------------------------------------|--------------------------------------------|
 | Indexer extension     | internal/kb/indexer.go              | Add graph indexing after chunk indexing    |
 | Background extraction | internal/kb/background_extractor.go | Async extraction to not block sync         |
 | CLI commands          | cmd/conduit/kb_kag.go               | conduit kb kag-sync, conduit kb kag-status |
 | Provider setup        | cmd/conduit/setup.go                | Add KAG provider selection to setup wizard |
 | Configuration         | internal/config/config.go           | Add KAG settings to conduit.yaml           |

 Configuration Schema:
 kb:
   kag:
     enabled: true
     provider: ollama  # ollama, openai, anthropic, hybrid
     ollama:
       model: qwen2.5-coder:7b
       host: http://localhost:11434
     openai:
       model: gpt-4o
       # API key from env: OPENAI_API_KEY
     anthropic:
       model: claude-3-5-sonnet
       # API key from env: ANTHROPIC_API_KEY
     extraction:
       confidence_threshold: 0.7
       max_entities_per_chunk: 20
       batch_size: 10

 Phase 5: KAG Query & MCP Tool (Week 4)

 Goal: Graph querying exposed as MCP tool

 | Task               | Files                               | Description                   |
 |--------------------|-------------------------------------|-------------------------------|
 | Graph query engine | internal/kb/kag_search.go           | Pattern-based graph traversal |
 | Query patterns     | internal/kb/query_patterns.go       | Common query templates        |
 | MCP tool           | internal/kb/mcp_server.go           | Add kag_query tool            |
 | Result formatter   | internal/kb/kag_result_formatter.go | Format graph results for LLM  |

 MCP Tool Schema:
 {
   "name": "kag_query",
   "description": "Query the knowledge graph for entities, relationships, and multi-hop reasoning. Best for: 'How is X related to Y?', 'What concepts are mentioned in section Z?', 'List all threat models'",
   "inputSchema": {
     "type": "object",
     "properties": {
       "query": {
         "type": "string",
         "description": "Natural language question about entities or relationships"
       },
       "entities": {
         "type": "array",
         "items": {"type": "string"},
         "description": "Optional entity hints to focus the search"
       },
       "max_hops": {
         "type": "integer",
         "default": 2,
         "description": "Maximum relationship hops for traversal"
       },
       "include_sources": {
         "type": "boolean",
         "default": true,
         "description": "Include source text snippets with results"
       }
     },
     "required": ["query"]
   }
 }

 Phase 6: Testing & Documentation (Week 5-6)

 | Task              | Description                                    |
 |-------------------|------------------------------------------------|
 | Unit tests        | Entity extraction, graph store, query patterns |
 | Integration tests | Full pipeline from document to graph query     |
 | Benchmark         | Compare RAG-only vs RAG+KAG on test queries    |
 | Documentation     | Update README, add KAG section to docs         |
 | CLI help          | Help text for new commands                     |

 ---
 Files to Create/Modify Summary

 | File                                | Action | Purpose                                   |
 |-------------------------------------|--------|-------------------------------------------|
 | internal/kb/graph_schema.go         | Create | Entity/relation type definitions          |
 | internal/kb/neo4j_store.go          | Create | Neo4j graph database abstraction          |
 | internal/kb/llm_provider.go         | Create | Multi-provider LLM interface              |
 | internal/kb/provider_ollama.go      | Create | Ollama extraction provider (Llama 3.1 8B) |
 | internal/kb/provider_openai.go      | Create | OpenAI extraction provider (GPT-4o-mini)  |
 | internal/kb/entity_extractor.go     | Create | Extraction orchestration                  |
 | internal/kb/extraction_validator.go | Create | Filter low-quality triples                |
 | internal/kb/kag_search.go           | Create | Cypher-based graph query engine           |
 | internal/kb/kag_config.go           | Create | KAG configuration                         |
 | internal/kb/indexer.go              | Modify | Integrate graph indexing                  |
 | internal/kb/mcp_server.go           | Modify | Add kag_query tool                        |
 | internal/config/config.go           | Modify | Add KAG settings                          |
 | cmd/conduit/kb_kag.go               | Create | CLI commands (kag-sync, kag-status)       |
 | cmd/conduit/setup.go                | Modify | Add KAG + Neo4j to setup wizard           |
 | scripts/install.sh                  | Modify | Add Neo4j installation option             |

 Neo4j Integration Details

 Docker-based Installation (like Qdrant):
 # Install Neo4j Community Edition
 conduit neo4j install

 # Creates container: conduit-neo4j
 # Ports: 7474 (HTTP), 7687 (Bolt)
 # Data: ~/.conduit/neo4j/

 CLI Commands:
 conduit neo4j install    # Install/start Neo4j container
 conduit neo4j start      # Start existing container
 conduit neo4j stop       # Stop container
 conduit neo4j status     # Check status + node/relationship counts
 conduit neo4j purge      # Clear all graph data

 conduit kb kag-sync      # Extract entities from indexed documents
 conduit kb kag-status    # Show KAG extraction progress
 conduit kb kag-query "How does ASL-3 relate to CBRN?"  # Test query

 ---
 Risk Mitigation

 | Risk                   | Mitigation                                                       |
 |------------------------|------------------------------------------------------------------|
 | LLM extraction quality | Validation layer filters low-confidence triples                  |
 | Hallucinated entities  | Cross-reference with source text, require provenance             |
 | API costs spiral       | Local Ollama as default, cost estimation before cloud extraction |
 | Graph grows unwieldy   | Limit entities per chunk, periodic graph compaction              |
 | Query latency          | Cache common traversals, limit max hops                          |

 ---
 Success Criteria

 1. Threat model query returns all 4 variants via kag_query
 2. Multi-hop queries work: "How does ASL-3 relate to CBRN weapons?"
 3. Aggregation queries work: "List all threat models in the KB"
 4. Latency acceptable: kag_query < 3 seconds for typical queries
 5. Provider switching works: Can change from Ollama to OpenAI without code changes

 ---
 Sources

 - https://www.plainconcepts.com/rag-vs-kag/
 - https://www.microsoft.com/en-us/research/blog/graphrag-unlocking-llm-discovery-on-narrative-private-data/
 - https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/graphrag-costs-explained-what-you-need-to-know/4207978
 - https://neo4j.com/blog/developer/knowledge-graph-extraction-challenges/
 - https://github.com/OpenSPG/KAG
 - https://arxiv.org/abs/2502.11371
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌