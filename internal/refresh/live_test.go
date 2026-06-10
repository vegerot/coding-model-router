//go:build live

// Live end-to-end test of the refresh pipeline against the real Artificial
// Analysis API. Skipped by default `go test`; run with `make live-test`. A
// failure means the live data no longer satisfies our build/validate
// assumptions. Requires AA_API_KEY in the environment.
package refresh_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/provider"
	"github.com/vegerot/coding-model-router/internal/refresh"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func TestLiveRefreshEndToEnd(t *testing.T) {
	key := os.Getenv("AA_API_KEY")
	if key == "" {
		t.Skip("AA_API_KEY not set; skipping live refresh test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, stale, err := refresh.Refresh(ctx, refresh.Options{
		Provider:  provider.NewAA(key),
		CachePath: path,
		Stderr:    io.Discard,
	})
	if err != nil {
		t.Fatalf("live Refresh failed: %v", err)
	}
	if stale {
		t.Error("fresh live refresh should not be stale")
	}

	// Validate already enforced the floors; re-assert the headline expectations
	// and that the cache round-trips.
	if len(s.Candidates) < 30 {
		t.Errorf("expected >=30 live candidates, got %d", len(s.Candidates))
	}
	if s.Sources.Provider != provider.AAName {
		t.Errorf("provider = %q, want %q", s.Sources.Provider, provider.AAName)
	}
	// Candidates must be sorted ascending by the cost axis.
	for i := 1; i < len(s.Candidates); i++ {
		if s.Candidates[i].BlendedPricePer1M < s.Candidates[i-1].BlendedPricePer1M {
			t.Errorf("candidates not sorted by blended price at index %d", i)
			break
		}
	}
	if _, err := snapshot.Load(path); err != nil {
		t.Errorf("cache did not round-trip: %v", err)
	}

	t.Logf("live refresh: %d candidates from %d raw models; cheapest=%s ($%.2f/1M, q=%.1f), priciest=%s ($%.2f/1M, q=%.1f)",
		len(s.Candidates), s.Sources.ModelCount,
		s.Candidates[0].Slug, s.Candidates[0].BlendedPricePer1M, s.Candidates[0].Quality,
		s.Candidates[len(s.Candidates)-1].Slug, s.Candidates[len(s.Candidates)-1].BlendedPricePer1M, s.Candidates[len(s.Candidates)-1].Quality)
}
