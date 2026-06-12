package provider_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vegerot/coding-model-router/internal/provider"
)

const apiKeyHeader = "x-api-key"

// fixtureServer serves test pages keyed by the ?page= param and records the API
// key header it saw.
func fixtureServer(t *testing.T, gotKey *string) *httptest.Server {
	t.Helper()
	pages := map[string]string{
		"1": `{
			"pagination":{"page":1,"total_pages":2,"has_more":true},
			"data":[
				{"slug":"gpt-5-5","name":"GPT-5.5","model_creator":{"name":"OpenAI"},"evaluations":{"artificial_analysis_coding_index":59.1},"artificial_analysis_intelligence_index_cost":{"total_cost":3357},"pricing":{"price_1m_input_tokens":5,"price_1m_output_tokens":30,"price_1m_cache_write_tokens":null}},
				{"slug":"mid","name":"Mid","model_creator":{"name":"Test"},"evaluations":{"artificial_analysis_coding_index":45},"pricing":{"price_1m_input_tokens":1,"price_1m_output_tokens":2}},
				{"slug":"edge-no-coding","name":"Edge","model_creator":{"name":"Test"},"evaluations":{"artificial_analysis_coding_index":null},"pricing":{"price_1m_input_tokens":null,"price_1m_output_tokens":null}}
			]
		}`,
		"2": `{
			"pagination":{"page":2,"total_pages":2,"has_more":false},
			"data":[
				{"slug":"cheap","name":"Cheap","model_creator":{"name":"Test"},"evaluations":{"artificial_analysis_coding_index":30},"pricing":{"price_1m_input_tokens":0,"price_1m_output_tokens":4}},
				{"slug":"top","name":"Top","model_creator":{"name":"Test"},"evaluations":{"artificial_analysis_coding_index":60},"pricing":{"price_1m_input_tokens":20,"price_1m_output_tokens":40}}
			]
		}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotKey != nil {
			*gotKey = r.Header.Get(apiKeyHeader)
		}
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		data, ok := pages[page]
		if !ok {
			http.Error(w, "no such page", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(data))
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
	if m.InputPricePer1M == nil || *m.InputPricePer1M != 5 ||
		m.OutputPricePer1M == nil || *m.OutputPricePer1M != 30 {
		t.Errorf("pricing wrong: in=%v out=%v", m.InputPricePer1M, m.OutputPricePer1M)
	}
	if m.CacheWritePricePer1M != nil {
		t.Errorf("cache_write was null in fixture; want nil pointer, got %v", *m.CacheWritePricePer1M)
	}
	if m.EvalTotalCostUSD == nil || *m.EvalTotalCostUSD != 3357 {
		t.Errorf("total_cost = %v, want 3357", m.EvalTotalCostUSD)
	}
	if m.OpenRouterID != "" {
		t.Errorf("free tier should omit openrouter id, got %q", m.OpenRouterID)
	}

	// The edge model carries null coding index + null pricing — Fetch passes it
	// through as nils; dropping it is the Build step's job.
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

func TestAAFetchPrefersCodingAgentIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"pagination":{"page":1,"total_pages":1,"has_more":false},
			"data":[
				{"slug":"deepseek-v4-pro","name":"DeepSeek V4 Pro","model_creator":{"name":"DeepSeek"},"evaluations":{"artificial_analysis_coding_index":47.5},"pricing":{"price_1m_input_tokens":0.27,"price_1m_output_tokens":1.35}},
				{"slug":"kimi-k2-6","name":"Kimi K2.6","model_creator":{"name":"Kimi"},"evaluations":{"artificial_analysis_coding_index":47.1},"pricing":{"price_1m_input_tokens":0.6,"price_1m_output_tokens":5.05}},
				{"slug":"gpt-5","name":"GPT-5","model_creator":{"name":"OpenAI"},"evaluations":{"artificial_analysis_coding_index":40},"pricing":{"price_1m_input_tokens":1.25,"price_1m_output_tokens":10}}
			]
		}`))
	})
	mux.HandleFunc("/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`{"hostModelSlug":"deepseek_deepseek-v4-pro-1m","indexScore":0.4699593794521551}{"hostModelSlug":"moonshot_kimi-k2-6","indexScore":0.46869496288215534}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	models, err := provider.NewAAWithURLs("test-key", srv.URL+"/models", srv.URL+"/agents").Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got := map[string]float64{}
	for _, m := range models {
		if m.CodingIndex != nil {
			got[m.Slug] = *m.CodingIndex
		}
	}
	if got["deepseek-v4-pro"] <= got["kimi-k2-6"] {
		t.Fatalf("deepseek coding-agent score = %v, kimi = %v; want deepseek higher", got["deepseek-v4-pro"], got["kimi-k2-6"])
	}
	if math.Abs(got["deepseek-v4-pro"]-46.99593794521551) > 1e-12 || math.Abs(got["kimi-k2-6"]-46.86949628821553) > 1e-12 {
		t.Fatalf("unexpected coding-agent scores: %+v", got)
	}
	if got["gpt-5"] != 40 {
		t.Fatalf("gpt-5 score = %v, want Data API fallback score 40", got["gpt-5"])
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
