# WP-2.1 — Vector storage benchmarks

**Date:** 2026-08-03
**Branch base:** `v2` @ `50ae62b`
**Budget:** p95 vector query < 100 ms at 50K × 768

## Machine

| | |
|---|---|
| CPU | Apple M5 Max (18 cores) |
| Memory | 128 GB |
| OS | macOS 26.6 (darwin/arm64) |
| Go | go1.26.5 |
| SQLite | mattn/go-sqlite3 v1.14.49, amalgamation 3.53.4 |
| Build | `CGO_ENABLED=1 -tags fts5` |

Single-machine numbers on fast hardware. The headroom discussion below matters
more than the absolute values — see *Does this hold on slower hardware?*

## Result: budget met

`BenchmarkVectorSearch`, unfiltered top-10 over 768-dim vectors, including the
enrichment join back to `kb_chunks` / `kb_documents`.

| Corpus | mean | p50 | **p95** | max | budget |
|---|---|---|---|---|---|
| 5,000 × 768 | 5.87 ms | 5.84 ms | **6.13 ms** | 6.14 ms | ✅ |
| 25,000 × 768 | 28.3 ms | 28.7 ms | **29.0 ms** | 29.2 ms | ✅ |
| 50,000 × 768 | 56.5 ms | 57.1 ms | **59.4 ms** | 63.5 ms | ✅ 1.7× headroom |

Scaling is linear in corpus size, as expected for an exact scan.

### Filtered search

`BenchmarkVectorSearchFiltered`, same queries with `SourceIDs: ["src_b"]`
(25% of the corpus). The filter is a SQL predicate evaluated *before* the
distance is computed, so it shrinks the scan rather than discarding results
afterwards:

| Corpus | mean | p50 | **p95** | vs unfiltered |
|---|---|---|---|---|
| 5,000 × 768 | 1.64 ms | 1.63 ms | **1.84 ms** | 3.6× faster |
| 25,000 × 768 | 7.97 ms | 8.21 ms | **8.48 ms** | 3.6× faster |
| 50,000 × 768 | 16.3 ms | 16.3 ms | **17.3 ms** | 3.4× faster |

Cost tracks the filtered row count almost exactly — a multi-source knowledge
base gets cheaper queries, not more expensive ones.

### Write path

`BenchmarkVectorUpsert` — one transaction carrying a 100-chunk document:

| | |
|---|---|
| 100 vectors / transaction | 314 µs |
| per vector | ~3.1 µs |

Negligible next to the embedding call it follows (Ollama: tens of ms per chunk).

Reproduce:

```
CGO_ENABLED=1 go test -tags fts5 -run XXX -bench BenchmarkVector \
  -benchtime 30x ./internal/kb/
```

## Decision: BLOB + pure-Go cosine scan, not sqlite-vec

The work package pre-authorised `github.com/asg017/sqlite-vec-go-bindings/cgo`
and named the BLOB scan as the fallback *if the binding proved unusable*. Both
were built and measured before choosing. **The binding works. It was still not
chosen.** That reverses the expected default, so the evidence is recorded here
in full.

### The binding does work with mattn/go-sqlite3

The documented risk is that the two fight over SQLite linkage. On this machine
they do not, provided `sqlite_vec.Auto()` is called before opening a
connection:

```
vec_version: v0.1.6   sqlite_version: 3.53.4
fts5: ok   vec0: ok   knn ok
```

`sqlite_version` 3.53.4 is mattn's statically compiled amalgamation, not the
macOS system SQLite (3.4x) — so the auto-extension did register against the
right SQLite. An earlier spike that omitted `Auto()` failed with
`no such function: vec_version`, which is the failure mode that would otherwise
be misread as "the binding is unusable".

### Isolated scan comparison

Same 768-dim data, top-10, distance computation only (no enrichment join), so
the two engines are compared like for like:

| Corpus | sqlite-vec `vec0` | BLOB scan (naive) | BLOB scan (`RawBytes` + zero-copy) |
|---|---|---|---|
| 5,000 | 3.3 ms | 5.8 ms | 4.9 ms |
| 25,000 | 16.3 ms | 28.5 ms | 24.3 ms |
| 50,000 | 32.2 ms | 58.2 ms | 47.6 ms |

