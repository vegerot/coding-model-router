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

Implement **M0–M1**: repo scaffold + data layer + `router snapshot` CLI. The
routing engine and proxy (M2–M5) are designed in `DESIGN.md` and built later.

## ✅ Progress

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
- [ ] **M1.5 — `cmd/router/snapshot.go`.** The remaining piece — see below.

## ▶️ Remaining: `router snapshot` CLI (M1.5)

`router snapshot [--refresh] [--json] [--cache PATH] [--api-key KEY]`:
- API key from `--api-key` else `$AA_API_KEY`.
- Cache exists & no `--refresh` → print from cache (no network); no cache →
  auto-fetch; `--refresh` → force fetch. `--json` → emit the raw snapshot JSON.
- `text/tabwriter` table sorted by blended price ascending:
  `MODEL · QUALITY · NORM · $/1M(blended) · IN$ · OUT$ · PROVIDER`, a summary line
  (`N candidates from M models · fetched … · D dropped`), and an
  **always-printed AA attribution line**.
- Exit codes: `0` ok, `1` no data (fetch failed, no cache), `2` stale-fallback
  (served cached snapshot; warnings on stderr).
- Wire `provider.NewAA(key)` + `refresh.Refresh`. Tests in `main_test` (golden
  table + exit codes, using `--cache` into a temp dir; no network).

## 🧪 Conventions & verification

- **Tests first**, in external `_test` packages (only exported API).
- Commit after each step; messages end with the `Co-Authored-By: Claude` trailer.
- Offline: `go build ./... && go vet ./... && go test ./...`.
- Live: `make live-test` (`go test -tags live ./...`) with `AA_API_KEY` set —
  shape-contract tests against the real API (skip when the key is unset).
- Manual: `AA_API_KEY=… go run ./cmd/router snapshot --refresh` → ~300 candidates,
  cheapest first; re-run without `--refresh` → served from cache, no network;
  `… snapshot --json | python3 -m json.tool` → valid.

## Future milestones (designed in DESIGN.md)

- **M2 engine** — pure `Select(snapshot, p, opts) → Plan`; imports only
  `internal/snapshot`. Cheapest at p=0, best at p=1, monotonic, Pareto-optimal.
- **M3 proxy** — `serve` subcommand; OpenAI-compatible SSE passthrough; knob
  parsing; **AA slug → OpenRouter ID mapping** lives here.
- **M4** — stickiness, OpenRouter `models[]` fallback, observability.
- **M5** — polish.

## Deferred

Token-weighted cost (the big one) · Aider / SWE-bench providers · adaptive
`p=auto` · budget governor · calibration · shadow/A-B · speed axis · capability
filters · tunable cost weighting.
