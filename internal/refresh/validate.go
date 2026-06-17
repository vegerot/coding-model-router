package refresh

import (
	"fmt"

	"github.com/vegerot/coding-model-router/internal/snapshot"
)

// Validation thresholds. These are tripwires for a source going wrong (an API
// shape change, a units change, a mostly-empty response), not tight bounds —
// today's live data yields ~500 raw models and 100+ candidates.
const (
	minRawModels        = 50
	minCandidates       = 30
	minTopQuality       = 20.0  // a max coding index below this hints at a scale/units change
	maxPlausibleQuality = 100.0 // AA's coding index is on a 0–100 scale
)

// Validate checks a built snapshot against the tripwire thresholds. A non-nil
// error means the refresh should be rejected (and the last-good snapshot kept).
func Validate(s *snapshot.Snapshot) error {
	if s.Sources.ModelCount < minRawModels {
		return fmt.Errorf("refresh: too few raw models from provider: %d < %d", s.Sources.ModelCount, minRawModels)
	}
	if len(s.Candidates) < minCandidates {
		return fmt.Errorf("refresh: too few candidates survived filtering: %d < %d", len(s.Candidates), minCandidates)
	}
	var maxQuality float64
	for _, c := range s.Candidates {
		if c.Slug == "" {
			return fmt.Errorf("refresh: candidate with empty slug")
		}
		if c.Quality < 0 || c.Quality > maxPlausibleQuality {
			return fmt.Errorf("refresh: candidate %s quality %v outside [0, %v]", c.Slug, c.Quality, maxPlausibleQuality)
		}
		if c.Quality > maxQuality {
			maxQuality = c.Quality
		}
	}
	if maxQuality < minTopQuality {
		return fmt.Errorf("refresh: max coding index %.1f < %.0f — possible scale/units change", maxQuality, minTopQuality)
	}
	return nil
}
