# Why Not Just Point Claude at My Docs Folder?

*An honest answer. Updated July 2026.*

This is the first question any skeptical developer should ask about Conduit — or about any local knowledge-base tool for AI agents. Here is the honest answer, including the cases where the answer is "you're right, just use the folder."

## You probably don't need Conduit if…

**Your docs are markdown, live in (or near) your repos, and total a few MB.**
Then the built-in mechanisms are better than anything Conduit does:

- **Commit the docs to the repo.** Claude Code, Codex, Copilot and every other agentic tool will grep them. The 2026 consensus is clear: for content that's already on disk next to your code, agentic keyword search is excellent — studies put it within a few percent of full RAG pipelines for repo-scale content, with zero infrastructure.
- **Use your tool's native context features.** `CLAUDE.md` imports and path-scoped rules (Claude Code), `AGENTS.md` (Codex, Copilot, others), Cursor rules and @Docs. These are zero-install, git-versioned, and team-shareable.
- **Native memory is real now.** Claude Code, Copilot, Codex, and Cursor all remember project conventions and session learnings out of the box. You do not need a knowledge base for "remember how we do error handling."
- **Long context + prompt caching.** If your entire corpus is a few hundred KB, loading all of it into a 1M-token context with caching is often cheaper and simpler than running any retrieval service.

If that's your situation, stop here. Conduit would be added complexity for capabilities your tools already have. Most individual developers are in this category, and pretending otherwise would waste your afternoon.

## Where the folder approach actually breaks

Each of these is a real limitation of "just point the agent at files" — and they compound:

**1. Not everything is a grep-able text file.**
PDFs, Word docs, exported wiki dumps, scanned specs, slide decks. Agents can't grep a PDF. Something has to extract, clean, chunk, and index that content before any AI tool can use it. This is the single biggest gap: if your knowledge lives in `~/Documents/architecture-reviews/*.pdf` rather than `./docs/*.md`, the folder approach simply does not work.

**2. Prose is where keyword search actually fails.**
Grep won the code-search argument because code identifiers are literal. Prose isn't: the decision you need says "we chose eventual consistency for the billing pipeline" and you search for "why is invoicing async." No keyword overlaps. Semantic + keyword hybrid retrieval measurably beats either alone on prose corpora — this is the one place the extra machinery earns its keep.

**3. Cross-project knowledge has no home.**
Your agent's context is the current repo. The design decision that answers today's question might live in another project's docs, a wiki export from two years ago, or a folder of meeting notes. Per-repo files and per-repo memory can't see any of it.

**4. Corpus size eventually beats context windows.**
At tens of thousands of chunks (hundreds of PDFs, years of notes), you can't load everything, and letting an agent iteratively grep through it burns enormous context on misses. Retrieval that returns the right five chunks with citations is cheaper and faster.

**5. Tool lock-in for your knowledge layer.**
Native memory is siloed: Claude's lives in `~/.claude`, Cursor's in its cloud, Copilot's in GitHub. Tools also die (Gemini CLI was retired in June 2026). An MCP knowledge server is tool-agnostic — one index, every client, survives switching.

**6. Some content must not leave your machine.**
Cursor's doc indexing and Copilot Spaces are cloud-side. If your corpus includes anything confidential — client documents, unreleased plans, personal records — cloud indexing is a non-starter, and you need the retrieval stack to be fully local, including embeddings.

## So the honest positioning is:

> **Conduit is for the developer whose private knowledge is multi-format (PDFs and docs, not just markdown), multi-project (no single repo owns it), too large to paste into context, and too sensitive to index in someone's cloud — and who wants every AI tool they use to query it the same way.**

If fewer than two of those clauses describe you, use the folder. Genuinely.

## Current caveats (also honest)

- Conduit v1's install is heavy (containers + local models) — far heavier than this niche deserves. A single-binary rebuild is being evaluated; whether it proceeds depends on whether this niche turns out to be real for people other than the author.
- Retrieval results are returned verbatim to your AI client — if you index untrusted documents, their content can influence your agent (prompt injection). Index what you trust.
- Search quality on prose is the point of the architecture, but you should demand evidence: benchmark it on *your* corpus against plain grep before adopting anything.

*Feedback, counterexamples, and "actually I just use X and it's fine" stories are all welcome — that's exactly the signal this page exists to collect.*
