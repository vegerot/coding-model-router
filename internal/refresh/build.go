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
		Attribution:   snapshot.Attribution,
		Sources:       snapshot.SourceMeta{Provider: providerName, ModelCount: len(models)},
	}

	for _, m := range models {
		if reason := dropReason(m); reason != "" {
			s.Dropped = append(s.Dropped, snapshot.DroppedRow{Slug: m.Slug, Reason: reason})
			continue
		}
		s.Candidates = append(s.Candidates, snapshot.Candidate{
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
		})
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

// dropReason returns why a model cannot become a candidate, or "" if it can.
func dropReason(m benchmark_provider.Model) string {
	switch {
	case m.Slug == "":
		return "empty slug"
	case m.CodingIndex == nil:
		return "missing coding index"
	case *m.CodingIndex < minCandidateCodingIndex:
		return fmt.Sprintf("coding index below minimum: %.1f < %.0f", *m.CodingIndex, minCandidateCodingIndex)
	}
	return ""
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
