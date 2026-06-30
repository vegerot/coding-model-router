package mapping_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/mapping"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func TestResolveFixture(t *testing.T) {
	s, err := snapshot.Load(filepath.Join("testdata", "aa-snapshot.json"))
	if err != nil {
		t.Fatalf("Load snapshot fixture: %v", err)
	}
	catalog, err := mapping.LoadCatalog(filepath.Join("testdata", "openrouter-models.json"))
	if err != nil {
		t.Fatalf("Load catalog fixture: %v", err)
	}

	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.Summary.Total != 4 ||
		report.Summary.Mapped != 2 ||
		report.Summary.Ambiguous != 1 ||
		report.Summary.Unmapped != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}

	bySlug := resultsBySlug(report)
	if got := bySlug["gpt-5-5-high"].OpenRouterID; got != "openai/gpt-5.5" {
		t.Errorf("gpt-5-5-high mapped to %q", got)
	}
	if got := bySlug["claude-sonnet-4-5"].OpenRouterID; got != "anthropic/claude-sonnet-4.5" {
		t.Errorf("claude mapped to %q", got)
	}
	if bySlug["ambiguous"].Status != mapping.StatusAmbiguous {
		t.Errorf("ambiguous status = %s", bySlug["ambiguous"].Status)
	}
	if bySlug["missing-model"].Status != mapping.StatusUnmapped {
		t.Errorf("missing-model status = %s", bySlug["missing-model"].Status)
	}
}

func TestMappedSnapshotKeepsOnlyMappedCandidates(t *testing.T) {
	s := snap(
		candidate("cheap", "Cheap", "", 30, 1),
		candidate("top", "Top", "", 60, 10),
	)
	catalog := catalog(
		modelWithPricing("test/top", "Top", "0.00001", "0.00001", ""),
	)
	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	mapped := mapping.MappedSnapshot(s, report)
	if len(mapped.Candidates) != 1 {
		t.Fatalf("mapped candidates = %d, want 1", len(mapped.Candidates))
	}
	if mapped.Candidates[0].Slug != "top" || mapped.Candidates[0].OpenRouterID != "test/top" {
		t.Fatalf("mapped candidate = %+v", mapped.Candidates[0])
	}
	if len(s.Candidates) != 2 || s.Candidates[1].OpenRouterID != "" {
		t.Fatalf("MappedSnapshot mutated original snapshot: %+v", s.Candidates)
	}
}

func TestMappedSnapshotUsesOpenRouterPricing(t *testing.T) {
	s := snap(
		candidate("gemma-4-31b", "Gemma 4 31B", "Google", 40, 0),
		candidate("cheap", "Cheap", "Test", 30, 1),
	)
	catalog := catalog(
		modelWithPricing("google/gemma-4-31b-it", "Gemma 4 31B", "0.00000012", "0.00000035", "0.00000009"),
		modelWithPricing("test/cheap", "Cheap", "0.00000001", "0.00000001", ""),
	)
	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	mapped := mapping.MappedSnapshot(s, report)
	bySlug := candidatesBySlug(mapped.Candidates)
	gemma := bySlug["gemma-4-31b"]
	if gemma.InputPricePer1M != 0.12 || gemma.OutputPricePer1M != 0.35 || gemma.CacheHitPricePer1M != 0.09 {
		t.Fatalf("gemma prices = %+v", gemma)
	}
	if gemma.BlendedPricePer1M != 0.1775 {
		t.Fatalf("gemma blended = %g, want 0.1775", gemma.BlendedPricePer1M)
	}
	if mapped.Candidates[0].Slug != "cheap" || mapped.Candidates[1].Slug != "gemma-4-31b" {
		t.Fatalf("mapped candidates not sorted by OpenRouter price: %+v", mapped.Candidates)
	}
}
func TestMappedSnapshotDeduplicatesOpenRouterIDs(t *testing.T) {
	s := snap(
		candidate("same-high", "Same High", "Test", 40, 1),
		candidate("same-low", "Same Low", "Test", 30, 1),
		candidate("other", "Other", "Test", 35, 2),
	)
	report := mapping.Report{Results: []mapping.Result{
		{
			Candidate:    s.Candidates[0],
			Status:       mapping.StatusMapped,
			OpenRouterID: "test/same",
			Matches:      []mapping.Match{{ID: "test/same", PromptPrice: "0.00000001", OutputPrice: "0.00000001"}},
		},
		{
			Candidate:    s.Candidates[1],
			Status:       mapping.StatusMapped,
			OpenRouterID: "test/same",
			Matches:      []mapping.Match{{ID: "test/same", PromptPrice: "0.00000001", OutputPrice: "0.00000001"}},
		},
		{
			Candidate:    s.Candidates[2],
			Status:       mapping.StatusMapped,
			OpenRouterID: "test/other",
			Matches:      []mapping.Match{{ID: "test/other", PromptPrice: "0.00000002", OutputPrice: "0.00000002"}},
		},
	}}

	mapped := mapping.MappedSnapshot(s, report)
	if len(mapped.Candidates) != 2 {
		t.Fatalf("mapped candidates = %+v, want 2 deduplicated by OpenRouterID", mapped.Candidates)
	}
	if mapped.Candidates[0].Slug != "same-high" || mapped.Candidates[0].OpenRouterID != "test/same" {
		t.Fatalf("first candidate = %+v, want higher-quality representative for duplicate OpenRouterID", mapped.Candidates[0])
	}
	if mapped.Candidates[1].Slug != "other" || mapped.Candidates[1].OpenRouterID != "test/other" {
		t.Fatalf("second candidate = %+v", mapped.Candidates[1])
	}
}

