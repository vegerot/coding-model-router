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
		model("test/top", "Top"),
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
	s := snap(candidate("aa-slug", "AA Slug", "OpenAI", 50, 1))
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
