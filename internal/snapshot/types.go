// Package snapshot defines the persisted candidate-model snapshot and the pure
// helpers that operate on it. It imports nothing else in this module: it is the
// stable contract that the data layer writes and the routing engine (M2) reads.
package snapshot

import "time"

// SchemaVersion is bumped whenever the on-disk Snapshot shape changes
// incompatibly. Load refuses to read a snapshot with a different version.
const SchemaVersion = 2

// Attribution is displayed wherever snapshot data is shown. Artificial Analysis
// requires attribution for use of their data, across all API tiers.
const Attribution = "Quality & pricing data: Artificial Analysis " +
	"(https://artificialanalysis.ai). Used under their terms; attribution required."

// Snapshot is the full, validated set of routing candidates plus provenance.
// Candidates are sorted by BlendedPricePer1M ascending (the cost axis).
type Snapshot struct {
	SchemaVersion int          `json:"schemaVersion"`
	FetchedAt     time.Time    `json:"fetchedAt"`
	Attribution   string       `json:"attribution"`
	Sources       SourceMeta   `json:"sources"`
	Candidates    []Candidate  `json:"candidates"`
	Dropped       []DroppedRow `json:"dropped,omitempty"`
}

// SourceMeta records which benchmark provider produced this snapshot and how
// many raw models it returned before filtering to candidates.
type SourceMeta struct {
	Provider   string `json:"provider"`   // e.g. "artificial-analysis"
	ModelCount int    `json:"modelCount"` // raw models from the provider, before filtering
}

// Candidate is one routable model: a benchmark record with a coding-quality
// score and the per-token prices used to rank cost. Keyed by Slug.
type Candidate struct {
	Slug         string `json:"slug"`                   // provider-native id (the stable key)
	OpenRouterID string `json:"openrouterId,omitempty"` // "" until mapped (M3 routing concern)
	Name         string `json:"name"`
	Creator      string `json:"creator,omitempty"`
	ReleaseDate  string `json:"releaseDate,omitempty"`

	// Quality signals on the Artificial Analysis 0–100 scale. Quality (the coding
	// index) drives the knob; the others are stored for future use.
	Quality           float64 `json:"quality"` // coding index
	AgenticIndex      float64 `json:"agenticIndex,omitempty"`
	IntelligenceIndex float64 `json:"intelligenceIndex,omitempty"`

	// Pricing, USD per 1M tokens.
	InputPricePer1M    float64 `json:"inputPricePer1m"`
	OutputPricePer1M   float64 `json:"outputPricePer1m"`
	CacheHitPricePer1M float64 `json:"cacheHitPricePer1m,omitempty"`

	// BlendedPricePer1M = (3·input + output)/4 — the V1 cost axis. The engine
	// picks the candidate with the lowest BlendedPricePer1M at or above the floor.
	BlendedPricePer1M float64 `json:"blendedPricePer1m"`

	// EvalTotalCostUSD is the provider's measured benchmark cost — informational
	// in V1, the seed for a future token-weighted cost axis.
	EvalTotalCostUSD float64 `json:"evalTotalCostUsd,omitempty"`

	Provider string `json:"provider"` // benchmark provider that supplied this candidate
}

// DroppedRow records a provider model that did not become a candidate, with the
// reason — for transparency and to surface data gaps.
type DroppedRow struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}
