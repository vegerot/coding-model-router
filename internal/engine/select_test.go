package engine_test

import (
	"errors"
	"math"
	"testing"

	"github.com/vegerot/coding-model-router/internal/engine"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func snap(cands ...snapshot.Candidate) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Candidates:    cands,
	}
}

func cand(slug string, quality, cost float64) snapshot.Candidate {
	return snapshot.Candidate{
		Slug:              slug,
		Name:              slug,
		Quality:           quality,
		BlendedPricePer1M: cost,
		InputPricePer1M:   cost,
		OutputPricePer1M:  cost,
		Provider:          "test",
	}
}

func selectSlug(t *testing.T, s *snapshot.Snapshot, p float64) string {
	t.Helper()
	plan, err := engine.Select(s, p, engine.Options{})
	if err != nil {
		t.Fatalf("Select(%v): %v", p, err)
	}
	return plan.Primary.Slug
}

func TestSelectPicksCheapestAtOrAboveFloor(t *testing.T) {
	s := snap(
		cand("cheap-low", 40, 1),
		cand("mid", 60, 2),
		cand("pricey-top", 80, 100),
	)

	tests := []struct {
		p    float64
		want string
	}{
		{p: 0, want: "cheap-low"},
		{p: 0.5, want: "mid"},
		{p: 1, want: "pricey-top"},
	}
	for _, tt := range tests {
		if got := selectSlug(t, s, tt.p); got != tt.want {
			t.Errorf("p=%v selected %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestSelectReturnsOrderedFallbacks(t *testing.T) {
	s := snap(
		cand("expensive", 80, 50),
		cand("cheap-low", 40, 1),
		cand("mid", 60, 2),
		cand("same-cost-better", 70, 2),
	)

	plan, err := engine.Select(s, 0.5, engine.Options{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if plan.Primary.Slug != "same-cost-better" {
		t.Fatalf("primary = %q, want same-cost-better", plan.Primary.Slug)
	}
	got := make([]string, len(plan.Fallbacks))
	for i, c := range plan.Fallbacks {
		got[i] = c.Slug
	}
	want := []string{"mid", "expensive", "cheap-low"}
	if len(got) != len(want) {
		t.Fatalf("fallback count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fallback %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectAddsLowerQualityRescueFallbacks(t *testing.T) {
	s := snap(
		cand("cheap", 30, 1),
		cand("mid", 50, 2),
		cand("top", 90, 3),
	)

	plan, err := engine.Select(s, 1, engine.Options{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if plan.Primary.Slug != "top" {
		t.Fatalf("primary = %q, want top", plan.Primary.Slug)
	}
	got := make([]string, len(plan.Fallbacks))
	for i, c := range plan.Fallbacks {
		got[i] = c.Slug
	}
	want := []string{"mid", "cheap"}
	if len(got) != len(want) {
		t.Fatalf("fallback count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fallback %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCostIsMonotonicAsPRises(t *testing.T) {
	s := snap(
		cand("cheap-low", 10, 1),
		cand("mid", 30, 3),
		cand("high", 70, 8),
		cand("top", 90, 13),
	)

	var last float64
	for i := 0; i <= 20; i++ {
		p := float64(i) / 20
		plan, err := engine.Select(s, p, engine.Options{})
		if err != nil {
			t.Fatalf("Select(%v): %v", p, err)
		}
		if plan.Primary.BlendedPricePer1M < last {
			t.Fatalf("cost decreased at p=%v: got %v after %v", p, plan.Primary.BlendedPricePer1M, last)
		}
		last = plan.Primary.BlendedPricePer1M
	}
}

func TestDominatedModelIsNeverChosen(t *testing.T) {
	s := snap(
		cand("cheap-better", 70, 1),
		cand("dominated", 60, 2),
		cand("top", 90, 10),
	)

	for i := 0; i <= 20; i++ {
		if got := selectSlug(t, s, float64(i)/20); got == "dominated" {
			t.Fatalf("dominated model selected at p=%v", float64(i)/20)
		}
	}
}

func TestSingleCandidateAlwaysQualifies(t *testing.T) {
	s := snap(cand("solo", 42, 9))

	if got := selectSlug(t, s, 1); got != "solo" {
		t.Fatalf("single candidate selected %q, want solo", got)
	}
}

func TestSelectRejectsInvalidInputs(t *testing.T) {
	if _, err := engine.Select(nil, 0, engine.Options{}); !errors.Is(err, engine.ErrNilSnapshot) {
		t.Errorf("nil snapshot err = %v, want ErrNilSnapshot", err)
	}
	if _, err := engine.Select(snap(), 0, engine.Options{}); !errors.Is(err, engine.ErrNoCandidates) {
		t.Errorf("empty snapshot err = %v, want ErrNoCandidates", err)
	}

	s := snap(cand("ok", 50, 1))
	for _, p := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1)} {
		if _, err := engine.Select(s, p, engine.Options{}); err == nil {
			t.Errorf("Select accepted invalid p=%v", p)
		}
	}
}
