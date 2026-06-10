# 🛣️ Pareto-Style Coding Model Router — Plan & Progress

> The durable design of record is [`DESIGN.md`](./DESIGN.md). This file tracks the
> plan and what's built. It reflects the **pivot** away from scraping (see history).

## Goal

A local OpenAI-compatible Go proxy with a single continuous knob `p ∈ [0, 1]`:
`p` is a quality floor; the router picks the **cheapest model at or above the
floor**. `p=0` → cheapest overall, `p=1` → top-ranked. Better than OpenRouter's
`pareto-code` in three ways: a continuous knob (not 3 tiers), the full benchmark
leaderboard (not a curated subset), and honest cost.

## 🔀 Pivot (2026-06-09): scrape → Artificial Analysis Data API

The original plan scraped AA's coding-**agents** leaderboard (a Next.js RSC
payload) and joined models.dev pricing, with a hand-curated AA→OpenRouter alias
table and token-usage-weighted cost. That was fragile and complex. We replaced
it with the **AA Data API free tier**, which returns — in clean paginated JSON,
one source — a model-level coding index, the agentic/intelligence indices, full
per-token pricing (incl. cache), and a measured eval cost.

Decisions made with the user during the pivot:
- **Quality** = `artificial_analysis_coding_index` (model-level; right signal for
  routing a raw model). Agentic + intelligence indices stored for later.
- **Cost (V1)** = a single **blended 3:1 per-token price** `(3·input + output)/4`
  from AA's in-band pricing. **No token-weighting in V1** — deferred (the inputs,
  AA's measured `total_cost`, are captured as `EvalTotalCostUSD`). Eventually
  weight cost by the tokens/dollars a model burns to run the benchmark.
- **One source**: AA only. No models.dev, no scrape. Pricing is in-band.
- **Pluggable**: data sourcing is a `provider.Provider` interface; AA is the
  first implementation. Aider polyglot / SWE-bench can be added later.
- **OpenRouter ID mapping** (`openrouter_api_id` is Pro-tier only; absent on free)
  is needed only for *routing* — deferred to M3. The snapshot is keyed by AA slug.
- **AA attribution required** across all tiers.

## Scope

Implement **M0–M2**: repo scaffold + data layer + `router snapshot` CLI +
pure routing engine. **Done.** The OpenAI-compatible proxy (M3–M5) is designed
in `DESIGN.md` and built later.

## ✅ Progress (M0–M2 complete)

- [x] **M0 — scaffold.** `go mod`, `cmd/router` subcommand dispatch, Makefile,
  README, DESIGN.md, `.gitignore` (ignores the API key). `go build`/`vet` clean.
- [x] **M1.1 — `internal/snapshot`.** `Snapshot`/`Candidate` (keyed by slug:
  coding-index `Quality`, agentic/intelligence indices, per-token prices,
  `BlendedPricePer1M` cost axis, informational `EvalTotalCostUSD`).
  `NormalizedQuality` (set-dependent min-max; single→1.0). Atomic `Save`,
  schema-checked `Load`. Tests in `snapshot_test`.
- [x] **M1.2 — `internal/provider`.** `Provider` interface (`Name`, `Fetch`) +
  provider-agnostic `Model`. `AA` implements it against the AA Data API free tier
  (paginated, `x-api-key`, maps coding/agentic/intelligence + pricing +
  total_cost). Black-box tests + `//go:build live` shape-contract test.
- [x] **M1.3 — `internal/refresh`.** Pure `Build` (drop models missing coding
  index or pricing with a reason; compute blended price; sort by cost) and
  `Validate` (raw-count ≥50, candidates ≥30, max-coding-index ≥20 tripwires).
  `Refresh` orchestrator: fetch → build → validate → atomic save, with last-good
  fallback (`stale=true`) on failure. Tests-first in `*_test`; live end-to-end
  test passes (**331 candidates from 507 raw models**).
