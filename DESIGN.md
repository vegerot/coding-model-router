# Design — Pareto-Style Coding Model Router

A local, OpenAI-compatible HTTP proxy that routes `/v1/chat/completions` to the
cheapest model whose coding quality clears a continuous knob `p ∈ [0, 1]`, where
quality and cost both come from a pluggable benchmark provider. This document is
the durable design of record.

## Goal & differences from OpenRouter's `pareto-code`

OpenRouter's `pareto-code` router picks among ~13 curated coding models using
three coarse quality tiers. This project improves on it three ways:

1. **Continuous knob, not tiers.** A single parameter `p ∈ [0, 1]`: `p=0` always
   selects the cheapest model, `p=1` the best.
2. **Full candidate set** from a benchmark leaderboard, not a curated subset.
3. **Honest cost.** Cost combines a real measured eval cost (which reflects how
   many tokens a model actually burns) with the model's per-token list price.

## Settled design decisions

- **`p` semantics — quality threshold.** `p` is a floor on the min–max-normalized
  coding quality score over the current candidate set. The router picks the
  **cheapest candidate at or above the floor** by composite cost score. This is
  automatically Pareto-optimal. `p=0` → cheapest overall; `p=1` → top-ranked
  regardless of cost. Tie-break: higher quality, then stable by slug.
- **Shape — local OpenAI-compatible proxy.** Serves `/v1/chat/completions`,
  accepts the knob via model name (`pareto@0.7`) and/or header, rewrites the
  model field, forwards to OpenRouter with SSE streaming passthrough.
- **Stack — Go.** Single static binary, long-running local daemon, stdlib only.
- **Data source — Artificial Analysis Data API (free tier), via a pluggable
  provider interface.** See below. The earlier plan to scrape AA's coding-agents
  leaderboard page (a Next.js RSC payload) and join models.dev pricing was
  dropped: it was fragile and unnecessary. The AA Data API returns coding
  quality, full pricing, and a measured eval cost in clean paginated JSON.
- **Quality metric — `artificial_analysis_coding_index`** (model-level coding
  score, 0–100). We route to a *raw* model, so the model-level coding index is
  the right signal (the harness-aware "Coding Agent Index" is not exposed by the
  API and conflates harness with model). The agentic and intelligence indices are
  stored alongside for future use.
- **Cost axis — a single blended per-token price (V1).** Cost is
  `BlendedPricePer1M = (3·price_1m_input + 1·price_1m_output) / 4` (AA's standard
  3:1 blend), taken straight from AA's in-band pricing. V1 deliberately does
  **not** weight cost by tokens produced — that kept the design to one data
  source and one number. Because it is a single measure, the engine compares it
  directly ("cheapest above the quality floor"); no cost normalization is needed.
  AA's measured eval cost (`artificial_analysis_intelligence_index_cost.total_cost`)
  is stored as informational only and may drive a token-weighted cost axis later.
- **AA attribution is required** across all tiers, wherever data is displayed.

## Architecture

```
client (aider / Claude Code / curl)
   │  POST /v1/chat/completions  model="pareto@0.7"
   ▼
┌─ proxy (Go) ─────────────────────────────────┐
│ router engine: candidates → composite cost   │
│   → quality floor(p) → cheapest above floor  │
│ stickiness: session key → pinned             │
│   model+provider, TTL ~5 min                 │
│ fallback: models[] passed to OpenRouter +    │
│   transport-level retry; Retry-After honored │
│ data refresher (background, daily):          │
│   • BenchmarkProvider.Fetch() → []Model      │
│     (default: Artificial Analysis Data API)  │
│   • disk-cached snapshot; stale data is OK   │
└──────────────┬───────────────────────────────┘
               ▼ forwards with rewritten model
          OpenRouter API
```

### Package layout

```
cmd/router/            CLI: subcommand dispatch + `snapshot` (M1) + `serve` (M3)
internal/provider/     BenchmarkProvider interface + provider-agnostic Model record
internal/provider/aa/  Artificial Analysis Data API provider (default)
internal/snapshot/     Snapshot/Candidate types, NormalizedQuality + CostScores, store
internal/refresh/      pure Build (Model→Snapshot), validation, Refresh orchestrator
internal/engine/       (M2) pure Select(snapshot, p, opts) → routing plan
internal/proxy/        (M3) OpenAI-compatible server, SSE passthrough, stickiness
```

