package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/cli"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func seedSnapshot(t *testing.T, path string) *snapshot.Snapshot {
	t.Helper()
	s := &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		Attribution:   snapshot.Attribution,
		Sources:       snapshot.SourceMeta{Provider: "artificial-analysis", ModelCount: 507},
		Candidates: []snapshot.Candidate{
			{
				Slug:              "cheap-low",
				Name:              "Cheap Low",
				Quality:           30,
				InputPricePer1M:   0,
				OutputPricePer1M:  4,
				BlendedPricePer1M: 1,
				Provider:          "artificial-analysis",
			},
			{
				Slug:              "mid",
				Name:              "Mid",
				Quality:           45,
				InputPricePer1M:   4,
				OutputPricePer1M:  16,
				BlendedPricePer1M: 7,
				Provider:          "artificial-analysis",
			},
			{
				Slug:              "pricey-top",
				Name:              "Pricey Top",
				Quality:           60,
				InputPricePer1M:   20,
				OutputPricePer1M:  40,
				BlendedPricePer1M: 25,
				Provider:          "artificial-analysis",
			},
		},
		Dropped: []snapshot.DroppedRow{{Slug: "x", Reason: "missing coding index"}},
	}
	if err := snapshot.Save(path, s); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return s
}

func TestSnapshotPrintsFromCache(t *testing.T) {
	t.Setenv("AA_API_KEY", "") // ensure the cache path is taken, never the network
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Snapshot([]string{"--cache", path}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got := out.String()

	// Header + all candidate slugs present.
	for _, want := range []string{"MODEL", "QUALITY", "NORM", "cheap-low", "mid", "pricey-top"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q\n---\n%s", want, got)
		}
	}
	// Attribution line is always printed.
	if !strings.Contains(got, "Artificial Analysis") {
		t.Errorf("missing ArtificialAnalysis attribution\n---\n%s", got)
	}
	// Summary mentions candidate and dropped counts.
	if !strings.Contains(got, "3 candidates") || !strings.Contains(got, "507") {
		t.Errorf("summary line wrong\n---\n%s", got)
	}
	// Highest normalized quality must be listed first.
	if strings.Index(got, "pricey-top") > strings.Index(got, "mid") ||
		strings.Index(got, "mid") > strings.Index(got, "cheap-low") {
		t.Errorf("expected pricey-top, then mid, then cheap-low\n---\n%s", got)
	}
}

func TestSnapshotJSON(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Snapshot([]string{"--cache", path, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var s snapshot.Snapshot
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if len(s.Candidates) != 3 || s.Sources.Provider != "artificial-analysis" {
		t.Errorf("decoded snapshot wrong: %+v", s.Sources)
	}
}

func TestSnapshotNoCacheNoKeyExits1(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "absent.json")

	var out, errOut bytes.Buffer
	code := cli.Snapshot([]string{"--cache", path}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (no cache + no key)", code)
	}
	if errOut.Len() == 0 {
		t.Error("expected an error message on stderr")
	}
}

func TestSnapshotAcceptsAAAPIKeyFlag(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Snapshot([]string{"--aa-api-key", "test-key", "--cache", path}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if out.Len() == 0 {
		t.Fatal("expected snapshot output")
	}
}

func TestSnapshotNormColumn(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	if code := cli.Snapshot([]string{"--cache", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	// Top quality normalizes to 1.00, bottom to 0.00.
	if !strings.Contains(got, "1.00") || !strings.Contains(got, "0.00") {
		t.Errorf("expected normalized 1.00 and 0.00 in table\n---\n%s", got)
	}
}
