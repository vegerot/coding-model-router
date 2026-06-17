package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/vegerot/coding-model-router/internal/provider"
)

const apiKeyHeader = "x-api-key"

// fixtureServer serves testdata/aa_page{N}.json keyed by the ?page= param and
// records the API key header it saw.
func fixtureServer(t *testing.T, gotKey *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotKey != nil {
			*gotKey = r.Header.Get(apiKeyHeader)
		}
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		data, err := os.ReadFile("testdata/aa_page" + page + ".json")
		if err != nil {
			http.Error(w, "no such page", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func TestAAFetchPaginatesAndMaps(t *testing.T) {
	var gotKey string
	srv := fixtureServer(t, &gotKey)
	defer srv.Close()

	p := provider.NewAAWithBaseURL("test-key", srv.URL)
	models, err := p.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// 3 on page 1 + 2 on page 2 = 5 total (pagination followed).
	if len(models) != 5 {
		t.Fatalf("expected 5 models across 2 pages, got %d", len(models))
	}
	if gotKey != "test-key" {
		t.Errorf("x-api-key header = %q, want test-key", gotKey)
	}

	// Spot-check field mapping on the first model (gpt-5-5).
	m := models[0]
	if m.Slug != "gpt-5-5" || m.Creator != "OpenAI" {
		t.Errorf("model[0] identity wrong: %+v", m)
	}
	if m.CodingIndex == nil || *m.CodingIndex != 59.1 {
		t.Errorf("coding index = %v, want 59.1", m.CodingIndex)
	}
	if m.InputPricePer1M != nil || m.OutputPricePer1M != nil || m.CacheWritePricePer1M != nil {
		t.Errorf("AA pricing should be ignored, got in=%v out=%v cache_write=%v", m.InputPricePer1M, m.OutputPricePer1M, m.CacheWritePricePer1M)
	}
	if m.EvalTotalCostUSD == nil || *m.EvalTotalCostUSD != 3357 {
		t.Errorf("total_cost = %v, want 3357", m.EvalTotalCostUSD)
	}
	if m.OpenRouterID != "" {
		t.Errorf("free tier should omit openrouter id, got %q", m.OpenRouterID)
	}

	// The edge model carries null coding index; Fetch passes it through as nil.
	var edge *provider.Model
	for i := range models {
		if models[i].Slug == "edge-no-coding" {
			edge = &models[i]
		}
	}
	if edge == nil {
		t.Fatal("edge-no-coding model missing")
	}
	if edge.CodingIndex != nil || edge.InputPricePer1M != nil {
		t.Errorf("edge model should have nil coding/price, got %+v", edge)
	}
}

func TestAAFetchRequiresKey(t *testing.T) {
	p := provider.NewAAWithBaseURL("", "http://example.invalid")
	if _, err := p.Fetch(context.Background(), http.DefaultClient); err == nil {
		t.Error("expected error when API key is empty, got nil")
	}
}

func TestAAFetchNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := provider.NewAAWithBaseURL("k", srv.URL)
	if _, err := p.Fetch(context.Background(), srv.Client()); err == nil {
		t.Error("expected error on 429, got nil")
	}
}

func TestAAName(t *testing.T) {
	if provider.NewAA("k").Name() != "artificial-analysis" {
		t.Errorf("Name() = %q", provider.NewAA("k").Name())
	}
	// Compile-time check that *AA satisfies Provider.
	var _ provider.Provider = (*provider.AA)(nil)
}
