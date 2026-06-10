// Package provider defines the pluggable benchmark-data interface and the
// provider-agnostic Model record that every provider returns. The data layer
// (internal/refresh) consumes Providers without knowing which benchmark or API
// backs them; today the only implementation is internal/provider/aa
// (Artificial Analysis Data API), but Aider, SWE-bench, etc. can be added by
// implementing this interface.
package provider

import (
	"context"
	"net/http"
)

// Provider fetches benchmark + pricing data for a set of models.
type Provider interface {
	// Name identifies the provider in snapshots and logs, e.g. "artificial-analysis".
	Name() string
	// Fetch returns one Model per model the provider knows about. The client may
	// be nil, in which case the implementation uses a sensible default.
	Fetch(ctx context.Context, client *http.Client) ([]Model, error)
}

// Model is a provider-agnostic benchmark record. Pointer fields are nil when the
// provider does not report that field for a given model (distinct from zero).
type Model struct {
	Slug        string // provider-native stable identifier
	Name        string // human-readable display name
	Creator     string // model creator / vendor, e.g. "OpenAI"
	ReleaseDate string // ISO date, may be empty

	// Quality signals (0–100 on the Artificial Analysis scale). CodingIndex is
	// the headline signal; a model without it cannot become a routing candidate.
	CodingIndex       *float64
	AgenticIndex      *float64
	IntelligenceIndex *float64

	// Pricing, USD per 1M tokens. Nil when the provider/source omits the field.
	InputPricePer1M      *float64
	OutputPricePer1M     *float64
	CacheHitPricePer1M   *float64
	CacheWritePricePer1M *float64

	// EvalTotalCostUSD is a measured dollar cost to run the provider's benchmark
	// (informational in V1; the seed for a future token-weighted cost axis).
	EvalTotalCostUSD *float64

	// OpenRouterID is the OpenRouter model id when the provider supplies one
	// (e.g. AA's Pro-tier openrouter_api_id); "" otherwise. Needed only for
	// routing (M3), not for the snapshot.
	OpenRouterID string
}