Dependency direction: `refresh → {provider, snapshot}`; `provider/aa` →
`provider`; `snapshot` imports nothing internal (the engine seam). M2's `engine`
imports only `snapshot`. M3's `proxy` imports `engine` + `refresh`.

## Pluggable provider interface

```go
// internal/provider
type Provider interface {
    Name() string                                            // e.g. "artificial-analysis"
    Fetch(ctx context.Context, c *http.Client) ([]Model, error)
}

// Model is a provider-agnostic benchmark record. Pointers are nil when the
// provider does not report that field.
type Model struct {
    Slug, Name, Creator string
    ReleaseDate         string
    CodingIndex         *float64 // quality signal (required to become a candidate)
    AgenticIndex        *float64
    IntelligenceIndex   *float64
    InputPricePer1M     *float64
    OutputPricePer1M    *float64
    CacheHitPricePer1M  *float64
    CacheWritePricePer1M *float64
    EvalTotalCostUSD    *float64 // measured eval cost
    OpenRouterID        string   // "" when unknown (AA free tier omits it)
}
```

Future providers (Aider polyglot YAML, SWE-bench experiments) implement the same
interface. Aider is the most likely second provider: its
`polyglot_leaderboard.yml` carries `pass_rate_2` (quality), `total_cost` (USD),
and `prompt_tokens`/`completion_tokens`, under Apache-2.0.

### Artificial Analysis provider (`internal/provider/aa`)

- Endpoint: `GET https://artificialanalysis.ai/api/v2/language/models/free`,
  header `x-api-key: $AA_API_KEY`. Paginated (`page` query param, 200/page,
  ~3 pages ≈ 500 models). Free tier: **25 requests/day** — a refresh costs ~3.
- Per-model free-tier fields we consume: `slug`, `name`, `model_creator.name`,
  `release_date`, `evaluations.{artificial_analysis_coding_index,
  artificial_analysis_agentic_index, artificial_analysis_intelligence_index}`,
  `pricing.{price_1m_input_tokens, price_1m_output_tokens,
  price_1m_cache_hit_tokens, price_1m_cache_write_tokens}`,
  `artificial_analysis_intelligence_index_cost.total_cost`.
- `openrouter_api_id` is **Pro-tier only** (absent on free). The OpenRouter ID is
  needed only for *routing* (M3), not for the snapshot; with a free key we map
  AA slug → OpenRouter ID later (small alias table / fuzzy match), with a Pro key
  the field is in-band. The snapshot is keyed by AA `slug`.
- The API key is read from `$AA_API_KEY` (or `--api-key`); it is never logged or
  committed (`.gitignore`d).

## Data layer (M1)

### Build pipeline (pure)

`Build(models []provider.Model, fetchedAt) → (Snapshot, warnings, error)`:
keep models that have a coding index **and** input price **and** output price
(others → `Dropped` with a reason) → compute
`BlendedPricePer1M = (3·input + output)/4` → fill `Candidate` → sort by
`BlendedPricePer1M`. Normalized quality is computed by the `snapshot` helper at
use time, not stored.

`Candidate` (stored): `Slug, OpenRouterID, Name, Creator, ReleaseDate`,
`Quality` (coding index), `AgenticIndex`, `IntelligenceIndex`,
`InputPricePer1M`, `OutputPricePer1M`, `CacheHitPricePer1M`,
`BlendedPricePer1M` (the cost axis), `EvalTotalCostUSD` (informational),
`Provider`.

`snapshot` helper (pure, set-dependent, keyed by `Slug`):
- `NormalizedQuality(cands) map[string]float64` — min–max of `Quality`. Cost
  needs no helper: the engine sorts candidates by `BlendedPricePer1M` directly.

### Validation rules

- ≥ 50 raw provider models (sanity; ~500 today) else fail.
- Per candidate: non-empty slug; `Quality` finite and in a plausible band
  (0–100); `InputPricePer1M`/`OutputPricePer1M` present and ≥ 0.
- ≥ 30 surviving candidates (133+ today) else fail.
- Max coding index ≥ 20 (guards a silent scale/units change) else fail.

### Snapshot store

