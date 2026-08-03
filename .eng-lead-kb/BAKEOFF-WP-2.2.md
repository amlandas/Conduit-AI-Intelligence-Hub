# WP-2.2 Embedding Model Bake-Off

**Date:** 2026-08-02
**Base:** `50ae62b` (tip of `v2`)
**Scope:** pick the default embedding model for the managed `llama-server` sidecar.
**Decision owner:** engineering manager. This document recommends; it does not decide.

---

## TL;DR

**Recommended default: `nomic-embed-text-v1.5` (F16 GGUF, 768-dim).**

Qwen3-Embedding-0.6B scored highest on the local retrieval eval (MRR 0.953 vs
0.880), but the margin comes from a 16-query corpus where a single query's rank
moves MRR by ~0.02 — it is not a reliable quality signal. Against that soft
advantage, Qwen3 costs 2.3x the download, 33% wider vectors, 4.4x the
parameters, and carries three open or unresolved llama.cpp-specific hazards.
nomic-embed-text v1.5 has no open llama.cpp correctness defect, is the smallest
and fastest candidate, and is the only one whose first-run download is small
enough to feel free.

Keep `qwen3-embedding-0.6b` in the registry as an opt-in upgrade, gated on
WP-3.3 verifying the GGUF conversion.

---

## 1. Research: current status of the llama.cpp embedding-quality issues

Researched 2026-08-02 against the GitHub API, llama.cpp master source, and
HuggingFace model metadata.

### 1.1 Two premises in the work-package brief were wrong

The brief asked about "#14234/#20085 (Qwen3-Embedding, ~20% retrieval-accuracy
loss reported in 2026)". Both halves of that need correcting:

