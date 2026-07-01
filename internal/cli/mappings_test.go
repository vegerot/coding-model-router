package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/cli"
	"github.com/vegerot/coding-model-router/internal/mapping"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func TestMappingsPrintsDiagnosticsFromCache(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	catalogPath := filepath.Join(t.TempDir(), "openrouter-models.json")
	seedSnapshot(t, snapshotPath)
	seedCatalog(t, catalogPath)

	var out, errOut bytes.Buffer
	code := cli.Mappings([]string{"--cache", snapshotPath, "--openrouter-cache", catalogPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"MODEL",
		"STATUS",
		"OPENROUTER_ID",
		"cheap-low",
		"mapped",
		"pricey-top",
		"unmapped",
		"2/3 mapped",
		"Artificial Analysis",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mappings output missing %q\n---\n%s", want, got)
		}
	}
}

func TestMappingsJSON(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	catalogPath := filepath.Join(t.TempDir(), "openrouter-models.json")
	seedSnapshot(t, snapshotPath)
	seedCatalog(t, catalogPath)

	var out, errOut bytes.Buffer
	code := cli.Mappings([]string{"--cache", snapshotPath, "--openrouter-cache", catalogPath, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var decoded struct {
		Report struct {
			Summary struct {
				Total  int `json:"total"`
				Mapped int `json:"mapped"`
			} `json:"summary"`
			Results []struct {
				Status       string `json:"status"`
				OpenRouterID string `json:"openrouterId"`
			} `json:"results"`
		} `json:"report"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if decoded.Report.Summary.Total != 3 || decoded.Report.Summary.Mapped != 2 {
		t.Fatalf("summary = %+v", decoded.Report.Summary)
	}
}

func TestSelectDefaultsToMappedOnly(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	catalogPath := filepath.Join(t.TempDir(), "openrouter-models.json")
	seedSnapshot(t, snapshotPath)
	seedCatalog(t, catalogPath)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{
		"--cache", snapshotPath,
		"--openrouter-cache", catalogPath,
		"--p", "1",
		"--json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var decoded struct {
		Plan struct {
			Primary struct {
				Slug         string `json:"slug"`
				OpenRouterID string `json:"openRouterId"`
			} `json:"primary"`
		} `json:"plan"`
		Mappings struct {
			Mapped   int `json:"mapped"`
			Unmapped int `json:"unmapped"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if decoded.Plan.Primary.Slug != "mid" || decoded.Plan.Primary.OpenRouterID != "test/mid" {
		t.Fatalf("primary = %+v, want mid mapped to test/mid", decoded.Plan.Primary)
	}
	if decoded.Mappings.Mapped != 2 || decoded.Mappings.Unmapped != 1 {
		t.Fatalf("mappings summary = %+v", decoded.Mappings)
	}
}

func TestSelectShowsUnmappedOpenRouterModels(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	catalogPath := filepath.Join(t.TempDir(), "openrouter-models.json")
	seedSnapshot(t, snapshotPath)
	seedCatalog(t, catalogPath)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{
		"--cache", snapshotPath,
		"--openrouter-cache", catalogPath,
		"--show-unmapped-openrouter-models",
		"--p", "1",
		"--json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var decoded struct {
		Plan struct {
			Primary struct {
				Slug         string `json:"slug"`
				OpenRouterID string `json:"openRouterId"`
			} `json:"primary"`
		} `json:"plan"`
		Mappings *mapping.Summary `json:"mappings"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if decoded.Plan.Primary.Slug != "pricey-top" || decoded.Plan.Primary.OpenRouterID != "" {
		t.Fatalf("primary = %+v, want unmapped pricey-top", decoded.Plan.Primary)
	}
	if decoded.Mappings != nil {
		t.Fatalf("mappings summary = %+v, want nil when showing unmapped", decoded.Mappings)
	}
}

func TestSelectOpenRouterSnapshotDoesNotNeedCatalog(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	s := seedSnapshot(t, snapshotPath)
	s.Sources.Provider = "openrouter"
	s.Attribution = "Quality data: Artificial Analysis via OpenRouter."
	for i := range s.Candidates {
		s.Candidates[i].OpenRouterID = "test/" + s.Candidates[i].Slug
		s.Candidates[i].Provider = "openrouter"
	}
	if err := snapshot.Save(snapshotPath, s); err != nil {
		t.Fatalf("save openrouter snapshot: %v", err)
	}

	var out, errOut bytes.Buffer
	code := cli.Select([]string{
		"--cache", snapshotPath,
		"--openrouter-cache", filepath.Join(t.TempDir(), "absent-catalog.json"),
		"--p", "1",
		"--json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var decoded struct {
		Plan struct {
			Primary struct {
				Slug         string `json:"slug"`
				OpenRouterID string `json:"openRouterId"`
			} `json:"primary"`
		} `json:"plan"`
		Mappings *mapping.Summary `json:"mappings"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if decoded.Plan.Primary.Slug != "pricey-top" || decoded.Plan.Primary.OpenRouterID != "test/pricey-top" {
		t.Fatalf("primary = %+v, want openrouter pricey-top", decoded.Plan.Primary)
	}
	if decoded.Mappings != nil {
		t.Fatalf("mappings summary = %+v, want nil for OpenRouter snapshots", decoded.Mappings)
	}
}

func TestMappingsOpenRouterSnapshotReportsAlreadyMappedRows(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	s := seedSnapshot(t, snapshotPath)
	s.Sources.Provider = "openrouter"
	s.Attribution = "Quality data: Artificial Analysis via OpenRouter."
	for i := range s.Candidates {
		s.Candidates[i].OpenRouterID = "test/" + s.Candidates[i].Slug
		s.Candidates[i].Provider = "openrouter"
	}
	if err := snapshot.Save(snapshotPath, s); err != nil {
		t.Fatalf("save openrouter snapshot: %v", err)
	}

	var out, errOut bytes.Buffer
	code := cli.Mappings([]string{
		"--cache", snapshotPath,
		"--openrouter-cache", filepath.Join(t.TempDir(), "absent-catalog.json"),
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"cheap-low", "test/cheap-low", "mapped", "3/3 mapped"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mappings output missing %q\n---\n%s", want, got)
		}
	}
}

func seedCatalog(t *testing.T, path string) {
	t.Helper()
	catalog := &mapping.Catalog{
		SchemaVersion: mapping.CatalogSchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
		Source:        mapping.CatalogSource,
		Models: []mapping.OpenRouterModel{
			{ID: "test/cheap-low", CanonicalSlug: "test/cheap-low", Name: "Cheap Low", Pricing: mapping.Pricing{Prompt: "0.000001", Completion: "0.000001"}},
			{ID: "test/mid", CanonicalSlug: "test/mid", Name: "Mid", Pricing: mapping.Pricing{Prompt: "0.000002", Completion: "0.000002"}},
		},
	}
	if err := mapping.SaveCatalog(path, catalog); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
}
