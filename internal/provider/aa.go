package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// AAName is the provider identifier recorded in snapshots.
const AAName = "artificial-analysis"

// AADefaultBaseURL is the Artificial Analysis Data API free-tier
// language-models endpoint (https://artificialanalysis.ai/data-api).
const AADefaultBaseURL = "https://artificialanalysis.ai/api/v2/language/models/free"

const (
	aaUserAgent = "coding-model-router/0.1 (+github.com/vegerot/coding-model-router)"
	aaMaxBody   = 16 << 20 // generous cap; a page is ~150KB
	aaMaxPages  = 25       // safety bound (free tier is ~3 pages) and a daily-quota cap
	aaAPIKeyHdr = "x-api-key"
)

// AA implements Provider against the Artificial Analysis Data API free tier. The
// free endpoint returns model identity, the coding/agentic/intelligence indices,
// full per-token pricing, and a measured eval cost — everything the data layer
// needs, in clean paginated JSON. Construct with NewAA; the zero value is unusable.
type AA struct {
	apiKey  string
	baseURL string
}

// NewAA returns an AA provider using the given API key and the default endpoint.
func NewAA(apiKey string) *AA {
	return &AA{apiKey: apiKey, baseURL: AADefaultBaseURL}
}

// NewAAWithBaseURL is like NewAA but overrides the endpoint (used by tests).
func NewAAWithBaseURL(apiKey, baseURL string) *AA {
	return &AA{apiKey: apiKey, baseURL: baseURL}
}

// Name implements Provider.
func (p *AA) Name() string { return AAName }

// --- wire types: only the free-tier fields we consume ---

type aaResponse struct {
	Tier       string        `json:"tier"`
	Pagination aaPagination  `json:"pagination"`
	Data       []aaModelJSON `json:"data"`
}

type aaPagination struct {
	Page       int  `json:"page"`
	TotalPages int  `json:"total_pages"`
	HasMore    bool `json:"has_more"`
}

type aaModelJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	ReleaseDate string `json:"release_date"`
	Creator     struct {
		Name string `json:"name"`
	} `json:"model_creator"`
	Evaluations struct {
		Intelligence *float64 `json:"artificial_analysis_intelligence_index"`
		Coding       *float64 `json:"artificial_analysis_coding_index"`
		Agentic      *float64 `json:"artificial_analysis_agentic_index"`
	} `json:"evaluations"`
	Cost struct {
		TotalCost *float64 `json:"total_cost"`
	} `json:"artificial_analysis_intelligence_index_cost"`
}

// Fetch implements Provider, paging through the free endpoint.
func (p *AA) Fetch(ctx context.Context, client *http.Client) ([]Model, error) {
	if p.apiKey == "" {
		return nil, errors.New("aa: API key is required (set AA_API_KEY or pass --aa-api-key)")
	}
	if client == nil {
		client = http.DefaultClient
	}

	var models []Model
	for page := 1; page <= aaMaxPages; page++ {
		resp, err := p.fetchPage(ctx, client, page)
		if err != nil {
			return nil, err
		}
		for _, m := range resp.Data {
			models = append(models, m.toModel())
		}
		if !resp.Pagination.HasMore || page >= resp.Pagination.TotalPages {
			break
		}
	}
	if len(models) == 0 {
		return nil, errors.New("aa: API returned no models")
	}
	return models, nil
}

func (p *AA) fetchPage(ctx context.Context, client *http.Client, page int) (*aaResponse, error) {
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("aa: parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("aa: build request: %w", err)
	}
	req.Header.Set(aaAPIKeyHdr, p.apiKey)
	req.Header.Set("User-Agent", aaUserAgent)

	httpResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aa: fetch page %d: %w", page, err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, aaMaxBody))
	if err != nil {
		return nil, fmt.Errorf("aa: read page %d: %w", page, err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aa: page %d: unexpected status %s: %s", page, httpResp.Status, aaSnippet(body))
	}

	var resp aaResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aa: decode page %d: %w", page, err)
	}
	return &resp, nil
}

func (m aaModelJSON) toModel() Model {
	return Model{
		Slug:              m.Slug,
		Name:              m.Name,
		Creator:           m.Creator.Name,
		ReleaseDate:       m.ReleaseDate,
		CodingIndex:       m.Evaluations.Coding,
		AgenticIndex:      m.Evaluations.Agentic,
		IntelligenceIndex: m.Evaluations.Intelligence,
		EvalTotalCostUSD:  m.Cost.TotalCost,
		// openrouter_api_id is Pro-tier only; left empty on the free tier.
	}
}

func aaSnippet(b []byte) string {
	const n = 200
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
