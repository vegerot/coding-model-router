package snapshot

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cand(id string, q float64) Candidate {
	return Candidate{OpenRouterID: id, Quality: q}
}

func TestNormalizedQuality(t *testing.T) {
	t.Run("min-max over a set", func(t *testing.T) {
		got := NormalizedQuality([]Candidate{
			cand("low", 0.40),
			cand("mid", 0.60),
			cand("high", 0.80),
		})
		want := map[string]float64{"low": 0.0, "mid": 0.5, "high": 1.0}
		for id, w := range want {
			if math.Abs(got[id]-w) > 1e-9 {
				t.Errorf("%s: got %v, want %v", id, got[id], w)
			}
		}
	})

	t.Run("single candidate normalizes to 1.0", func(t *testing.T) {
		got := NormalizedQuality([]Candidate{cand("solo", 0.42)})
		if got["solo"] != 1.0 {
			t.Errorf("single candidate: got %v, want 1.0", got["solo"])
		}
	})

	t.Run("all-equal qualities map to 1.0", func(t *testing.T) {
		got := NormalizedQuality([]Candidate{cand("a", 0.5), cand("b", 0.5)})
		if got["a"] != 1.0 || got["b"] != 1.0 {
			t.Errorf("equal qualities: got %v, want all 1.0", got)
		}
	})

	t.Run("empty set yields empty map", func(t *testing.T) {
		if got := NormalizedQuality(nil); len(got) != 0 {
			t.Errorf("empty set: got %v, want empty", got)
		}
	})
}

func sampleSnapshot() *Snapshot {
	return &Snapshot{
		SchemaVersion: SchemaVersion,
		FetchedAt:     time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		Attribution:   Attribution,
		Sources:       SourceMeta{AAURL: "https://artificialanalysis.ai/agents/coding-agents", AARowCount: 21, PricingSource: "models.dev"},
		Candidates: []Candidate{
			{
				AASlug: "deepseek_deepseek-v4-pro-1m", OpenRouterID: "deepseek/deepseek-v4-pro",
				DisplayLabel: "Claude Code - DeepSeek V4 Pro (high)", Agent: "Claude Code", Effort: "high",
				Quality:        0.501,
				TokenMix:       TokenMix{MeanInputTokens: 3449322, MeanOutputTokens: 29998, MeanCacheTokens: 2722770, CacheHitRate: 0.798},
				Prices:         Prices{InputPer1M: 0.435, OutputPer1M: 0.87, CacheReadPer1M: 0.003625, Source: "models.dev"},
				CostPerTaskUSD: 0.352, AAMeanCostUSD: 0.352, ContextWindow: 1048576,
			},
		},
		Dropped: []DroppedRow{{AASlug: "cursor_composer-2", Reason: "not-on-openrouter"}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "snapshot.json")
	orig := sampleSnapshot()

	if err := Save(path, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != orig.SchemaVersion ||
		!got.FetchedAt.Equal(orig.FetchedAt) ||
		len(got.Candidates) != 1 ||
		got.Candidates[0].OpenRouterID != "deepseek/deepseek-v4-pro" ||
		math.Abs(got.Candidates[0].CostPerTaskUSD-0.352) > 1e-9 ||
		len(got.Dropped) != 1 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestSaveIsAtomicNoTmpResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	if err := Save(path, sampleSnapshot()); err != nil {
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
	s.SchemaVersion = SchemaVersion + 99
	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected Load to reject a schema-version mismatch, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("expected error loading a missing file, got nil")
	}
}
