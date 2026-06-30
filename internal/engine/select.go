// Package engine implements the pure routing decision: given a validated
// snapshot and a quality knob p, choose the cheapest model that clears the
// normalized quality floor.
package engine

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/vegerot/coding-model-router/internal/snapshot"
)

var (
	ErrNilSnapshot            = errors.New("engine: nil snapshot")
	ErrNoCandidates           = errors.New("engine: no candidates")
	ErrNoQualifyingCandidates = errors.New("engine: no candidates meet quality floor")
)

// Options is reserved for future engine-only selection controls. Keep it in the
// API now so M3 can call the stable M2 shape from DESIGN.md.
type Options struct{}

// Plan is the model routing decision. Primary is the chosen model. Fallbacks are
// the remaining candidates in routing order: first candidates that also clear p
// by the same cost-first policy, then lower-quality rescue fallbacks ordered by
// best quality. This is the complete chain a proxy layer can expose as provider
// fallbacks.
type Plan struct {
	P         float64              `json:"p"`
	Primary   snapshot.Candidate   `json:"primary"`
	Fallbacks []snapshot.Candidate `json:"fallbacks,omitempty"`
}

// Select chooses the cheapest candidate whose min-max-normalized coding quality
// is at or above p. Ties on cost prefer higher raw quality, then slug. The
// fallback chain includes every remaining candidate so CLI selection output and
// proxy failover behavior share the same routing plan.
func Select(s *snapshot.Snapshot, p float64, _ Options) (Plan, error) {
	if s == nil {
		return Plan{}, ErrNilSnapshot
	}
	if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
		return Plan{}, fmt.Errorf("engine: p must be in [0,1], got %v", p)
	}
	if len(s.Candidates) == 0 {
		return Plan{}, ErrNoCandidates
	}

	norm := snapshot.NormalizedQuality(s.Candidates)
	qualifiers := make([]snapshot.Candidate, 0, len(s.Candidates))
	rescueFallbacks := make([]snapshot.Candidate, 0, len(s.Candidates))
	for _, c := range s.Candidates {
		if norm[c.Slug] >= p {
			qualifiers = append(qualifiers, c)
		} else {
			rescueFallbacks = append(rescueFallbacks, c)
		}
	}
	if len(qualifiers) == 0 {
		return Plan{}, ErrNoQualifyingCandidates
	}

	sort.SliceStable(qualifiers, func(i, j int) bool {
		return cheaperForRouting(qualifiers[i], qualifiers[j])
	})
	sort.SliceStable(rescueFallbacks, func(i, j int) bool {
		return betterRescueFallback(rescueFallbacks[i], rescueFallbacks[j])
	})

	fallbacks := make([]snapshot.Candidate, 0, len(s.Candidates)-1)
	fallbacks = append(fallbacks, qualifiers[1:]...)
	fallbacks = append(fallbacks, rescueFallbacks...)

	return Plan{
		P:         p,
		Primary:   qualifiers[0],
		Fallbacks: fallbacks,
	}, nil
}

func cheaperForRouting(a, b snapshot.Candidate) bool {
	if a.BlendedPricePer1M != b.BlendedPricePer1M {
		return a.BlendedPricePer1M < b.BlendedPricePer1M
	}
	if a.Quality != b.Quality {
		return a.Quality > b.Quality
	}
	return a.Slug < b.Slug
}

func betterRescueFallback(a, b snapshot.Candidate) bool {
	if a.Quality != b.Quality {
		return a.Quality > b.Quality
	}
	if a.BlendedPricePer1M != b.BlendedPricePer1M {
		return a.BlendedPricePer1M < b.BlendedPricePer1M
	}
	return a.Slug < b.Slug
}
