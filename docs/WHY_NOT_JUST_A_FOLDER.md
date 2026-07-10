# Why Not Just Point My AI at My Files?

*Updated July 2026.*

Every AI tool now claims it can work with your documents. ChatGPT and Claude take file uploads and Projects. NotebookLM eats whole folders. Coding agents grep your repo. So the first question anyone should ask about Conduit is: why would I run a separate knowledge base at all?

Fair question. For a lot of people the answer is: you shouldn't. This page tries to sort out which side of the line you're on.

## When the built-in options are enough

**If your material is small and lives in one place, use what your AI tool already gives you.**

A few hundred pages of notes, a handful of PDFs, one active project? Upload them to a ChatGPT or Claude Project, or drop them in NotebookLM. These tools handle modest corpora well, and models with million-token context windows plus prompt caching can often just read everything you have. No index, no sync, nothing to maintain.

**If you're a developer and your docs are markdown sitting in your repos, you need even less.** Coding agents search files directly, and the consensus by now is that this works well for anything that's already plain text on disk. `CLAUDE.md` imports, `AGENTS.md`, Cursor rules. All free, all versioned with your code. Claude Code, Copilot, Codex, and Cursor also remember project conventions on their own now, so "help my tool remember how we do things" no longer needs any external product.

If that covers you, stop reading. Adding Conduit to that setup buys you complexity and not much else. Most people are in this category, including most developers.

## Where it stops working

The built-in options share some walls. You hit them when:

**Your knowledge isn't in grep-able text files.** Case files, research papers, scanned contracts, old Word documents, slide decks, wiki exports. An agent can't grep a PDF, and upload-based tools cap out on file counts and sizes long before a real archive fits. Somebody has to extract, clean, and index that content before any AI can use it. This is the biggest wall and the most common one.

**You search prose the way people actually ask questions.** Keyword search wins for code because identifiers are literal. Prose is the opposite. The decision memo says "we chose eventual consistency for the billing pipeline" and you ask "why is invoicing async." Nothing overlaps. For documents written in natural language, combining semantic and keyword retrieval measurably beats either one, and it beats an agent guessing search terms.

**Your knowledge crosses projects and apps.** The answer to today's question might sit in another project's design docs, in notes from two years ago, or in a client folder that has nothing to do with the repo or Project you're currently in. Per-project uploads and per-repo files can't see across those boundaries. You end up re-uploading the same material into every new context, or doing without.

**The corpus outgrows the context window.** Hundreds of PDFs or years of accumulated notes won't fit, and letting an agent rummage through it by trial and error burns time and tokens on misses. Past a certain size you want retrieval that hands back the right five passages with citations.

**Your knowledge is trapped in one vendor's silo.** Claude's memory lives in Claude. ChatGPT's Projects live in ChatGPT. Cursor's index lives in Cursor's cloud. Switch tools, or use three at once, and you're maintaining three copies of your own knowledge. Tools also get discontinued; your knowledge layer shouldn't die with them. An MCP server is one index that every client can query, and MCP is now supported nearly everywhere.

**The content can't leave your machine.** Uploads, Projects, NotebookLM, Cursor's doc indexing, Copilot Spaces: all cloud-side. If you handle client documents, patient records, unreleased work, or anything under a confidentiality obligation, that's disqualifying. Then the whole pipeline has to run locally, embeddings included.

## Who Conduit is actually for

Put together, that's a specific person: someone whose private knowledge is **multi-format** (PDFs and documents, not just markdown), **spread across projects or apps**, **too large to upload or paste**, and in some cases **too sensitive to index in anyone's cloud** — and who wants the AI tools they use, plural, to query it the same way.

That might be a researcher with a decade of papers and reading notes. A lawyer or consultant with client archives. An analyst with years of reports. Or a developer whose design docs and decision records span a dozen repos. The common thread is a serious personal corpus and more than one AI tool, not a job title.

If fewer than two of those clauses describe you, the folder or the upload button is the right answer. That's not false modesty; it's what the comparison comes out to.

## Caveats, since this page promised honesty

- Conduit v1's install is heavy for what it does: containers plus local models. A single-binary rebuild is under evaluation, and whether it happens depends on whether enough people recognize themselves in the paragraph above.
- Search results go to your AI client verbatim. If you index documents you don't trust, whatever instructions they contain reach your AI as tool output (prompt injection). Index what you trust.
- Don't take retrieval quality on faith, from us or anyone. Test it on your own material against the simplest alternative before adopting any tool in this category.

*If you read this and thought "I just use X and it works fine," that's useful information. Tell us. Collecting exactly that signal is what this page is for.*
