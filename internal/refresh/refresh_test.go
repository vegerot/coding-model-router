package refresh_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
	"github.com/vegerot/coding-model-router/internal/refresh"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func ptr(f float64) *float64 { return &f }

// validModel is a fully-populated benchmark_provider.Model that survives Build's filter.
func validModel(slug string, coding, in, out float64) benchmark_provider.Model {
	return benchmark_provider.Model{
		Slug: slug, Name: slug, Creator: "Test",
		CodingIndex: ptr(coding), AgenticIndex: ptr(coding + 1), IntelligenceIndex: ptr(coding + 2),
		InputPricePer1M: ptr(in), OutputPricePer1M: ptr(out),
		CacheHitPricePer1M: ptr(in / 10), EvalTotalCostUSD: ptr(in * 100),
	}
}

// manyValid returns n valid models with spread-out quality and price.
func manyValid(n int) []benchmark_provider.Model {
	out := make([]benchmark_provider.Model, n)
	for i := range out {
		out[i] = validModel(fmt.Sprintf("m-%02d", i), 20+float64(i), 1+float64(i), 4+float64(i))
	}
	return out
}

const fixedTime = "2026-06-09T12:00:00Z"

func at() time.Time {
	t, err := time.Parse(time.RFC3339, fixedTime)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildFiltersAndComputes(t *testing.T) {
	models := []benchmark_provider.Model{
		{
			Slug: "good", Name: "good", Creator: "Test",
			CodingIndex: ptr(50), AgenticIndex: ptr(51), IntelligenceIndex: ptr(52), EvalTotalCostUSD: ptr(400),
		},
		{Slug: "no-coding", Name: "no-coding"},
		{Slug: "too-low", Name: "too-low", CodingIndex: ptr(19)},
	}
	s := refresh.Build(models, "artificial-analysis", at())

	if len(s.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(s.Candidates))
	}
	c := s.Candidates[0]
	if c.Slug != "good" {
		t.Errorf("kept wrong candidate: %s", c.Slug)
	}
	if c.InputPricePer1M != 0 || c.OutputPricePer1M != 0 || c.BlendedPricePer1M != 0 {
		t.Errorf("ArtificialAnalysis prices should not be carried into snapshot: %+v", c)
	}
	if c.AgenticIndex != 51 || c.IntelligenceIndex != 52 || c.EvalTotalCostUSD != 400 {
		t.Errorf("optional fields not carried: %+v", c)
	}
	if c.Provider != "artificial-analysis" {
		t.Errorf("provider = %q", c.Provider)
	}
	if s.Sources.ModelCount != 3 || s.Sources.Provider != "artificial-analysis" {
		t.Errorf("sources wrong: %+v", s.Sources)
	}
	if len(s.Dropped) != 2 {
		t.Fatalf("expected 2 dropped, got %d (%+v)", len(s.Dropped), s.Dropped)
	}
	for _, d := range s.Dropped {
		if d.Reason == "" {
			t.Errorf("dropped %s has no reason", d.Slug)
		}
	}
	if !s.FetchedAt.Equal(at()) || s.SchemaVersion != snapshot.SchemaVersion {
		t.Errorf("metadata wrong: fetchedAt=%v schema=%d", s.FetchedAt, s.SchemaVersion)
	}
}

func TestBuildSortsByQualityDescending(t *testing.T) {
	models := []benchmark_provider.Model{
		validModel("mid", 45, 4, 16),
		validModel("top", 60, 20, 40),
		validModel("cheap", 30, 0, 4),
	}
	s := refresh.Build(models, "p", at())
	got := []string{s.Candidates[0].Slug, s.Candidates[1].Slug, s.Candidates[2].Slug}
	want := []string{"top", "mid", "cheap"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order = %v, want %v", got, want)
		}
	}
}

func TestBuildDropsModelsBelowMinimumCodingIndex(t *testing.T) {
	models := []benchmark_provider.Model{
		validModel("too-small", 19.9, 1, 4),
		validModel("threshold", 20.0, 2, 8),
		validModel("above-threshold", 20.1, 3, 12),
	}
	s := refresh.Build(models, "p", at())

	if len(s.Candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2 (%+v)", len(s.Candidates), s.Candidates)
	}
	for _, c := range s.Candidates {
		if c.Slug == "too-small" {
			t.Fatalf("below-threshold model survived: %+v", c)
		}
	}
	if len(s.Dropped) != 1 {
		t.Fatalf("dropped count = %d, want 1 (%+v)", len(s.Dropped), s.Dropped)
	}
	if s.Dropped[0].Slug != "too-small" ||
		!strings.Contains(s.Dropped[0].Reason, "coding index below minimum: 19.9 < 20") {
		t.Fatalf("unexpected dropped row: %+v", s.Dropped[0])
	}
}

