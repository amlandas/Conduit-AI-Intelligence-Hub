# MCP Port Notes (WP-3.1)

Conduit's Knowledge Base MCP server moved off a hand-rolled JSON-RPC
implementation onto the official Model Context Protocol Go SDK.

| | Before | After |
|---|---|---|
| Implementation | `internal/kb/mcp_server.go` (862 lines, hand-rolled) | `internal/mcpserver/` (SDK-based) |
| Protocol revision | hard-coded `2024-11-05` | negotiated, up to `2026-07-28` |
| Tools | 6 | 7 (`kb_lexical_search` added) |
| Launch command | `conduit mcp kb` | `conduit mcp kb` (unchanged) |

---

## Versions

- **SDK**: `github.com/modelcontextprotocol/go-sdk v1.7.0` (released 2026-07-27).
  This is the first SDK line that speaks spec revision `2026-07-28`.
- **Transitive dependency used directly**: `github.com/google/jsonschema-go v0.4.3`.
  It is the SDK's own JSON Schema type; we import it to hand-write each tool's
  `InputSchema` rather than letting the SDK infer one from a Go struct. That is
  what keeps the published schemas byte-identical to the previous server's.
- **Other new transitive deps** (pulled in by the SDK, not used directly):
  `segmentio/encoding`, `segmentio/asm`, `yosida95/uritemplate/v3`,
  `golang.org/x/oauth2`, `golang.org/x/sync`, `golang.org/x/time`.

## Protocol version negotiation

The SDK's supported set, newest first:

```
2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26, 2024-11-05
```

Two lifecycle models coexist, and the SDK server implements both:

- **Stateless (>= 2026-07-28, SEP-2575)** — there is no `initialize` /
  `notifications/initialized` handshake. A client discovers the server with the
  `server/discover` RPC and the session is live from its first request; protocol
  version and client capabilities travel in each request's `_meta`.
- **Legacy (<= 2025-11-25)** — the familiar `initialize` handshake. A request
  carrying no `_meta.io.modelcontextprotocol/protocolVersion` is treated as
  legacy, so old clients need no changes.

Observed behavior (both are covered by tests in `internal/mcpserver`):

- SDK client <-> our server: negotiates **`2026-07-28`** via `server/discover`.
- Raw legacy `initialize` advertising `2024-11-05`: server answers
  **`2024-11-05`**. Byte-for-byte, that response is what the old server sent,
  minus the capability differences noted below.

## Shape changes at the protocol level

These are the only wire-visible differences. Everything else — tool names,
argument names, result text formatting — is unchanged.

1. **Tool errors are now `isError` results, not JSON-RPC errors.**
   The old server answered every tool failure with a JSON-RPC error
   `{"code": -32000, "message": "<err>"}`. The spec says tool execution errors
   belong in the result with `isError: true` so the model can see and correct
   them. The SDK enforces that. The error *text* is preserved (e.g.
   `get document: ...`).

2. **Unknown tool is `-32602`, not `-32000`.**
   "Errors in finding the tool" remain protocol errors; the SDK uses
   `InvalidParams (-32602)` with message `unknown tool "kb_bogus"`.

3. **Arguments are now validated against the published schema.**
   Previously a `tools/call` with `{}` for `kb_search` silently searched for the
   empty string; now it returns
   `isError: true` with `validating "arguments": validating root: required: missing properties: ["query"]`.
   Enum violations and type mismatches are rejected the same way. Unknown extra
   properties are still accepted (no `additionalProperties: false`).

4. **Capability flags moved.**
   Old: `tools {listChanged:false}`, `resources {listChanged:true, subscribe:false}`,
   `prompts {listChanged:false}`.
   New: all three advertise `listChanged:true`, which is accurate — the SDK does
   emit list-changed notifications. The SDK's historical default of also
   advertising `logging` is explicitly suppressed (`ServerOptions.Capabilities`
   set to an empty value), matching the old server, which never implemented
   logging. Logging is deprecated as of `2026-07-28` anyway.

