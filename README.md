# coding-model-router 🛣️

A Pareto-style coding-model router with a **single continuous quality knob** `p ∈ [0, 1]`.

Instead of OpenRouter's `pareto-code` (a curated shortlist + three coarse quality tiers), this router:

1. **Continuous knob, not tiers.** `p` is a quality floor; the router picks the **cheapest model at or above the floor**. `p=0` → cheapest model overall; `p=1` → the top-ranked model regardless of cost. Every choice is automatically Pareto-optimal.
2. **Full leaderboard.** Candidates come from Artificial Analysis's model-level coding index, not a hand-picked subset.
3. **Honest V1 cost.** Each model's cost axis is a single Artificial Analysis in-band blended price: `(3*input + output)/4` per 1M tokens.

## Status

Under construction. **M0–M3 are complete**: the data layer builds a validated cached snapshot, filters out models below coding index `20.0` before normalization, the pure routing engine selects the cheapest model at or above a continuous quality floor, and M3 dynamically resolves AA candidates to OpenRouter model IDs.

```sh
make build
./router snapshot --refresh    # fetch live data, build + cache the snapshot, print the table
./router snapshot              # print from cache (no network)
./router snapshot --json       # machine-readable snapshot
./router select --p 0.7        # choose the cheapest model at or above p=0.7
./router select --p 0.7 --json # machine-readable selection plan
./router mappings              # resolve cached candidates to OpenRouter model IDs
./router mappings --json       # machine-readable mapping diagnostics
./router select --p 0.7 --mapped-only
```

`router mappings` uses the cached AA snapshot plus a cached OpenRouter model catalog from `GET https://openrouter.ai/api/v1/models`. It does not rely on a checked-in alias table: deterministic matches are derived at runtime, ambiguous matches stay unresolved, and `select --mapped-only` excludes unresolved/ambiguous candidates before routing selection.

The OpenAI-compatible proxy (`router serve`) is designed in [`DESIGN.md`](./DESIGN.md) and built in M4.

## Development

```sh
make build      # build ./router
make test       # offline unit + golden tests
make vet        # go vet
make live-test  # shape-contract tests against the real endpoints (network; not run by default)
```

Requires Go 1.26+. No third-party dependencies.

## Attribution

- **Quality and pricing data:** [Artificial Analysis](https://artificialanalysis.ai). Used under their terms; attribution required wherever this data is displayed.
- **Model catalog data:** OpenRouter `/api/v1/models`.
