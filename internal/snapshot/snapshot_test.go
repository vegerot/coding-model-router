package snapshot_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func cand(slug string, quality, blended float64) snapshot.Candidate {
	return snapshot.Candidate{Slug: slug, Quality: quality, BlendedPricePer1M: blended}
}

func TestNormalizedQuality(t *testing.T) {
	t.Run("min-max over a set, keyed by slug", func(t *testing.T) {
		got := snapshot.NormalizedQuality([]snapshot.Candidate{
			cand("low", 40, 1),
			cand("mid", 60, 1),
			cand("high", 80, 1),
		})
		want := map[string]float64{"low": 0.0, "mid": 0.5, "high": 1.0}
		for slug, w := range want {
			if math.Abs(got[slug]-w) > 1e-9 {
				t.Errorf("%s: got %v, want %v", slug, got[slug], w)
			}
		}
	})

	t.Run("single candidate normalizes to 1.0", func(t *testing.T) {
		got := snapshot.NormalizedQuality([]snapshot.Candidate{cand("solo", 42, 1)})
		if got["solo"] != 1.0 {
			t.Errorf("single candidate: got %v, want 1.0", got["solo"])
		}
	})

	t.Run("all-equal qualities map to 1.0", func(t *testing.T) {
		got := snapshot.NormalizedQuality([]snapshot.Candidate{cand("a", 50, 1), cand("b", 50, 2)})
		if got["a"] != 1.0 || got["b"] != 1.0 {
			t.Errorf("equal qualities: got %v, want all 1.0", got)
		}
	})

	t.Run("empty set yields empty map", func(t *testing.T) {
		if got := snapshot.NormalizedQuality(nil); len(got) != 0 {
			t.Errorf("empty set: got %v, want empty", got)
		}
	})
}

func sampleSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		Attribution:   snapshot.Attribution,
		Sources:       snapshot.SourceMeta{Provider: "artificial-analysis", ModelCount: 500},
		Candidates: []snapshot.Candidate{
			{
				Slug: "gpt-5-5", OpenRouterID: "", Name: "GPT-5.5 (xhigh)", Creator: "OpenAI",
				ReleaseDate: "2026-04-23",
				Quality:     59.1, AgenticIndex: 74.1, IntelligenceIndex: 60.2,
				InputPricePer1M: 5, OutputPricePer1M: 30, CacheHitPricePer1M: 0.5,
				BlendedPricePer1M: 11.25, EvalTotalCostUSD: 3357, Provider: "artificial-analysis",
			},
		},
		Dropped: []snapshot.DroppedRow{{Slug: "edge-no-coding", Reason: "missing coding index"}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "snapshot.json")
	orig := sampleSnapshot()

	if err := snapshot.Save(path, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := snapshot.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != orig.SchemaVersion ||
		!got.FetchedAt.Equal(orig.FetchedAt) ||
		got.Sources.Provider != "artificial-analysis" ||
		len(got.Candidates) != 1 ||
		got.Candidates[0].Slug != "gpt-5-5" ||
		math.Abs(got.Candidates[0].BlendedPricePer1M-11.25) > 1e-9 ||
		math.Abs(got.Candidates[0].Quality-59.1) > 1e-9 ||
		len(got.Dropped) != 1 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestSaveIsAtomicNoTmpResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	if err := snapshot.Save(path, sampleSnapshot()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A successful atomic Save leaves exactly the final file — no snapshot-*.tmp residue.
	if len(entries) != 1 || entries[0].Name() != "snapshot.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only snapshot.json, got %v", names)
	}
}

func TestLoadRejectsSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	s := sampleSnapshot()
	s.SchemaVersion = snapshot.SchemaVersion + 99
	if err := snapshot.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := snapshot.Load(path); err == nil {
		t.Error("expected Load to reject a schema-version mismatch, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := snapshot.Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("expected error loading a missing file, got nil")
	}
}
