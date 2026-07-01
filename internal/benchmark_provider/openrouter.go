package benchmark_provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// OpenRouterBenchmarksName is the provider identifier recorded in snapshots
// built from OpenRouter's benchmark endpoint.
const OpenRouterBenchmarksName = "openrouter"

// OpenRouterBenchmarksDefaultBaseURL is OpenRouter's unified benchmark endpoint.
const OpenRouterBenchmarksDefaultBaseURL = "https://openrouter.ai/api/v1/benchmarks"

const openRouterBenchmarksMaxBody = 16 << 20

// OpenRouterBenchmarks implements Provider against OpenRouter's benchmarks API.
// It uses Artificial Analysis scores as republished by OpenRouter, with routable
// OpenRouter model IDs and OpenRouter pricing in the same row.
type OpenRouterBenchmarks struct {
	apiKey  string
	baseURL string
}

func NewOpenRouterBenchmarks(apiKey string) *OpenRouterBenchmarks {
	return &OpenRouterBenchmarks{apiKey: apiKey, baseURL: OpenRouterBenchmarksDefaultBaseURL}
}

func NewOpenRouterBenchmarksWithBaseURL(apiKey, baseURL string) *OpenRouterBenchmarks {
	return &OpenRouterBenchmarks{apiKey: apiKey, baseURL: baseURL}
}

func (p *OpenRouterBenchmarks) Name() string { return OpenRouterBenchmarksName }

type openRouterBenchmarksResponse struct {
	Data []openRouterBenchmarkJSON `json:"data"`
}

type openRouterBenchmarkJSON struct {
	Source            string   `json:"source"`
	ModelPermaslug    string   `json:"model_permaslug"`
	DisplayName       string   `json:"display_name"`
	IntelligenceIndex *float64 `json:"intelligence_index"`
	CodingIndex       *float64 `json:"coding_index"`
	AgenticIndex      *float64 `json:"agentic_index"`
	Pricing           struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

func (p *OpenRouterBenchmarks) Fetch(ctx context.Context, client *http.Client) ([]Model, error) {
	if p.apiKey == "" {
		return nil, errors.New("openrouter: API key is required (set OPENROUTER_API_KEY or pass --openrouter-api-key)")
	}
	if client == nil {
		client = http.DefaultClient
	}
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("openrouter: parse benchmarks URL: %w", err)
	}
	q := u.Query()
	q.Set("source", "artificial-analysis")
	q.Set("task_type", "coding")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter: build benchmarks request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("User-Agent", artificialAnalysisUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter: fetch benchmarks: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, openRouterBenchmarksMaxBody))
	if err != nil {
		return nil, fmt.Errorf("openrouter: read benchmarks: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter: unexpected status %s: %s", resp.Status, artificialAnalysisSnippet(body))
	}

	var payload openRouterBenchmarksResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("openrouter: decode benchmarks: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil, errors.New("openrouter: benchmarks API returned no models")
	}
	models := make([]Model, 0, len(payload.Data))
	for _, row := range payload.Data {
		models = append(models, row.toModel())
	}
	return models, nil
}

func (m openRouterBenchmarkJSON) toModel() Model {
	in := openRouterBenchmarkPricePer1M(m.Pricing.Prompt)
	out := openRouterBenchmarkPricePer1M(m.Pricing.Completion)
	return Model{
		Slug:              m.ModelPermaslug,
		Name:              m.DisplayName,
		Creator:           creatorFromOpenRouterID(m.ModelPermaslug),
		CodingIndex:       m.CodingIndex,
		AgenticIndex:      m.AgenticIndex,
		IntelligenceIndex: m.IntelligenceIndex,
		InputPricePer1M:   in,
		OutputPricePer1M:  out,
		OpenRouterID:      m.ModelPermaslug,
	}
}

func openRouterBenchmarkPricePer1M(value string) *float64 {
	if value == "" {
		return nil
	}
	price, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	per1M := price * 1_000_000
	return &per1M
}

func creatorFromOpenRouterID(id string) string {
	creator, _, ok := strings.Cut(id, "/")
	if !ok {
		return ""
	}
	return creator
}
