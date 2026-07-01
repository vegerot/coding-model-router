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

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
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
		Provider:  benchmark_provider.NewAA(key),
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
	if s.Sources.Provider != benchmark_provider.AAName {
		t.Errorf("provider = %q, want %q", s.Sources.Provider, benchmark_provider.AAName)
	}
	// Candidates must be sorted descending by quality.
	for i := 1; i < len(s.Candidates); i++ {
		if s.Candidates[i].Quality > s.Candidates[i-1].Quality {
			t.Errorf("candidates not sorted by quality at index %d", i)
			break
		}
	}
	if _, err := snapshot.Load(path); err != nil {
		t.Errorf("cache did not round-trip: %v", err)
	}

	t.Logf("live refresh: %d candidates from %d raw models; top=%s (q=%.1f), bottom=%s (q=%.1f)",
		len(s.Candidates), s.Sources.ModelCount,
		s.Candidates[0].Slug, s.Candidates[0].Quality,
		s.Candidates[len(s.Candidates)-1].Slug, s.Candidates[len(s.Candidates)-1].Quality)
}

func TestLiveOpenRouterRefreshEndToEnd(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set; skipping live OpenRouter refresh test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, stale, err := refresh.Refresh(ctx, refresh.Options{
		Provider:  benchmark_provider.NewOpenRouterBenchmarks(key),
		CachePath: path,
		Stderr:    io.Discard,
	})
	if err != nil {
		t.Fatalf("live OpenRouter Refresh failed: %v", err)
	}
	if stale {
		t.Error("fresh live OpenRouter refresh should not be stale")
	}
	if len(s.Candidates) < 30 {
		t.Errorf("expected >=30 live OpenRouter candidates, got %d", len(s.Candidates))
	}
	if s.Sources.Provider != benchmark_provider.OpenRouterBenchmarksName {
		t.Errorf("provider = %q, want %q", s.Sources.Provider, benchmark_provider.OpenRouterBenchmarksName)
	}
	for _, c := range s.Candidates {
		if c.OpenRouterID == "" {
			t.Errorf("candidate %s missing OpenRouterID", c.Slug)
		}
		if c.BlendedPricePer1M <= 0 {
			t.Errorf("candidate %s has non-positive blended price %g", c.Slug, c.BlendedPricePer1M)
		}
	}
	if _, err := snapshot.Load(path); err != nil {
		t.Errorf("OpenRouter cache did not round-trip: %v", err)
	}
}
