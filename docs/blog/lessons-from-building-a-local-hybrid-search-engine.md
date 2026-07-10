# What I Learned Building a Local Hybrid-Search Engine for AI Tools (and What the Bugs Taught Me)

*July 2026 — by the author of [Conduit](https://github.com/amlandas/Conduit-AI-Intelligence-Hub), a local-first knowledge base for AI coding tools, built over six weeks in late 2025 and honestly reassessed six months later.*

In December 2025 I set out to solve a problem that annoyed me daily: my AI coding tools knew nothing about my private documents — design docs, decision records, PDFs, notes spread across projects. So I built Conduit: a local-first knowledge base exposed over MCP, with hybrid search combining SQLite FTS5, a vector database, and a knowledge graph, all running on my machine with zero cloud egress.

I iterated through 17 documented phases of retrieval engineering. Then I stopped, life happened, and the project sat dormant for six months while the AI landscape reorganized itself. Last week I came back and ran a brutal audit — code, market, everything — with fresh eyes.

This post is both things I learned: the retrieval engineering lessons that held up, and the harder lessons from the audit about what I *thought* I'd built. The second half is less flattering and probably more useful.

## Part 1: The retrieval lessons that held up

### Pipeline quality is multiplicative

The single most important thing I learned about RAG: retrieval quality compounds *multiplicatively* across pipeline stages. If extraction is 90% good, chunking 90%, embedding 90%, and retrieval 90%, your end-to-end quality is 0.9⁴ ≈ **65%**. Users experience the product as "misses a third of the time," and no amount of tuning the *last* stage fixes the first one.

Corollary: **clean before embedding, not after retrieval.** I initially tried filtering boilerplate (headers, footers, page numbers, OCR noise) out of search results. Useless — once garbage is embedded, it pollutes the vector space and steals retrieval slots from real content. Moving the cleaning step before chunking and embedding was one of the highest-ROI changes in the entire project.

### Hybrid beats either alone, and RRF is the right glue

FTS5/BM25 alone is too literal ("car" won't find "automobile"). Vector search alone is too fuzzy — it struggles with exact identifiers, proper nouns, error strings, the things developers actually search for. Fusing both with Reciprocal Rank Fusion (rank-based, k=60) sidesteps the score-incompatibility problem entirely because you never compare BM25 scores to cosine similarities — only ranks.

Six months later this is now the boring consensus (hybrid BM25+dense+RRF is the acknowledged production baseline for prose retrieval), which is oddly validating. The interesting nuance the industry settled meanwhile: **for code, agentic grep won** — Anthropic, Windsurf, Devin and others dropped vector indexes for repo search. Embeddings are for when words are fuzzy; code usually isn't. Prose is. Build accordingly.

### Match the architecture to the consumer

My retrieval results are consumed by an AI, not a human. That changes the design more than any other single fact:

- **No LLM in the hot path.** The consuming model will do its own synthesis; an LLM summarizing retrieval results for another LLM is latency and information loss. Return clean, well-attributed chunks and get out of the way.
- **Precision and noise reduction over synthesis.** Cast a wide net at retrieval time, then filter aggressively (MMR for diversity, score floors, reranking).
- **Teach the model to use your tool.** The MCP tool description — "use short keyword phrases, not full questions" — did more for retrieval quality than several algorithm changes. Your tool description is a prompt; treat it like one.
- **Fail loudly into degraded modes.** When the vector side is down, falling back to lexical-only is fine — *if* you tell the client. A `degraded: true` flag in the response lets the agent calibrate its trust. (My biggest operational bug was the one place this fallback happened silently.)

### Boring engineering lessons that cost me real days

- Go's regexp is RE2: no lookahead, no backreferences. Test your patterns *in Go*, not in a PCRE playground.
- SQLite FTS5 syntax is a minefield: a bare hyphen can parse as column syntax and crash the query. Sanitize every metacharacter and document *why* for each one.
- SQLite's `bm25()` returns negative-is-better scores. Remember this every single time you write `ORDER BY`. (Foreshadowing.)
- Mixed abstraction layers create silent bugs: my `Sync()` deleted vectors through the indexer, but `Remove()` used raw SQL and orphaned them. Same operation, different layers, silent drift.

## Part 2: What the six-month-later audit taught me

This is the part I wish someone had written before I started.

### Features you never verified don't exist

The audit found that my proudest feature — query-adaptive weighting, which classified queries and tuned lexical/semantic weights per query type — was **dead code**. A default applied earlier in the call chain meant the adaptive branch could never execute. Every search ran 50/50, always. The code was there, well-commented, narrating its own sophistication. It just never ran.

Nearby: an apostrophe-handling rule quietly classified any query containing `'` as an exact-phrase search and skipped semantic retrieval entirely. *"What's the difference between X and Y"* — the most natural question shape in English — never touched the vector index.

Neither bug required cleverness to catch. A single table test over the fusion function would have found both. I had 156 tests — on the plumbing. The 1,265-line search core had zero, because it was "the part I understood best." **Test coverage inversely proportional to confidence is exactly backwards: your cleverest code is where your bugs are.**

### Solo velocity is a lie without verification

I shipped 32K lines in about four weeks and it *felt* like extraordinary velocity — 21 releases, detailed docs, phase after phase of "implemented ✅." The audit's verdict was that a meaningful part of that velocity was fake: merged features that never executed, fallback tiers that sorted results backwards (see: BM25 negative scores), a "relaxed search" whose wildcards were stripped by my own sanitizer before reaching the database. When you're the only reviewer, the only thing standing between you and self-deception is a test suite that doesn't care about your feelings.

### Your dependency stack is a product decision, not a technical one

Conduit requires Docker/Podman, a vector database container, a graph database container, Ollama, and ~10GB of models. Every one of those was individually justifiable ("purpose-built for the job"). Collectively, they produce a 45–90 minute install that I marketed as "5 minutes" because that's how long it took *me*, on a machine that already had everything.

Meanwhile, the tools that actually won mindshare in this space in 2026 install with one command and require zero external services. Embedded vector search in the same SQLite file, single-binary embedding inference — the capability gap between "heavy stack" and "single binary" collapsed to nearly nothing at personal-corpus scale, and the adoption gap is total. **Retrieval quality is invisible at install time; friction is not.** Nobody ever experienced my carefully tuned RRF fusion because nobody got through the install.

### The market doesn't wait for you to come back

In the six months Conduit slept: every major coding harness shipped native session memory, agentic grep won the code-search argument, one hosted service won public-docs retrieval outright, and a handful of small open-source projects moved into the exact "local private-docs over MCP" niche I'd staked out — none of them winning it either, which is its own signal worth sitting with. A dormant pre-adoption project doesn't hold its place in line. It just leaves.

## What happens next

I'm doing the honest thing: fixing the security issues the audit found (including one embarrassing one — the "private" knowledge base's containers were listening on all network interfaces), publishing this post, and writing a genuinely honest comparison page — "why not just point Claude at your docs folder?" — where the answer for most developers is *you should just do that*.

Whether the project continues depends on whether the remaining niche is real: multi-format private corpora (PDFs, wikis, decision records) that are too big for a context window, spread across too many projects for a repo folder, and too sensitive for cloud indexing — served identically to every AI tool you use. If that's you, I'd genuinely like to hear about it.

Either way, the retrieval engine taught me more about search than anything I've built — and the audit taught me more about myself as an engineer than the engine did.

---

*The full 17-phase engineering log is in the repo: [PROJECT_LEARNINGS.md](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/blob/main/docs/PROJECT_LEARNINGS.md). The reassessment that prompted this post — five code audits, four market research passes, three adversarial reviews of the resulting plan — convinced me that publishing lessons beats polishing features. Corrections welcome.*
