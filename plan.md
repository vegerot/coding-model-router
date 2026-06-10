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

Implement **M0–M2.2**: repo scaffold + data layer + `router snapshot` CLI,
pure routing engine, CLI selection, and pre-normalization eligibility filtering.
**Done.** Dynamic OpenRouter mapping (M3) comes next; the OpenAI-compatible
proxy moves to M4 and later.

## ✅ Progress (M0–M2.2 complete)

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
- [x] **M2.2 — pre-normalization eligibility filter.** `refresh.Build` now drops
  models with `artificial_analysis_coding_index < 20.0` before they enter
  `Snapshot.Candidates`, so `NormalizedQuality` and `engine.Select` operate only
  over coding-eligible models. Models exactly at `20.0` remain eligible; dropped
  rows record `coding index below minimum: X < 20`. `snapshot.SchemaVersion` is
  bumped to `2` so stale caches with too-small candidates are rejected.

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

## Future milestones

- **M3 — dynamic OpenRouter mapping.** Make AA slug → OpenRouter model ID
  resolution its own milestone before proxying. The resolver should use the
  current AA snapshot plus the live OpenRouter catalog from
  `GET https://openrouter.ai/api/v1/models`, then cache the catalog locally so
  diagnostics and mapped-only selection do not require a network call every run.
  Update `DESIGN.md` and `README.md` when this is implemented.

  Experiment justification:
  - `go run ./... snapshot --json` produced **189** coding-eligible AA
    candidates after the M2.2 filter.
  - The AA free endpoint exposed **0** `openRouterId` / `openrouter_api_id`
    values for those candidates.
  - The repo had **0** curated aliases checked in.
  - Strict deterministic matching against the OpenRouter model catalog resolved
    **141 / 189 candidates (74.6%)**.
  - The **top 20** candidates by AA coding quality were resolvable; the **top
    25** were not all resolvable.
  - Several AA reasoning-effort variants naturally collapse to one OpenRouter
    model ID, e.g. `gpt-5-5-high`, `gpt-5-5-medium`, `gpt-5-5-low`, and
    `gpt-5-5-non-reasoning` all resolve to `openai/gpt-5.5`.

  Mapping policy:
  - Do **not** check in an alias table for M3; new model releases should not
    require code changes just to become eligible for resolution.
  - Do **not** add a local override file in M3.
  - Use deterministic runtime rules only: provider/creator must match where
    known; normalized AA slug/name must exactly match normalized OpenRouter
    ID/name; effort labels such as `xhigh`, `high`, `medium`, `low`, `minimal`,
    `adaptive`, `reasoning`, and `non-reasoning` may be ignored for model-ID
    matching.
  - Do **not** use fuzzy runtime matching. Ambiguous matches are unresolved and
    excluded from mapped-only selection.
  - If multiple AA variants deterministically map to the same OpenRouter ID,
    keep them as distinct AA candidates while routing them to that same
    OpenRouter ID.

  CLI/API surface:
  - Add `router mappings` diagnostics with mapped/unmapped/ambiguous counts and
    percentages, top unmapped candidates sorted by AA coding quality, and
    `--json` output.
  - Add `router select --mapped-only` so selection can ignore unresolved or
    ambiguous candidates before calling the routing engine.
  - Keep proxy serving, request rewriting, streaming, auth forwarding, retry
    behavior, and OpenRouter request execution out of M3.

  Test plan:
  - Resolver tests for deterministic pairs, ambiguity, provider mismatch, and
    effort-label collapse.
  - CLI tests for `router mappings` counts, sorted unmapped output, JSON output,
    and `router select --mapped-only`.
  - Fixture tests with checked-in AA snapshot and OpenRouter model-list fixtures
    so resolver behavior is deterministic offline.
  - Verify with `go test ./...`, `go build ./...`, and `go vet ./...`.

- **M4 — proxy.** `serve` subcommand; OpenAI-compatible request/response shape;
  SSE passthrough; knob parsing; use M3 mappings to rewrite selected AA
  candidates to OpenRouter model IDs.
- **M5 — resilience and observability.** Stickiness, OpenRouter `models[]`
  fallback, structured logs, selection trace output, and operator-friendly
  diagnostics.
- **M6 — polish.** Installation, docs, release packaging, and UX refinements.

## Deferred

Token-weighted cost (the big one) · Aider / SWE-bench providers · adaptive
`p=auto` · budget governor · calibration · shadow/A-B · speed axis · capability
filters (including future context-window minimums from AA Pro or models.dev) ·
tunable cost weighting · user-local OpenRouter alias overrides such as
`~/.config/coding-model-router/openrouter-aliases.json`.