5. **`resources/list` is served by middleware.**
   The old server listed sources dynamically. The SDK only lists *statically*
   registered resources, so reads go through a `ResourceTemplate`
   (`kb://source/{sourceID}`) and `resources/list` is answered by a receiving
   middleware that queries the source table. URIs, names, descriptions and the
   `application/json` body are unchanged.

6. **`prompts/get` now works.**
   The old server advertised a `kb_context` prompt in `prompts/list` but never
   implemented `prompts/get`; a client that listed it and fetched it got
   `unknown method: prompts/get`. The listing shape is unchanged; `prompts/get`
   now returns the same prompt-ready context `kb_search_with_context` produces.

7. **Clean disconnect exits 0.**
   `Server.Run` maps stdin EOF / closed connection / cancelled context to a nil
   error, so `conduit mcp kb` exits 0 when an AI client detaches, as before.
   Without this the SDK's `server is closing: EOF` would surface as a cobra
   error and a non-zero exit on every normal shutdown.

## New tool: `kb_lexical_search`

Pure FTS5/BM25 keyword search. It is the only tool that bypasses
`HybridSearcher` entirely — it calls `kb.Searcher.Search` directly, so there is
no embedding lookup, no RRF fusion, no MMR/diversity filtering and no fallback
ladder. Results come back in raw BM25 order with the same citation fields
`kb_search` emits (`**Title** (score: N)`, `Path:`, snippet).

Arguments: `query` (required), `limit` (default 10, capped at 50), `source_id`.

The description is written for an agentic grep-style loop — search, read, refine
keywords, search again — and documents the query semantics the agent needs to
drive it: terms are ANDed, the last term is prefix-matched, FTS5 operator
characters are stripped, and BM25 scores are negative (more negative = more
relevant).

## Things found while porting (not changed)

- **Dead degraded-mode banner.** `toolSearch` in the old server built the string
  `"<mode> (degraded - semantic unavailable)"` and then discarded it — it never
  reached a client. The only degraded-mode signal that ever shipped was
  `result.Note`, appended to the "No results found for: ..." message on the
  empty-result path. That is preserved exactly. Surfacing the banner on
  non-empty results would change output for every search; it is a deliberate
  follow-up, not part of this port.

## Client compatibility

- **Claude Code / Cursor / VS Code / Gemini CLI** — no config change. The
  registered command is still `conduit mcp kb`; `conduit mcp configure` is
  untouched.
- **Older clients (2024-11-05 through 2025-11-25)** — supported via the legacy
  `initialize` path, verified by a raw-frame subprocess test.
- **Clients that treat a JSON-RPC error as fatal** — will now see `isError`
  results instead for tool failures, which is strictly friendlier.
- **Prompt-tuned clients** — tool descriptions and result text are byte-identical;
  `TestToolDescriptionsCarriedOverVerbatim` pins the six original descriptions.

## Stdout purity

The stdio transport owns `os.Stdout`. Nothing in `internal/mcpserver` (or
anything it calls) may write there. Guards:

- `ServerOptions.Logger` is left nil, so the SDK emits no diagnostics of its own.
- All Conduit logging goes through zerolog, whose global logger writes to stderr.
- Cobra writes command errors and usage to stderr.
- `TestStdoutIsProtocolPure` runs the server as a real subprocess with debug
  logging enabled, drives it with raw JSON-RPC frames, and fails if any stdout
  line is not a JSON-RPC 2.0 frame.

## File map

```
internal/mcpserver/server.go        Server type, New/Run/Connect, clean-shutdown handling
internal/mcpserver/tools.go         Tool definitions: verbatim descriptions + hand-written schemas
internal/mcpserver/handlers.go      Tool handlers (behavior ported 1:1)
internal/mcpserver/resources.go     kb://source/{sourceID} template + resources/list middleware
internal/mcpserver/prompts.go       kb_context prompt
internal/mcpserver/server_test.go   Handshake, tools/list, tools/call, error shapes, resources, prompts
internal/mcpserver/stdio_test.go    Subprocess stdio test: stdout purity + legacy 2024-11-05 handshake
```

`internal/kb/mcp_server.go` was deleted. No `--legacy` escape hatch was kept:
reinstating the old server would have meant carrying all 862 lines, well past
the 20-line bar for a trivial fallback.