- [x] **M1.5 — `internal/cli` + `cmd/router`.** `cli.Snapshot` implements
  `router snapshot [--refresh] [--json] [--cache PATH] [--api-key KEY]`: API key
  from `--api-key` else `$AA_API_KEY`; cache + no `--refresh` → print from cache
  (no network), else fetch via `provider.NewAA` + `refresh.Refresh` and cache;
  `--json` → raw JSON; `text/tabwriter` table sorted cheapest-first
  (`MODEL·QUALITY·NORM·$/1M·IN$·OUT$·PROVIDER`) + summary + **always-printed AA
  attribution**; exit codes `0`/`1`/`2` (ok / no-data / stale-fallback). Logic
  lives in `internal/cli` so it is black-box testable (`cli_test`); `cmd/router`
  is a thin dispatcher. Live smoke: 331 candidates, cached re-run does no network.
- [x] **M2 — `internal/engine`.** Pure `Select(snapshot, p, opts) → Plan`
  importing only `internal/snapshot`. `p` is validated in `[0,1]`; selection
  uses set-dependent normalized quality and picks the cheapest candidate at or
  above the floor. `Plan` returns the primary plus ordered qualifying fallbacks
  (cost, then higher quality, then slug). Tests cover p=0 cheapest, p=1 best,
  monotonic non-decreasing cost as p rises, dominated models never chosen,
  single-candidate behavior, fallback ordering, and invalid input errors.
- [x] **M2.1 — CLI selection.** `router select [--p P] [--refresh] [--json]
  [--cache PATH] [--api-key KEY]` loads the cached/refreshed snapshot, calls
  `engine.Select`, and prints the selected primary plus ordered fallbacks.
  JSON output includes the plan, snapshot source metadata, fetch time, and AA
  attribution.

## 🧪 Conventions & verification

- **Tests first**, in external `_test` packages (only exported API).
- Commit after each step; messages end with a
  `Co-Authored-By: <agent name> <agent email>` trailer, using the name and email
  of the agent that wrote the code.
- Offline: `go build ./... && go vet ./... && go test ./...`.
- Live: `make live-test` (`go test -tags live ./...`) with `AA_API_KEY` set —
  shape-contract tests against the real API (skip when the key is unset).
- Manual: `AA_API_KEY=… go run ./cmd/router snapshot --refresh` → ~300 candidates,
  cheapest first; re-run without `--refresh` → served from cache, no network;
  `… snapshot --json | python3 -m json.tool` → valid; `… select --p 0.7`
  prints the selected primary and fallbacks from the cached/refreshed snapshot.

## Future milestones (designed in DESIGN.md)

- **M2.2 pre-normalization eligibility filter** — fix the current candidate set
  including models that are too small/weak for coding harnesses. Build-time
  filtering should use only AA free-tier data for now: drop models with
  `artificial_analysis_coding_index < 20.0` before they enter
  `Snapshot.Candidates`, so normalization and `engine.Select` operate only over
  eligible models. Models exactly at `20.0` remain eligible. Dropped rows should
  record a clear reason such as `coding index below minimum: 18.5 < 20`.
  Implementation should bump `snapshot.SchemaVersion` from `1` to `2` so stale
  caches that include too-small models are rejected and refreshed. Tests should
  cover below-threshold drop, threshold-inclusive keep, schema mismatch, and the
  normal offline suite (`go test ./...`, `go build ./...`, `go vet ./...`).
  When implemented, update `DESIGN.md` and `README.md` to document the behavior.
  Future improvements: replace or supplement the score gate with a context-size
  minimum from AA Pro `context_window_tokens`, or from models.dev context
  metadata.
- **M3 proxy** — `serve` subcommand; OpenAI-compatible SSE passthrough; knob
  parsing; **AA slug → OpenRouter ID mapping** lives here.
- **M4** — stickiness, OpenRouter `models[]` fallback, observability.
- **M5** — polish.

## Deferred

Token-weighted cost (the big one) · Aider / SWE-bench providers · adaptive
`p=auto` · budget governor · calibration · shadow/A-B · speed axis · capability
filters · tunable cost weighting.