func TestBuildCarriesOpenRouterPricingAndDropsUnpricedRows(t *testing.T) {
	models := []benchmark_provider.Model{
		validModel("priced", 50, 5, 30),
		{Slug: "missing-input", Name: "missing-input", CodingIndex: ptr(40), OutputPricePer1M: ptr(1), OpenRouterID: "test/missing-input"},
		{Slug: "missing-output", Name: "missing-output", CodingIndex: ptr(40), InputPricePer1M: ptr(1), OpenRouterID: "test/missing-output"},
		validModel("zero-priced", 45, 0, 0),
	}
	for i := range models {
		models[i].OpenRouterID = "test/" + models[i].Slug
	}

	s := refresh.Build(models, benchmark_provider.OpenRouterBenchmarksName, at())

	if len(s.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1 (%+v)", len(s.Candidates), s.Candidates)
	}
	c := s.Candidates[0]
	if c.Slug != "priced" || c.OpenRouterID != "test/priced" {
		t.Fatalf("candidate identity = %+v", c)
	}
	if c.InputPricePer1M != 5 || c.OutputPricePer1M != 30 {
		t.Fatalf("prices = in %g out %g, want 5/30", c.InputPricePer1M, c.OutputPricePer1M)
	}
	if c.BlendedPricePer1M != 11.25 {
		t.Fatalf("blended price = %g, want 11.25", c.BlendedPricePer1M)
	}
	if len(s.Dropped) != 3 {
		t.Fatalf("dropped count = %d, want 3 (%+v)", len(s.Dropped), s.Dropped)
	}
	var missing, nonPositive int
	for _, d := range s.Dropped {
		switch {
		case strings.Contains(d.Reason, "missing OpenRouter pricing"):
			missing++
		case strings.Contains(d.Reason, "non-positive OpenRouter pricing"):
			nonPositive++
		default:
			t.Fatalf("unexpected dropped reason: %+v", d)
		}
	}
	if missing != 2 || nonPositive != 1 {
		t.Fatalf("dropped reasons: missing=%d nonPositive=%d, want 2/1 (%+v)", missing, nonPositive, s.Dropped)
	}
}

func TestValidate(t *testing.T) {
	t.Run("healthy snapshot passes", func(t *testing.T) {
		s := refresh.Build(manyValid(60), "p", at())
		if err := refresh.Validate(s); err != nil {
			t.Errorf("expected healthy snapshot to pass, got %v", err)
		}
	})
	t.Run("too few candidates fails (enough raw, most dropped)", func(t *testing.T) {
		// 60 raw models clears the raw-count floor, but only 10 are complete, so
		// the candidate floor (>=30) is what must fail.
		models := manyValid(10)
		for i := 0; i < 50; i++ {
			models = append(models, benchmark_provider.Model{Slug: fmt.Sprintf("bad-%02d", i), InputPricePer1M: ptr(1), OutputPricePer1M: ptr(2)})
		}
		s := refresh.Build(models, "p", at())
		if s.Sources.ModelCount != 60 {
			t.Fatalf("setup: ModelCount = %d, want 60", s.Sources.ModelCount)
		}
		if err := refresh.Validate(s); err == nil {
			t.Error("expected too-few-candidates failure, got nil")
		}
	})
	t.Run("quality scale guard fails when all scores tiny", func(t *testing.T) {
		models := make([]benchmark_provider.Model, 60)
		for i := range models {
			models[i] = validModel(fmt.Sprintf("t-%02d", i), 0.3, 1+float64(i), 4) // max coding 0.3
		}
		s := refresh.Build(models, "p", at())
		if err := refresh.Validate(s); err == nil {
			t.Error("expected quality-scale-guard failure (max coding < 20), got nil")
		}
	})
}

func TestRefreshHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	prov := &stubProvider{name: "artificial-analysis", models: manyValid(60)}

	s, stale, err := refresh.Refresh(context.Background(), refresh.Options{
		Provider: prov, CachePath: path, Now: at, Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if stale {
		t.Error("fresh refresh should not be stale")
	}
	if len(s.Candidates) != 60 {
		t.Errorf("expected 60 candidates, got %d", len(s.Candidates))
	}
	// Cache was written and reloads.
	if _, err := snapshot.Load(path); err != nil {
		t.Errorf("expected cache written, Load failed: %v", err)
	}
}

func TestRefreshFallsBackToLastGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	// Seed a good cache.
	ok := &stubProvider{name: "p", models: manyValid(60)}
	if _, _, err := refresh.Refresh(context.Background(), refresh.Options{
		Provider: ok, CachePath: path, Now: at, Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("seed Refresh: %v", err)
	}

	// Now the provider errors: Refresh should return the last-good snapshot, stale.
	boom := &stubProvider{name: "p", err: errors.New("network down")}
	s, stale, err := refresh.Refresh(context.Background(), refresh.Options{
		Provider: boom, CachePath: path, Now: at, Stderr: io.Discard,
	})
	if err == nil {
		t.Error("expected the causal error to be returned alongside last-good")
	}
	if !stale {
		t.Error("expected stale=true when serving last-good")
	}
	if s == nil || len(s.Candidates) != 60 {
		t.Errorf("expected last-good snapshot with 60 candidates, got %+v", s)
	}
}

func TestRefreshNoCacheNoData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")
	boom := &stubProvider{name: "p", err: errors.New("network down")}
	s, stale, err := refresh.Refresh(context.Background(), refresh.Options{
		Provider: boom, CachePath: path, Now: at, Stderr: io.Discard,
	})
	if err == nil {
		t.Error("expected error when fetch fails and no cache exists")
	}
	if stale {
		t.Error("stale should be false when there is no last-good to serve")
	}
	if s != nil {
		t.Errorf("expected nil snapshot, got %+v", s)
	}
}

// stubProvider implements benchmark_provider.BenchmarkProvider for Refresh tests.
type stubProvider struct {
	name   string
	models []benchmark_provider.Model
	err    error
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Fetch(context.Context, *http.Client) ([]benchmark_provider.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.models, nil
}
