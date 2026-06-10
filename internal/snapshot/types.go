// Package snapshot defines the persisted candidate-model snapshot and the pure
// helpers that operate on it. It imports nothing else in this module: it is the
// stable contract that the data layer writes and the routing engine (M2) reads.
package snapshot

import "time"

// SchemaVersion is bumped whenever the on-disk Snapshot shape changes
// incompatibly. Load refuses to read a snapshot with a different version.
const SchemaVersion = 1

// Attribution is displayed wherever snapshot data is shown. Artificial Analysis
// requires attribution for use of their leaderboard data.
const Attribution = "Quality data: Artificial Analysis Coding Agent Index " +
	"(https://artificialanalysis.ai/agents/coding-agents). " +
	"Pricing: models.dev / OpenRouter."

// Snapshot is the full, validated set of routing candidates plus provenance.
// Candidates are sorted by CostPerTaskUSD ascending.
type Snapshot struct {
	SchemaVersion int          `json:"schemaVersion"`
	FetchedAt     time.Time    `json:"fetchedAt"`
	Attribution   string       `json:"attribution"`
	Sources       SourceMeta   `json:"sources"`
	Candidates    []Candidate  `json:"candidates"`
	Dropped       []DroppedRow `json:"dropped,omitempty"`
}

// SourceMeta records where a snapshot's data came from, for transparency and
// staleness reasoning.
type SourceMeta struct {
	AAURL         string `json:"aaUrl"`
	AARowCount    int    `json:"aaRowCount"`    // raw rows before collapse
	PricingSource string `json:"pricingSource"` // "models.dev" | "openrouter" | "mixed"
}

// Candidate is one routable model: an AA leaderboard row collapsed to its best
// harness/effort, mapped to an OpenRouter ID, with token-weighted pricing.
type Candidate struct {
	AASlug       string `json:"aaSlug"`       // hostModelSlug of the kept row
	OpenRouterID string `json:"openrouterId"` // e.g. "anthropic/claude-opus-4.8"
	DisplayLabel string `json:"displayLabel"` // kept row's label (harness + effort)
	Agent        string `json:"agent"`        // harness name of the kept row
	Effort       string `json:"effort,omitempty"`

	Quality        float64  `json:"quality"` // raw AA indexScore, 0–1
	TokenMix       TokenMix `json:"tokenMix"`
	Prices         Prices   `json:"prices"` // USD per 1M tokens
	CostPerTaskUSD float64  `json:"costPerTaskUsd"`
	AAMeanCostUSD  float64  `json:"aaMeanCostUsd"` // AA's observed mean cost, for sanity/display

	CacheReadEstimated bool `json:"cacheReadEstimated,omitempty"` // 0.1× heuristic used
	ContextWindow      int  `json:"contextWindow,omitempty"`      // seam for M2 constraints
}

// TokenMix is AA's observed per-task mean token usage. MeanInputTokens includes
// the cached portion (MeanCacheTokens).
type TokenMix struct {
	MeanInputTokens  float64 `json:"meanInputTokens"`
	MeanOutputTokens float64 `json:"meanOutputTokens"`
	MeanCacheTokens  float64 `json:"meanCacheTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
}

// Prices are per-1M-token prices for a model on OpenRouter.
type Prices struct {
	InputPer1M     float64 `json:"inputPer1m"`
	OutputPer1M    float64 `json:"outputPer1m"`
	CacheReadPer1M float64 `json:"cacheReadPer1m"`
	Source         string  `json:"source"` // "models.dev" | "openrouter"
}

// DroppedRow records an AA model that did not become a candidate, with the
// reason — for transparency and to surface alias-table gaps.
type DroppedRow struct {
	AASlug string `json:"aaSlug"`
	Reason string `json:"reason"` // "not-on-openrouter" | "duplicate-of:<slug>" | "unmapped" | "no-price"
}
