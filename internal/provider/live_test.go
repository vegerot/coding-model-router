//go:build live

// Live shape-contract test for the Artificial Analysis provider. Skipped by the
// default `go test`; run with `make live-test` (= `go test -tags live ./...`).
// It hits the real AA Data API and asserts the free-tier response still has the
// shape the data layer assumes. A failure means AA changed its API — update the
// wire types / assumptions. Requires AA_API_KEY in the environment.
package provider_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/provider"
)

func TestLiveAAShape(t *testing.T) {
	key := os.Getenv("AA_API_KEY")
	if key == "" {
		t.Skip("AA_API_KEY not set; skipping live shape-contract test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	models, err := provider.NewAA(key).Fetch(ctx, nil)
	if err != nil {
		t.Fatalf("live AA fetch failed: %v", err)
	}

	// Headroom below today's ~500 so routine leaderboard churn doesn't flake.
	if len(models) < 100 {
		t.Fatalf("expected >=100 models from live AA, got %d", len(models))
	}

	var withCoding, withPricing, withCost int
	var maxCoding float64
	var sawOpenAI, sawAnthropic bool
	for _, m := range models {
		if m.Slug == "" {
			t.Error("live model has empty slug")
		}
		if m.CodingIndex != nil {
			withCoding++
			if *m.CodingIndex > maxCoding {
				maxCoding = *m.CodingIndex
			}
			// Coding index is on a 0–100 scale; <=1 would signal a units change.
			if *m.CodingIndex < 0 || *m.CodingIndex > 100 {
				t.Errorf("coding index for %s out of 0–100 band: %v", m.Slug, *m.CodingIndex)
			}
		}
		if m.InputPricePer1M != nil && m.OutputPricePer1M != nil {
			withPricing++
		}
		if m.EvalTotalCostUSD != nil {
			withCost++
		}
		switch m.Creator {
		case "OpenAI":
			sawOpenAI = true
		case "Anthropic":
			sawAnthropic = true
		}
	}

	if withCoding < 50 {
		t.Errorf("only %d/%d models had a coding index (expected >=50)", withCoding, len(models))
	}
	if withPricing < 50 {
		t.Errorf("only %d/%d models had input+output pricing (expected >=50)", withPricing, len(models))
	}
	if maxCoding < 20 {
		t.Errorf("max coding index %.1f < 20 — possible scale/units change", maxCoding)
	}
	if !sawOpenAI || !sawAnthropic {
		t.Errorf("expected OpenAI and Anthropic anchors; openai=%v anthropic=%v", sawOpenAI, sawAnthropic)
	}
	t.Logf("live AA: %d models, %d coding, %d pricing, %d total_cost, max coding=%.1f",
		len(models), withCoding, withPricing, withCost, maxCoding)
}
