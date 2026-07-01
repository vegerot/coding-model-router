package benchmark_provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
)

func TestOpenRouterBenchmarksFetchMapsRoutableCodingModels(t *testing.T) {
	var gotAuth, gotSource, gotTaskType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSource = r.URL.Query().Get("source")
		gotTaskType = r.URL.Query().Get("task_type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [
				{
					"source": "artificial-analysis",
					"model_permaslug": "openai/gpt-5.5-20260423",
					"display_name": "GPT-5.5 (xhigh)",
					"intelligence_index": 54.8,
					"coding_index": 74.9,
					"agentic_index": 44.9,
					"pricing": {
						"prompt": "0.000005",
						"completion": "0.00003"
					}
				}
			],
			"meta": {
				"as_of": "2026-07-01T00:00:51.889Z",
				"citation": "Source: Artificial Analysis (artificialanalysis.ai) via OpenRouter (openrouter.ai/rankings)."
			}
		}`))
	}))
	defer srv.Close()

	provider := benchmark_provider.NewOpenRouterBenchmarksWithBaseURL("or-key", srv.URL)
	models, err := provider.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotAuth != "Bearer or-key" {
		t.Fatalf("Authorization = %q, want Bearer or-key", gotAuth)
	}
	if gotSource != "artificial-analysis" || gotTaskType != "coding" {
		t.Fatalf("query source=%q task_type=%q, want artificial-analysis/coding", gotSource, gotTaskType)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	m := models[0]
	if m.Slug != "openai/gpt-5.5-20260423" || m.OpenRouterID != "openai/gpt-5.5-20260423" {
		t.Fatalf("model identity = slug %q openrouter %q", m.Slug, m.OpenRouterID)
	}
	if m.Name != "GPT-5.5 (xhigh)" {
		t.Fatalf("Name = %q", m.Name)
	}
	if m.CodingIndex == nil || *m.CodingIndex != 74.9 {
		t.Fatalf("CodingIndex = %v, want 74.9", m.CodingIndex)
	}
	if m.AgenticIndex == nil || *m.AgenticIndex != 44.9 {
		t.Fatalf("AgenticIndex = %v, want 44.9", m.AgenticIndex)
	}
	if m.IntelligenceIndex == nil || *m.IntelligenceIndex != 54.8 {
		t.Fatalf("IntelligenceIndex = %v, want 54.8", m.IntelligenceIndex)
	}
	if m.InputPricePer1M == nil || *m.InputPricePer1M != 5 {
		t.Fatalf("InputPricePer1M = %v, want 5", m.InputPricePer1M)
	}
	if m.OutputPricePer1M == nil || *m.OutputPricePer1M != 30 {
		t.Fatalf("OutputPricePer1M = %v, want 30", m.OutputPricePer1M)
	}

	var _ benchmark_provider.BenchmarkProvider = (*benchmark_provider.OpenRouterBenchmarks)(nil)
}
