# Design — Pareto-Style Coding Model Router

A local, OpenAI-compatible HTTP proxy that routes `/v1/chat/completions` to the
cheapest OpenRouter model whose quality clears a continuous knob `p ∈ [0, 1]`,
costed by token-usage-weighted pricing. This document is the durable design of
record; it supersedes the original scratch `plan.md`.

## Goal & differences from OpenRouter's `pareto-code`

OpenRouter's `pareto-code` router picks among ~13 curated coding models using
three coarse quality tiers. This project improves on it three ways:

1. **Continuous knob, not tiers.** A single parameter `p ∈ [0, 1]`: `p=0` always
   selects the cheapest model, `p=1` the best.
2. **Full candidate set.** All models on Artificial Analysis's
   [coding-agents leaderboard](https://artificialanalysis.ai/agents/coding-agents),
   not a curated subset.
3. **Token-usage-weighted pricing.** Effective price per model = models.dev
   OpenRouter per-token prices weighted by AA's observed per-task token mix
   (input / cached / output), so verbose-but-cheap models are costed honestly.

## Settled design decisions

- **`p` semantics — quality threshold.** `p` is a floor on the min–max-normalized
  AA Coding Agent Index over the current candidate set. The router picks the
  **cheapest candidate at or above the floor** by effective $/task. This is
  automatically Pareto-optimal (anything cheaper with ≥ quality would also
  qualify). `p=0` → cheapest overall; `p=1` → top-ranked regardless of cost.
  Tie-break: higher quality, then stable by slug.
- **Shape — local OpenAI-compatible proxy.** Serves `/v1/chat/completions`,
  accepts the knob via model name (`pareto@0.7`) and/or header, rewrites the
  model field, forwards to OpenRouter with SSE streaming passthrough.
- **Stack — Go.** Single static binary, long-running local daemon, stdlib only.
- **Quality metric — AA Coding Agent Index** (0–1): equal-weight composite of
  SWE-Bench-Pro-Hard-AA, Terminal-Bench v2, and SWE-Atlas-QnA. The official AA
  Data API does **not** expose this index or per-task token usage at any tier
  (its `artificial_analysis_coding_index` is a *different*, LLM-level benchmark),
  so we scrape the leaderboard page; the AA API is at most an optional sanity
  check.
- **Harness rows → one candidate per model.** AA's rows are harness+model pairs
  (e.g. Opus appears under Claude Code, Cursor CLI, Opencode, and at multiple
  reasoning-effort levels). Collapse to one candidate per underlying model by
  keeping the row with the highest index score; that row's token mix prices it.
  Models not available on OpenRouter are dropped.
- **Cost model.**
  `cost/task = (meanInputTokens − meanCacheTokens)·inputPrice
              + meanCacheTokens·cacheReadPrice
              + meanOutputTokens·outputPrice`,
  where per-token price = per-1M price / 1e6. Missing cache-read price → 0.1×
  input-price heuristic (and the candidate is flagged).
- **AA attribution is required** wherever their data is displayed.

## Architecture

```
client (aider / Claude Code / curl)
   │  POST /v1/chat/completions  model="pareto@0.7"
   ▼
┌─ proxy (Go) ─────────────────────────────────┐
│ router engine: candidates → effective cost   │
│   → quality floor(p) → cheapest above floor  │
│ stickiness: session key → pinned             │
│   model+provider, TTL ~5 min                 │
│ fallback: models[] passed to OpenRouter +    │
│   transport-level retry; Retry-After honored │
│ data refresher (background, daily):          │
│   • AA page RSC payload (scores + token mix) │
│   • models.dev api.json (OpenRouter prices)  │
│   • disk-cached snapshot; stale data is OK   │
└──────────────┬───────────────────────────────┘
               ▼ forwards with rewritten model
          OpenRouter API
```

### Package layout

```
cmd/router/          CLI: subcommand dispatch + `snapshot` (M1) + `serve` (M3)
internal/snapshot/   Snapshot/Candidate types, NormalizedQuality, atomic Load/Save
                     — imports nothing internal; this is the engine seam (M2)
internal/aa/         AA page fetch + RSC flight decode + row extraction
internal/pricing/    models.dev parse (primary) + OpenRouter /models (fallback)
internal/refresh/    alias table, pure Build, validation, Refresh orchestrator
internal/engine/     (M2) pure Select(snapshot, p, opts) → routing plan
internal/proxy/      (M3) OpenAI-compatible server, SSE passthrough, stickiness
```

Dependency direction: `refresh → {aa, pricing, snapshot}`; `snapshot` imports
nothing internal. M2's `engine` imports only `snapshot` (pure, no I/O). M3's
`proxy` imports `engine` + `refresh`.

## Data layer (M1)

### Sources & shapes (verified 2026-06-09)

- **AA leaderboard** — `https://artificialanalysis.ai/agents/coding-agents`,
  ~595 KB HTML via plain `curl` (no JS). The dataset is a Next.js RSC flight
  payload spread across ~47 `self.__next_f.push([1,"…"])` script chunks.
  Decoding: extract each push's JSON string literal in document order,
  JSON-unescape, concatenate (~317 KB text), locate the single `"rows":[` array,
  stream-decode it. ~21 rows. Per row we use:
  `hostModelSlug, displayLabel, agentName, indexScore,
  mean{inputTokens, outputTokens, cacheTokens, cacheHitRate, costUsd}`.
  **`hostModelSlug` is not unique** — it repeats across harnesses and
  reasoning-effort levels (`(max)`/`(medium)`/`(xhigh)` in `displayLabel`); a
  separate slug (`openai_gpt-5-5-medium`) can denote the same model.
- **models.dev** — `https://models.dev/api.json`, ~2.2 MB, keyed by provider. The
  `openrouter` provider has ~339 models keyed by OpenRouter slug, each with
  `cost{input, output, cache_read, cache_write}` in **USD per 1M tokens
  (numbers)**. ~45% carry `cache_read`. Eight models carry `cost.tiers` for
  >200k-context pricing (ignored in v1).
- **OpenRouter `/api/v1/models`** (fallback price source) — pricing as
  **per-single-token strings** under `pricing.prompt`/`pricing.completion`
  (× 1e6 to normalize; `prompt→input`, `completion→output`).

### Alias table (AA slug → OpenRouter ID)

Committed Go map; `""` value = deliberate drop. Most slugs map mechanically
(`vendor_model-N-M` → `vendor/model.N.M`); the rest are hand-curated:

| AA hostModelSlug | OpenRouter ID | note |
|---|---|---|
| anthropic_claude-opus-4-8 | anthropic/claude-opus-4.8 | |
| anthropic_claude-opus-4-7 | anthropic/claude-opus-4.7 | |
| anthropic_claude-opus-4-6 | anthropic/claude-opus-4.6 | |
| anthropic_claude-sonnet-4-6 | anthropic/claude-sonnet-4.6 | |
| openai_gpt-5-5 | openai/gpt-5.5 | |
| openai_gpt-5-5-medium | openai/gpt-5.5 | duplicate model; deduped by collapse |
| openai_gpt-5-4 | openai/gpt-5.4 | |
| alibaba_cloud_qwen3-7-plus | qwen/qwen3.7-plus | vendor prefix differs |
| friendliai_glm-5-1 | z-ai/glm-5.1 | AA prefixes hosting provider, not creator |
| moonshot_kimi-k2-6 | moonshotai/kimi-k2.6 | vendor prefix differs |
| deepseek_deepseek-v4-pro-1m | deepseek/deepseek-v4-pro | `-1m` = same 1M-context model |
| google_gemini-3-1-pro_ai-studio | google/gemini-3.1-pro-preview | no GA slug; `_ai-studio` is AA's endpoint qualifier |
| cursor_composer-2, -2-5, -2-5-fast | *(drop)* | Cursor-only; not on OpenRouter |

Resolution order: exact alias hit → mechanical fallback **verified against the
price-table keys** → otherwise an `unmapped` drop with a loud stderr warning
(never a silent guess).

### Build pipeline (pure)

`Build(rows, prices, fetchedAt) → (Snapshot, warnings, error)`:
validate rows → resolve aliases → collapse by OpenRouter ID (keep max index
score; losers recorded as `duplicate-of:…`) → extract effort label
(`\(([a-z-]+)\)\s*$`) → look up prices (missing cache-read → 0.1× input, flag) →
compute `CostPerTaskUSD` (clamp uncached ≥ 0) → cross-check vs AA's observed
`mean.costUsd` → sort ascending by cost.

`CostPerTaskUSD` is **stored** in the snapshot (set-independent; CLI, engine, and
tests then read identical numbers). **Normalized quality is not stored** — it is
set-dependent, so `snapshot.NormalizedQuality(cands)` recomputes min–max over
whatever candidate subset the caller selects from.

### Validation rules

- ≥ 15 raw AA rows (21 today) else fail.
- Per row: non-empty slug/label/agent; `indexScore ∈ (0,1]`;
  `mean.input/output/costUsd > 0`; `cacheTokens ∈ [0, inputTokens]` else fail.
- Max index score ≥ 0.3 (guards against a silent quality-scale change) else fail.
- ≥ 8 surviving candidates (11 today) else fail.
- Cost cross-check ratio `cost/AAcost` outside [0.5, 2] → warn; outside
  [0.05, 20] → fail (that's a unit-conversion bug, not market noise).
- Bad prices on a candidate → drop it + warn (then re-check the ≥8 floor).

### Snapshot store

Atomic write (`CreateTemp` → write → `Sync` → `Rename`) at
`os.UserCacheDir()/coding-model-router/snapshot.json`. `Load` errors on a
`SchemaVersion` mismatch. `Refresh` never deletes or overwrites the cache on
failure — it returns the last-good snapshot flagged `stale=true` plus the causal
error.

### Effective frontier (snapshot of 2026-06-09, for reference)

Effective $/task: deepseek 0.35 · qwen 0.50 · gemini-3.1-pro 0.93 · gpt-5.4 1.11
· sonnet-4.6 1.23 · kimi 1.36 · glm-5.1 1.58 · opus-4.6 2.02 · opus-4.7 4.21 ·
gpt-5.5 4.33 · opus-4.8 5.49. Pareto frontier (6 of 11 dominated):
deepseek → qwen → gpt-5.4 → opus-4.7 → opus-4.8. Selection is monotonic in `p`.
(With today's data, `p > 0.31` resolves to only two models — see "knob spreading"
in deferred features.)

## Future milestones (designed; not yet built)

- **M2 — engine.** Pure function importing only `internal/snapshot`:
  `Select(s *snapshot.Snapshot, p float64, opts Options) (Plan, error)`; `Plan`
  = primary OpenRouter ID + ordered fallbacks (remaining qualifiers by ascending
  cost). Unit-tested for: cheapest at `p=0`, best at `p=1`, monotonic
  non-decreasing cost in `p`, dominated models never chosen.
- **M3 — proxy.** `serve` subcommand. Knob parsing: `pareto@0.7` model name; bare
  `pareto` → configured default `p`; `X-Pareto-P` header overrides; malformed →
  400; any other model name → transparent passthrough. SSE passthrough with
  mid-stream-error chunk inspection; usage extracted from the final chunk.
  `SnapshotProvider interface{ Current() *snapshot.Snapshot }` seam; background
  refresher goroutine wraps `refresh.Refresh` (daily; stale-OK with loud warning).
- **M4 — resilience + observability.** Stickiness keyed by `X-Session-Id` (else a
  hash of system prompt + first user message); in-memory store, sliding ~5 min
  TTL; pins model **and provider**; refreshes apply to new sessions only
  (hysteresis). Fallback via OpenRouter's `models[]` array + transport-level
  retry; honor `Retry-After`. Per-request log: chosen model, `p`, effective cost
  estimate, actual `usage.cost`, fallback hops (from `openrouter_metadata`).
- **M5 — polish.** README usage examples, license.

## OpenRouter mechanics (verified from docs, for M3/M4)

- **Native fallback** — request param `models: []` (priority order); OpenRouter
  retries on *any* error (provider down, rate limit, moderation, context
  overflow) and reports/bills the model that actually served via the response
  `model` field. Plan: send `model` + `models[]` = the engine's fallback list
  (cap ~3). Honor `Retry-After` on 429/503.
- **Provider pinning** — `provider: {order: ["slug"], allow_fallbacks: false}`;
  503 if the pinned provider is down → proxy re-routes fresh and re-pins.
- **Usage** — now automatic in every response (legacy `usage.include` /
  `stream_options.include_usage` deprecated); arrives in the final SSE chunk
  before `[DONE]`; cached tokens in `usage.prompt_tokens_details.cached_tokens`;
  `usage.cost` in credits. Serving provider + per-attempt fallback log via the
  request header `X-OpenRouter-Metadata: enabled` → `openrouter_metadata` on the
  final chunk.
- **Mid-stream errors** — after a 200 + streamed tokens, an error arrives as an
  SSE chunk with a top-level `error` and `finish_reason: "error"`. Inspect every
  chunk, not just the HTTP status. Not transparently retryable; surface to the
  client and log. Error body everywhere: `{"error":{code,message,metadata}}`;
  502 = provider down, 503 = no provider meets routing requirements.
- **Auth** — forward a client-supplied `Authorization: Bearer sk-or-…` if
  present, else inject `OPENROUTER_API_KEY` from env. Send attribution headers
  `HTTP-Referer` / `X-OpenRouter-Title`.

## Deferred features

Adaptive `p=auto` via a difficulty classifier · budget governor + cost ledger ·
personal calibration from task outcomes · shadow / A-B mode · speed axis
(`:nitro`-style) · capability filters (context window, tool-calling,
open-weights, data policy) · **(model, reasoning-effort) pairs as distinct
candidates + forwarding `reasoning.effort`** (AA scores efforts separately;
`Candidate.Effort` is stored now to enable this) · **knob spreading** via
rank-interpolated floors (so mid-range `p` spans more than two models) ·
cache-**write** costs (Anthropic ~1.25× input; currently ignored — biases all
Anthropic rows equally) · >200k-context tier pricing (`cost.tiers`).

## Risks

- **Scraper fragility** (low–moderate): the RSC payload shape is standard Next.js
  App Router output; field names change only on a site refactor. Mitigated by the
  cached last-good snapshot, schema validation on refresh, loud staleness
  warnings, and `//go:build live` shape-contract tests.
- **Alias-table staleness = the #1 ongoing maintenance cost.** New AA models with
  novel slugs become loud `unmapped` warnings rather than silently vanishing; the
  ≥8-candidate floor is the backstop.
- **Effort mismatch:** a candidate's quality reflects its *best-scoring* harness +
  effort row, but the proxy forwards default-effort requests. Documented;
  effort-forwarding is deferred.