- **The ~20% figure is from June 2025, not 2026.** It comes from
  [#14234](https://github.com/ggml-org/llama.cpp/issues/14234), opened
  **2025-06-17** and **closed the same day**.
- **[#20085](https://github.com/ggml-org/llama.cpp/issues/20085) is a different
  bug entirely.** It is "Qwen3-Embedding-0.6B with `--reranking` and
  `--embedding` both on returns all zeroes" (opened 2026-03-03, closed
  2026-03-05 as user misconfiguration). It contains no accuracy percentages.

**No 2026 reproduction of a 20% Qwen3-Embedding retrieval loss exists.** The
figure traces to one 2025 report with a known, simple cause.

### 1.2 #14234 — Qwen3-Embedding: the real root cause is a missing EOS token

The reporter was **already using `--pooling last`**, so pooling was not the
cause. ggerganov's diagnosis (2025-06-17), verbatim:

> Looks like you have to add the EOS token manually like this:
> `"input": ["test<|endoftext|>"],`

The reporter confirmed and closed. So of the candidate causes the brief listed:

| Candidate cause | Verdict |
|---|---|
| Missing appended EOS token | **Confirmed root cause** |
| Pooling type (last vs mean) | Not the cause here — already correct — but still must be passed explicitly |
| Instruction/query prefix | Separate, smaller factor (~1–5% per Qwen's own README) |
| F16 vs Q8 quantization | Not implicated |

**Is it fixed today? Probably not, and the official GGUF is stale.**
`Qwen/Qwen3-Embedding-0.6B-GGUF` was last modified **2025-07-14** and has never
been re-uploaded. Its HF discussion
[#17 "Outdated GGUF"](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/discussions/17)
is unresolved, with users reporting (2025-11-30) that *"without appending
`<|endoftext|>` to the end of every prompt the quality of matching here in
llama.cpp is terrible"*. Reading current-master `convert_hf_to_gguf` shows no
`add_eos_token` handling for Qwen3-Embedding, and the upstream
`tokenizer_config.json` has no `add_eos_token` key. Conversion fix
[711d5e6](https://github.com/ggml-org/llama.cpp/commit/711d5e6) (2025-08-02,
pre-tokenizer hash) post-dates the published GGUF — another reason to
self-convert.

**Conduit's response:** `ModelSpec.InputSuffix` carries `<|endoftext|>` for
Qwen3 and is appended to every input by both providers. A registry test pins it.

### 1.3 #19040 — EmbeddingGemma: closed, was never a code bug

[#19040](https://github.com/ggml-org/llama.cpp/issues/19040) (opened 2026-01-23,
**closed 2026-02-28**, `completed`) is a duplicate of
[#16538](https://github.com/ggml-org/llama.cpp/issues/16538). Two independent
causes, both configuration/conversion rather than llama.cpp defects:

1. **Missing sentence-transformers dense modules in the GGUF conversion.** A
   correct EmbeddingGemma GGUF has 316 tensors; the broken ones have 314,
   missing `dense_2.weight` and `dense_3.weight`. Maintainer `danbev` measured
   cosine similarity vs the reference at **-0.015** without them and **1.000**
   with them. Fixed by converting with `--sentence-transformers-dense-modules`
   ([PR #16367](https://github.com/ggml-org/llama.cpp/pull/16367), merged
   2025-10-09).
2. **Wrong pooling.** EmbeddingGemma's `1_Pooling/config.json` specifies
   **mean**; the reporter used `--pooling cls`. Switching to mean reproduced
   reference results.

**Live trap worth recording:** llama.cpp's convenience flag
`--embd-gemma-default` still points at `ggml-org/embeddinggemma-300M-qat-q4_0-GGUF`,
which is one of the variants **without** dense modules (302,863,104 params vs
307,581,696 for a correct build). Using the shortcut silently yields
non-reference embeddings.

EmbeddingGemma is not a WP-2.2 candidate, but the lesson generalises and is why
`ModelSpec` pins pooling and prefixes rather than relying on GGUF metadata.

### 1.4 llama-server `/v1/embeddings`: current state

- **Batching works.** `input` accepts a string array. Since
  [PR #14645](https://github.com/ggml-org/llama.cpp/pull/14645) (merged
  2025-07-12) pooled mode returns exactly one vector per input entry. Conduit's
  client batches at 32 by default.
- **`--pooling` defaults to the model's GGUF metadata**, and to `none` when that
  metadata is absent — in which case `/v1/embeddings` fails or returns per-token
  vectors. **Always pass it explicitly.** The sidecar does.
- **The `-ub` trap is real and still unfixed.** In embedding mode attention is
  non-causal, so an entire input must fit in one physical ubatch; `-ub` does not
  split. Oversized inputs return HTTP 500 *"input (N tokens) is too large to
  process. increase the physical batch size"*. There is still **no startup
  validation**: [PR #18123](https://github.com/ggml-org/llama.cpp/pull/18123) is
  open, [PR #24290](https://github.com/ggml-org/llama.cpp/pull/24290) was closed
  unmerged, and [#25293](https://github.com/ggml-org/llama.cpp/issues/25293)
  (opened 2026-07-04, **still open**) documents someone losing days to it.
  **Conduit sets `-b` and `-ub` equal to `-c`.**
- **The OpenAI `dimensions` parameter is silently ignored.**
  [#25210](https://github.com/ggml-org/llama.cpp/issues/25210) is open;
  [PR #24898](https://github.com/ggml-org/llama.cpp/pull/24898) is unmerged.
  Matryoshka truncation (relevant to nomic v1.5) must be done client-side with
  re-normalisation. Conduit does not currently truncate.
- **Open, unresolved, worth watching:**
  [#26282](https://github.com/ggml-org/llama.cpp/issues/26282) (2026-07-29)
  reports embeddings drifting slightly (L2 ~0.02 on normalised vectors) between
  cold start and warm state. If it generalises it would mean vectors written at
  index time differ from vectors computed at query time — exactly the kind of
  silent degradation that is expensive to detect later. **Recommend WP-3.4 add a
  determinism check: embed a fixed probe string at daemon start and after N
  requests, and alarm on drift.**

### 1.5 Candidate summary

| | nomic-embed-text v1.5 | Qwen3-Embedding-0.6B | mxbai-embed-large-v1 |
|---|---|---|---|
| Dims | 768 | 1024 | 1024 |
| Params | 137M | 596M | 335M |
| Artifact | 274 MB (F16) | 639 MB (Q8_0) | 670 MB (F16) |
| GGUF ctx | 2048 | 32768 | 512 |
| Pooling | mean | **last** | cls |
| Prefixes | `search_document: ` / `search_query: ` (**mandatory**) | `Instruct: …\nQuery: ` on queries | optional query prefix |
| EOS suffix | no | **`<|endoftext|>` required** | no |
| License | Apache-2.0 | Apache-2.0 | Apache-2.0 |
| Open llama.cpp defects | none found | stale GGUF, `--reranking` conflict, sm_70 NaN wedge | none found |
| Repo freshness | 2025-04-28 | **2025-07-14 (stale)** | 2024-04-07 |

Caveat on nomic: the GGUF declares `context_length: 2048` although the model
supports 8192. Reaching 8192 needs `-c 8192 -b 8192 --rope-scaling yarn
--rope-freq-scale .75`, and llama.cpp's YaRN is a *substitute* for the Dynamic
NTK-Aware RoPE scaling `transformers` uses, not an exact match. **The registry
pins 2048**, which is the safe, reference-faithful setting and is ample for
Conduit's ~1000-character chunks (~250 tokens).

---

## 2. Local retrieval eval

### 2.1 What actually ran — and what did not

**`llama-server` is NOT installed on this machine.** `FindLlamaServer` searched
PATH, `/opt/homebrew/bin`, `/usr/local/bin` and `/usr/bin` and found nothing;
Homebrew has `ollama` but not `llama.cpp`.

So the eval ran through the **local Ollama daemon** (llama.cpp under the hood),
which the work package explicitly permits as a comparison reference. All three
candidates were pulled locally and are genuinely the models named:

| Ollama model | dims | quant |
|---|---|---|
| `nomic-embed-text:latest` | 768 | F16 |
| `qwen3-embedding:0.6b` | 1024 | Q8_0 |
| `mxbai-embed-large:latest` | 1024 | F16 |

**Read these numbers as characterising the models, not the llama-server sidecar
path.** Ollama does its own tokenization, templating and pooling. In
particular, llama.cpp-specific hazards — `--pooling` defaults, the `-ub` trap,
`--reranking` conflicts — are precisely what Ollama papers over. **The
llama-server numbers in this table do not exist and must not be inferred.**

### 2.2 Method

- **Corpus:** `internal/kb/testdata/corpus/` (7 documents, 6.5 KB), split on
  blank lines into **18 paragraph chunks**, with each document's title line
  merged into its first body paragraph. The corpus was read only; no golden
  test file was modified.
- **Scoring:** cosine similarity over L2-normalised vectors, full-pool ranking.
- **Metrics:** hit@1, hit@3, MRR. Random-baseline hit@3 ≈ 0.167.
- **Two query sets**, both written by hand against the corpus:
  - **Set A (15 queries)** — natural questions sharing vocabulary with the gold
    chunk.
  - **Set B (16 queries)** — paraphrase-only, minimal lexical overlap, with
    deliberate distractors: chunks 13 and 16 are byte-identical boilerplate
    (either is accepted); chunk 15 mentions a lantern competing with chunk 12;
    chunks 0 and 4 both contain "all men are created equal".
- **Ablations** on prefixes and the EOS suffix, to test the research claims
  rather than assume them.

Harness: `bakeoff.py` / `bakeoff2.py` (scratchpad, not committed — the full
query sets and method above are sufficient to reproduce).

### 2.3 Set A results — saturated, not informative

| Config | hit@1 | hit@3 | MRR |
|---|---|---|---|
| mxbai-embed-large [spec] | 0.933 | 0.933 | 0.950 |
| mxbai-embed-large [no prefix] | 0.933 | 0.933 | 0.947 |
| qwen3-embedding [Instruct + EOS] | 0.867 | 1.000 | 0.922 |
| qwen3-embedding [Instruct, no EOS] | 0.867 | 1.000 | 0.922 |
| nomic-embed-text [task prefixes] | 0.867 | 1.000 | 0.911 |
| nomic-embed-text [no prefixes] | 0.867 | 1.000 | 0.911 |
| qwen3-embedding [naive] | 0.867 | 0.867 | 0.893 |

Everything lands between 0.89 and 0.95 MRR, and the nomic and qwen3 prefix
ablations produce **byte-identical rank vectors**. Set A cannot distinguish
these models. Reported for completeness only.

### 2.4 Set B results — the discriminating run

| Config | hit@1 | hit@3 | MRR |
|---|---|---|---|
| **qwen3-embedding [Instruct + EOS]** | **0.938** | 0.938 | **0.953** |
| qwen3-embedding [Instruct, no EOS] | 0.938 | 0.938 | 0.945 |
| **nomic-embed-text [task prefixes]** | 0.812 | 0.938 | 0.880 |
| qwen3-embedding [naive: no prefix/EOS] | 0.812 | 0.938 | 0.871 |
| mxbai-embed-large [query prefix] | 0.750 | **1.000** | 0.865 |
| nomic-embed-text [no prefixes] | 0.812 | 0.938 | 0.865 |
| mxbai-embed-large [no prefix] | 0.688 | **1.000** | 0.823 |

**Findings:**

1. **Prefixes matter, consistently and in the predicted direction.** This is the
   most robust result in the whole eval:
   - qwen3: MRR **0.953 → 0.871** without the Instruct prefix (−0.082)
   - mxbai: **0.865 → 0.823**, hit@1 0.750 → 0.688 (−0.042)
   - nomic: **0.880 → 0.865** (−0.015)

   Encoding prefixes in the registry rather than leaving them to callers is
   therefore load-bearing, not decorative.

2. **The Qwen3 EOS suffix has a small, real, single-query effect.** MRR 0.953 vs
   0.945; the difference is one query moving from rank 4 to rank 8. Directionally
   consistent with #14234, but **one query is not evidence** — do not cite this
   as confirmation.

3. **A direct probe confirms the suffix is not a no-op** on the Ollama path:

   ```
   qwen3-embedding:0.6b   cos(embed("test"), embed("test<|endoftext|>")) = 0.974617
   nomic-embed-text       cos(embed("test"), embed("test<|endoftext|>")) = 0.661698
   ```

   The nomic number is the important one: it is a BERT model for which
   `<|endoftext|>` is **not** a special token, so the literal string is
   tokenized as text and badly perturbs a short input. **This validates making
   `InputSuffix` per-model rather than global** — applying Qwen3's suffix
   universally would actively damage the other two models.

4. **mxbai has the best hit@3 (1.000) but the worst hit@1 (0.750).** It reliably
   surfaces the right chunk but ranks it poorly. For Conduit — which returns raw
   chunks to an AI client that re-reads them — hit@3 has real value, but hit@1
   and MRR matter more for the top-of-list quality users perceive.

### 2.5 Honest limits of this eval

- **n = 16 queries, 18 chunks.** One query changing rank moves MRR by ~0.02.
  Only the prefix ablations (0.04–0.08) exceed plausible noise. **The
  qwen3-vs-nomic gap of 0.073 is at the edge of meaningful and should not by
  itself decide the default.**
- **Ollama, not llama-server.** See 2.1.
- **The corpus is 6.5 KB of public-domain prose.** It is not representative of
  the code, notes and technical documentation Conduit actually indexes.
- **No latency or memory measurements** — a fair comparison needs the sidecar
  path, which needs llama-server installed.

**Recommended follow-up (WP-3.3, once llama-server is installed):** re-run Set B
through the sidecar, add a 500+ chunk technical corpus, and measure
embed-throughput and RSS per model.

---

## 3. Recommendation

**Default: `nomic-embed-text-v1.5`.** Rationale, in priority order:

1. **First-run cost.** 274 MB versus 639 MB. The entire point of WP-2.2 is that
   Conduit works with zero external services; the first-run download is the
   moment that promise is kept or broken. Qwen3 costs 2.3x for a quality
   difference this eval cannot reliably measure.
2. **Risk.** nomic has no open llama.cpp correctness defect. Qwen3 has a stale
   never-re-uploaded GGUF, an unresolved upstream discussion thread, a silent
   all-zeros failure when combined with `--reranking`, and a NaN wedge on
   sm_70 CUDA.
3. **Index size.** 768 vs 1024 dims is 25% off every stored vector — directly
   relevant to WP-2.1's sqlite-vec work.
4. **Speed and RAM.** 137M vs 596M parameters, which dominates CPU-only cold
   start and steady-state memory.
5. **Quality is adequate.** hit@3 0.938 on the hard set, with the top result
   correct 81% of the time, on a corpus where the ceiling is ~0.95.

**Keep `qwen3-embedding-0.6b` registered as an opt-in upgrade**, not the
default. Before it can be promoted, WP-3.3 should:

- verify or self-convert the GGUF (`gguf-dump | grep add_eos`, and confirm
  `pooling_type`), and
- empirically confirm `embed("test") != embed("test<|endoftext|>")` through
  **llama-server**, not Ollama.

**`mxbai-embed-large-v1` should stay a control only.** It is the largest
artifact, has the weakest hit@1, and its 512-token context is the tightest of
the three.

---

## 4. What this work package changed as a result

| Finding | Code response |
|---|---|
| Pooling defaults silently to `none`/wrong (#19040, §1.4) | `--pooling` always passed explicitly, from `ModelSpec.Pooling` |
| `-ub` must hold the whole sequence (#25293, open) | `-b` and `-ub` set equal to `-c` |
| Qwen3 needs an appended EOS (#14234) | `ModelSpec.InputSuffix`, applied per-model by both providers |
| Applying that suffix universally would hurt BERT models (§2.4 probe) | `InputSuffix` is per-model; empty for nomic and mxbai |
| Prefixes are worth 0.04–0.08 MRR (§2.4) | Prefixes live in the registry, not in caller code; pinned by test |
| Sidecar must never leave the loopback | `--host 127.0.0.1` hard-coded; a test asserts no `0.0.0.0`/`::` |
| Artifacts must be verifiable | Registry pins exact SHA-256 and byte size per model |

**On SHA-256:** these were obtained from the HuggingFace model-tree API, where
the LFS `oid` **is** the file's SHA-256 — so the registry is pinned to exact,
verifiable artifacts without downloading them. (A parallel research pass
concluded these hashes were unavailable from the API; that is incorrect, and the
`/api/models/{repo}/tree/main` endpoint returns them under `lfs.oid`.)

Two of the three pins were then **independently confirmed** against Ollama's
content-addressed blob store, which had pulled the same upstream artifacts
during this eval:

| Model | Registry pin | Ollama blob |
|---|---|---|
| `qwen3-embedding-0.6b` | `06507c7b4268…c3e439` | `sha256-06507c7b4268…c3e439` ✅ |
| `mxbai-embed-large-v1` | `819c2adf5ce6…e39c3d` | `sha256-819c2adf5ce6…e39c3d` ✅ |
| `nomic-embed-text-v1.5` | `f7af6f66802f…c2fdb` | `sha256-970aa74c0a90…ef0e6` (Ollama ships its own conversion — no match expected) |

**Incidental confirmation of the `-ub` analysis:** Ollama's own internal
invocation is
`llama-server --embedding --host 127.0.0.1 -c 32768 -b 2048 -ub 2048 …` for
Qwen3 — i.e. it runs with `-ub` well below `-c` and chunks inputs itself rather
than relying on llama-server to split them. Conduit's sidecar instead sets
`-b = -ub = -c`, which is the safer configuration for a server we do not
pre-chunk for.

---

## Sources

- [#19040](https://github.com/ggml-org/llama.cpp/issues/19040) ·
  [#16538](https://github.com/ggml-org/llama.cpp/issues/16538) ·
  [PR #16367](https://github.com/ggml-org/llama.cpp/pull/16367)
- [#14234](https://github.com/ggml-org/llama.cpp/issues/14234) ·
  [#20085](https://github.com/ggml-org/llama.cpp/issues/20085) ·
  [PR #14645](https://github.com/ggml-org/llama.cpp/pull/14645)
- [#25293](https://github.com/ggml-org/llama.cpp/issues/25293) ·
  [PR #18123](https://github.com/ggml-org/llama.cpp/pull/18123) ·
  [#25210](https://github.com/ggml-org/llama.cpp/issues/25210) ·
  [#26282](https://github.com/ggml-org/llama.cpp/issues/26282) ·
  [#26044](https://github.com/ggml-org/llama.cpp/issues/26044)
- [llama-server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)
- [Qwen3-Embedding-0.6B-GGUF discussion #17](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/discussions/17)
- [nomic-embed-text-v1.5-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF) ·
  [ChristianAzinn/mxbai-embed-large-v1-gguf](https://huggingface.co/ChristianAzinn/mxbai-embed-large-v1-gguf)