Atomic write (`CreateTemp` → write → `Sync` → `Rename`) at
`os.UserCacheDir()/coding-model-router/snapshot.json`. `Load` errors on a
`SchemaVersion` mismatch. `Refresh` never deletes or overwrites the cache on
failure — it returns the last-good snapshot flagged `stale=true` plus the causal
error.

## Future milestones (designed; not yet built)

- **M2 — engine.** Pure function importing only `internal/snapshot`:
  `Select(s *snapshot.Snapshot, p float64, opts Options) (Plan, error)`; `Plan`
  = primary + ordered fallbacks (remaining qualifiers by ascending cost score).
  Unit-tested: cheapest at `p=0`, best at `p=1`, monotonic non-decreasing cost in
  `p`, dominated models never chosen.
- **M3 — proxy + OpenRouter mapping.** `serve` subcommand. Knob parsing:
  `pareto@0.7` model name; bare `pareto` → default `p`; `X-Pareto-P` header
  overrides; malformed → 400; other model names → transparent passthrough. SSE
  passthrough with mid-stream-error chunk inspection; usage from the final chunk.
  This is where AA slug → OpenRouter ID mapping lives (alias table / fuzzy match
  against OpenRouter `/api/v1/models`, or a Pro key's `openrouter_api_id`).
- **M4 — resilience + observability.** Stickiness keyed by `X-Session-Id` (else a
  hash of system prompt + first user message); in-memory, sliding ~5 min TTL;
  pins model+provider; refreshes apply to new sessions only. Fallback via
  OpenRouter `models[]` + transport retry; honor `Retry-After`. Per-request log:
  chosen model, `p`, cost score, actual `usage.cost`, fallback hops.
- **M5 — polish.** README usage examples, license.

## OpenRouter mechanics (verified from docs, for M3/M4)

- **Native fallback** — request param `models: []` (priority order); OpenRouter
  retries on any error and reports/bills the model that served via response
  `model`. Honor `Retry-After` on 429/503.
- **Provider pinning** — `provider: {order: ["slug"], allow_fallbacks: false}`;
  503 if the pinned provider is down → re-route fresh and re-pin.
- **Usage** — automatic in every response; final SSE chunk before `[DONE]`;
  cached tokens in `usage.prompt_tokens_details.cached_tokens`; `usage.cost` in
  credits. Serving provider via `X-OpenRouter-Metadata: enabled`.
- **Mid-stream errors** — after 200 + tokens, an error arrives as an SSE chunk
  with top-level `error` and `finish_reason: "error"`. Inspect every chunk.
- **Auth** — forward client `Authorization: Bearer sk-or-…` if present, else
  inject `OPENROUTER_API_KEY`. Send `HTTP-Referer` / `X-OpenRouter-Title`.

## Deferred features

- **Token-weighted cost (the big one).** V1 ranks cost by a flat blended
  per-token *price*. Eventually weight cost by how many tokens (or what measured
  dollar cost) a model actually consumes to run the benchmark — so a model that
  burns lots of reasoning/output tokens per task is costed honestly, not just by
  its sticker price. AA already exposes the inputs for this:
  `artificial_analysis_intelligence_index_cost.total_cost` (a measured USD cost,
  stored now as `EvalTotalCostUSD`) on the free tier, and
  `artificial_analysis_intelligence_index_token_counts` (input / output /
  reasoning token counts) on the Pro tier. The eventual cost axis becomes some
  blend of price and that measured token burn (caveat: AA's figure is the
  Intelligence Index workload, not a coding-specific run — a richer provider or
  the Coding Agent Index would be more faithful).
- Aider / SWE-bench providers · adaptive `p=auto` via difficulty classifier ·
  budget governor + cost ledger · personal calibration · shadow / A-B mode ·
  speed axis (`:nitro`) · capability filters (context window, tool-calling,
  open-weights, data policy).

## Risks

- **AA API key dependency** — refresh needs a valid `$AA_API_KEY`; free tier is
  25 req/day. Mitigated by the cached last-good snapshot and stale-OK refresh.
- **Free-tier field gaps** — some models lack a coding index, pricing, or
  total_cost; they are dropped (with a reason) and the ≥30-candidate floor is the
  backstop.
- **`total_cost` is the Intelligence Index workload**, not coding-specific — the
  best free proxy for token burn, but not a true coding-task cost.
- **AA slug → OpenRouter ID mapping** (deferred to M3) is the new ongoing
  maintenance cost in place of the old scrape's alias table.
