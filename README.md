# coding-model-router 🛣️

A Pareto-style coding-model router with a **single continuous quality knob** `p ∈ [0, 1]`.

Instead of OpenRouter's `pareto-code` (a curated shortlist + three coarse quality tiers), this router:

1. **Continuous knob, not tiers.** `p` is a quality floor; the router picks the **cheapest model at or above the floor**. `p=0` → cheapest model overall; `p=1` → the top-ranked model regardless of cost. Every choice is automatically Pareto-optimal.
2. **Full leaderboard.** Candidates come from the entire [Artificial Analysis coding-agents leaderboard](https://artificialanalysis.ai/agents/coding-agents), not a hand-picked subset.
3. **Token-usage-weighted pricing.** Each model's effective cost-per-task weights OpenRouter per-token prices (via [models.dev](https://models.dev)) by Artificial Analysis's observed per-task token mix (input / cached / output), so verbose-but-cheap models are costed honestly.

## Status

Under construction. The **data layer** (M0–M1) is the current milestone: it builds a *snapshot* of candidate models (quality + token-weighted effective cost) from live data, with a disk cache and validation.

```sh
make build
./router snapshot --refresh    # fetch live data, build + cache the snapshot, print the table
./router snapshot              # print from cache (no network)
./router snapshot --json       # machine-readable snapshot
```

The routing engine and OpenAI-compatible proxy (`router serve`) are designed in [`DESIGN.md`](./DESIGN.md) and built in later milestones.

## Development

```sh
make build      # build ./router
make test       # offline unit + golden tests
make vet        # go vet
make live-test  # shape-contract tests against the real endpoints (network; not run by default)
```

Requires Go 1.26+. No third-party dependencies.

## Attribution

- **Quality data:** [Artificial Analysis](https://artificialanalysis.ai/agents/coding-agents) — AA Coding Agent Index (composite of SWE-Bench-Pro-Hard-AA, Terminal-Bench v2, SWE-Atlas-QnA). Used under their terms; attribution required wherever this data is displayed.
- **Pricing:** [models.dev](https://models.dev) (primary) and [OpenRouter](https://openrouter.ai) (fallback).