func TestMappedSnapshotDropsModelsMissingOpenRouterPricing(t *testing.T) {
	s := snap(candidate("free", "Free", "Test", 30, 0))
	catalog := catalog(model("test/free", "Free"))
	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	mapped := mapping.MappedSnapshot(s, report)
	if len(mapped.Candidates) != 0 {
		t.Fatalf("mapped candidates = %+v, want none", mapped.Candidates)
	}
}

func TestResolveRejectsProviderMismatch(t *testing.T) {
	s := snap(candidate("claude-sonnet-4-5", "Claude Sonnet 4.5", "OpenAI", 90, 1))
	catalog := catalog(model("anthropic/claude-sonnet-4.5", "Claude Sonnet 4.5"))

	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.Results[0].Status != mapping.StatusUnmapped {
		t.Fatalf("status = %s, want unmapped", report.Results[0].Status)
	}
}

func TestResolveUsesProviderSuppliedOpenRouterID(t *testing.T) {
	s := snap(candidate("aa-slug", "ArtificialAnalysis Slug", "OpenAI", 50, 1))
	s.Candidates[0].OpenRouterID = "openai/exact"
	catalog := catalog(model("openai/exact", "Exact"))

	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.Results[0].Status != mapping.StatusMapped ||
		report.Results[0].OpenRouterID != "openai/exact" {
		t.Fatalf("result = %+v", report.Results[0])
	}
}

func TestResolvePrefersNonFreeVariant(t *testing.T) {
	s := snap(candidate("nex-n2-pro", "Nex N2 Pro", "NexAGI", 59, 0))
	catalog := catalog(
		modelWithPricing("nex-agi/nex-n2-pro", "Nex N2 Pro", "0.0000005", "0.000001", ""),
		modelWithPricing("nex-agi/nex-n2-pro:free", "Nex N2 Pro (free)", "0", "0", ""),
	)

	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.Results[0].Status != mapping.StatusMapped {
		t.Fatalf("status = %s, want mapped", report.Results[0].Status)
	}
	if got := report.Results[0].OpenRouterID; got != "nex-agi/nex-n2-pro" {
		t.Errorf("OpenRouterID = %q, want nex-agi/nex-n2-pro (non-free)", got)
	}
}

func TestResolveRejectsNilInputs(t *testing.T) {
	if _, err := mapping.Resolve(nil, catalog()); !errors.Is(err, mapping.ErrNilSnapshot) {
		t.Fatalf("nil snapshot err = %v", err)
	}
	if _, err := mapping.Resolve(snap(), nil); !errors.Is(err, mapping.ErrNilCatalog) {
		t.Fatalf("nil catalog err = %v", err)
	}
}

func resultsBySlug(report mapping.Report) map[string]mapping.Result {
	out := make(map[string]mapping.Result, len(report.Results))
	for _, r := range report.Results {
		out[r.Candidate.Slug] = r
	}
	return out
}

func snap(cands ...snapshot.Candidate) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		Attribution:   snapshot.Attribution,
		Sources:       snapshot.SourceMeta{Provider: "artificial-analysis", ModelCount: len(cands)},
		Candidates:    cands,
	}
}

func candidate(slug, name, creator string, quality, cost float64) snapshot.Candidate {
	return snapshot.Candidate{
		Slug:              slug,
		Name:              name,
		Creator:           creator,
		Quality:           quality,
		InputPricePer1M:   cost,
		OutputPricePer1M:  cost,
		BlendedPricePer1M: cost,
		Provider:          "artificial-analysis",
	}
}

func catalog(models ...mapping.OpenRouterModel) *mapping.Catalog {
	return &mapping.Catalog{
		SchemaVersion: mapping.CatalogSchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
		Source:        mapping.CatalogSource,
		Models:        models,
	}
}

func model(id, name string) mapping.OpenRouterModel {
	return mapping.OpenRouterModel{ID: id, CanonicalSlug: id, Name: name}
}

func modelWithPricing(id, name, prompt, completion, cacheRead string) mapping.OpenRouterModel {
	m := model(id, name)
	m.Pricing = mapping.Pricing{
		Prompt:         prompt,
		Completion:     completion,
		InputCacheRead: cacheRead,
	}
	return m
}

func candidatesBySlug(cands []snapshot.Candidate) map[string]snapshot.Candidate {
	out := make(map[string]snapshot.Candidate, len(cands))
	for _, c := range cands {
		out[c.Slug] = c
	}
	return out
}