sqlite-vec is ~1.4× faster. Both are inside budget.

### Why the slower option won

1. **Filter exactness is a correctness property; 1.4× is a performance nicety
   inside budget.** `vec0` KNN takes `k` and returns `k`; filtering on a column
   requires declaring it as a partition or metadata column, and a `SourceIDs`
   *list* does not map cleanly onto either. The natural workaround — over-fetch
   then filter — loses recall exactly when the filter is selective. Measured:
   `TestVectorIndex_RankingIsExact` builds a corpus where 4 of 40 chunks belong
   to the rare source and the closest one ranks below many others. Exact
   pre-filtering returns all 4. A post-filtered top-10 would return 1. That is a
   silent 75% recall loss on a routine query, traded for 27 ms.

2. **Ordinary rows make the transaction requirement fall out for free.**
   `kb_vectors` is a plain table with `ON DELETE CASCADE` onto `kb_chunks`, so
   ingestion writes text, FTS and vectors in one transaction with no special
   handling, and deleting a chunk reclaims its vector without the deletion path
   knowing vectors exist. `vec0`'s shadow tables are transactional too, but
   carry no foreign key, so every delete path would need an explicit companion
   delete — a new way to leak orphaned vectors.

3. **`Auto()` is process-global.** It registers an auto-extension for *every*
   SQLite connection in the process, including ones opened by unrelated code and
   by the test binary. That is a wide blast radius for a 27 ms saving.

4. **Platform risk for a project that ships prebuilt binaries.** The linkage
   works here because mattn's static symbols win over the system `libsqlite3`.
   That is a linker-ordering property, verified on darwin/arm64 only, and the
   macOS SDK explicitly documents `sqlite3_auto_extension` as unsupported
   (it compiles with deprecation warnings on every build). Conduit already
   struggles to publish beyond macOS ARM64; adding a C dependency whose
   correctness rests on link order is the wrong direction.

5. **Zero new dependencies.** Removing Qdrant already dropped grpc, protobuf,
   genproto and `golang.org/x/net`. The SQLite index is written against the
   standard library, so WP-2.1 is a net dependency *reduction* with nothing
   added.

### If this decision needs revisiting

The seam is an interface. `VectorIndex` has one implementation today; a
`vec0`-backed one can be added without touching `SemanticSearcher`,
`HybridSearcher` or ingestion. The trigger would be a corpus target well above
50K, not the current 1.4×.

## Does this hold on slower hardware?

An M5 Max is near the top of consumer performance. The scan is memory-bandwidth
and scalar-FLOP bound, so a 2–3× slower machine is the relevant question:

| Machine | 50K p95 (est.) | 25K p95 (est.) |
|---|---|---|
| M5 Max (measured) | 59 ms | 29 ms |
| 2× slower | ~119 ms | ~58 ms |
| 3× slower | ~178 ms | ~87 ms |

**An unfiltered 50K query would exceed the budget on a machine ~1.7× slower
than this one.** Qualifiers:

- 50K is the documented *ceiling*, not the typical corpus. At 25K there is 3.4×
  headroom.
- Real queries are usually source-filtered, which is 3.4× cheaper: a filtered
  50K query is 17 ms here and stays inside budget even at 5× slower.
- Search already runs concurrently with FTS5 in `searchFusion`, so vector
  latency overlaps lexical latency rather than adding to it.

The honest statement: **budget met at the ceiling on this class of hardware,
with real headroom at typical corpus sizes, and a known ceiling-plus-slow-machine
combination that would need attention.** If that combination shows up, the cheap
fixes in order are (a) parallelise the scan across goroutines — WAL permits
concurrent readers, so N connections can each scan a rowid range, which this
18-core machine would absorb almost linearly; (b) quantise to int8, ~4× less
memory traffic for a small recall cost; (c) the `vec0` implementation behind the
existing interface.

None of these are worth building today.

## Verification

- `CGO_ENABLED=1 go build -tags fts5 ./...` — clean
- `CGO_ENABLED=1 go vet -tags fts5 ./...` — clean
- `CGO_ENABLED=1 go test -tags fts5 -race -count=1 ./...` — all packages pass
- Golden retrieval harness unchanged and green
