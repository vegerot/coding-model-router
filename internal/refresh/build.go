// Package refresh turns benchmark-provider data into a validated, disk-cached
// snapshot. Build and Validate are pure (no I/O); Refresh orchestrates a fetch,
// build, validate, and atomic save, falling back to the last-good snapshot on
// failure.
package refresh

import (
	"fmt"
	"sort"
	"time"

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

const minCandidateCodingIndex = 20.0

// Build transforms provider models into a Snapshot: it drops models missing a
// coding index or models below the minimum coding-index eligibility floor
// (recording each with a reason), and sorts candidates by descending quality.
// It is pure — validation is a separate step (Validate).
func Build(models []benchmark_provider.Model, providerName string, fetchedAt time.Time) *snapshot.Snapshot {
	s := &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		FetchedAt:     fetchedAt,
		Attribution:   attributionForProvider(providerName),
		Sources:       snapshot.SourceMeta{Provider: providerName, ModelCount: len(models)},
	}

	for _, m := range models {
		if reason := dropReason(m, providerName); reason != "" {
			s.Dropped = append(s.Dropped, snapshot.DroppedRow{Slug: m.Slug, Reason: reason})
			continue
		}
		c := snapshot.Candidate{
			Slug:              m.Slug,
			OpenRouterID:      m.OpenRouterID,
			Name:              m.Name,
			Creator:           m.Creator,
			ReleaseDate:       m.ReleaseDate,
			Quality:           *m.CodingIndex,
			AgenticIndex:      deref(m.AgenticIndex),
			IntelligenceIndex: deref(m.IntelligenceIndex),
			EvalTotalCostUSD:  deref(m.EvalTotalCostUSD),
			Provider:          providerName,
		}
		if m.InputPricePer1M != nil && m.OutputPricePer1M != nil {
			c.InputPricePer1M = *m.InputPricePer1M
			c.OutputPricePer1M = *m.OutputPricePer1M
			c.BlendedPricePer1M = (3*c.InputPricePer1M + c.OutputPricePer1M) / 4
		}
		if m.CacheHitPricePer1M != nil {
			c.CacheHitPricePer1M = *m.CacheHitPricePer1M
		}
		s.Candidates = append(s.Candidates, c)
	}

	sort.SliceStable(s.Candidates, func(i, j int) bool {
		a, b := s.Candidates[i], s.Candidates[j]
		if a.Quality != b.Quality {
			return a.Quality > b.Quality
		}
		return a.Slug < b.Slug
	})
	return s
}

func attributionForProvider(providerName string) string {
	if providerName == benchmark_provider.OpenRouterBenchmarksName {
		return snapshot.OpenRouterAttribution
	}
	return snapshot.Attribution
}

// dropReason returns why a model cannot become a candidate, or "" if it can.
func dropReason(m benchmark_provider.Model, providerName string) string {
	switch {
	case m.Slug == "":
		return "empty slug"
	case m.CodingIndex == nil:
		return "missing coding index"
	case *m.CodingIndex < minCandidateCodingIndex:
		return fmt.Sprintf("coding index below minimum: %.1f < %.0f", *m.CodingIndex, minCandidateCodingIndex)
	case providerName == benchmark_provider.OpenRouterBenchmarksName && m.OpenRouterID == "":
		return "missing OpenRouter model ID"
	case providerName == benchmark_provider.OpenRouterBenchmarksName && (m.InputPricePer1M == nil || m.OutputPricePer1M == nil):
		return "missing OpenRouter pricing"
	case providerName == benchmark_provider.OpenRouterBenchmarksName && (*m.InputPricePer1M <= 0 || *m.OutputPricePer1M <= 0):
		return "non-positive OpenRouter pricing"
	}
	return ""
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
