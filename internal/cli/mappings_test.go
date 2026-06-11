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

func TestSelectMappedOnly(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.json")
	catalogPath := filepath.Join(t.TempDir(), "openrouter-models.json")
	seedSnapshot(t, snapshotPath)
	seedCatalog(t, catalogPath)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{
		"--cache", snapshotPath,
		"--openrouter-cache", catalogPath,
		"--mapped-only",
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
				OpenRouterID string `json:"openrouterId"`
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

func seedCatalog(t *testing.T, path string) {
	t.Helper()
	catalog := &mapping.Catalog{
		SchemaVersion: mapping.CatalogSchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
		Source:        mapping.CatalogSource,
		Models: []mapping.OpenRouterModel{
			{ID: "test/cheap-low", CanonicalSlug: "test/cheap-low", Name: "Cheap Low"},
			{ID: "test/mid", CanonicalSlug: "test/mid", Name: "Mid"},
		},
	}
	if err := mapping.SaveCatalog(path, catalog); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
}
