package mapping

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/vegerot/coding-model-router/internal/snapshot"
)

type Status string

const (
	StatusMapped    Status = "mapped"
	StatusUnmapped  Status = "unmapped"
	StatusAmbiguous Status = "ambiguous"
)

var (
	ErrNilSnapshot = errors.New("mapping: nil snapshot")
	ErrNilCatalog  = errors.New("mapping: nil OpenRouter catalog")
)

// Report is the deterministic resolution result for one snapshot/catalog pair.
type Report struct {
	SnapshotFetchedAt time.Time `json:"snapshotFetchedAt"`
	CatalogFetchedAt  time.Time `json:"catalogFetchedAt"`
	CatalogSource     string    `json:"catalogSource"`
	Summary           Summary   `json:"summary"`
	Results           []Result  `json:"results"`
}

type Summary struct {
	Total            int     `json:"total"`
	Mapped           int     `json:"mapped"`
	Unmapped         int     `json:"unmapped"`
	Ambiguous        int     `json:"ambiguous"`
	MappedPercent    float64 `json:"mappedPercent"`
	UnmappedPercent  float64 `json:"unmappedPercent"`
	AmbiguousPercent float64 `json:"ambiguousPercent"`
}

type Result struct {
	Candidate      snapshot.Candidate `json:"candidate"`
	Status         Status             `json:"status"`
	OpenRouterID   string             `json:"openrouterId,omitempty"`
	OpenRouterName string             `json:"openrouterName,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	Matches        []Match            `json:"matches,omitempty"`
}

type Match struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonicalSlug,omitempty"`
	Name          string `json:"name,omitempty"`
}

func Resolve(s *snapshot.Snapshot, catalog *Catalog) (Report, error) {
	if s == nil {
		return Report{}, ErrNilSnapshot
	}
	if catalog == nil {
		return Report{}, ErrNilCatalog
	}

	report := Report{
		SnapshotFetchedAt: s.FetchedAt,
		CatalogFetchedAt:  catalog.FetchedAt,
		CatalogSource:     catalog.Source,
		Results:           make([]Result, 0, len(s.Candidates)),
	}
	for _, c := range s.Candidates {
		result := resolveCandidate(c, catalog.Models)
		report.Results = append(report.Results, result)
		switch result.Status {
		case StatusMapped:
			report.Summary.Mapped++
		case StatusAmbiguous:
			report.Summary.Ambiguous++
		default:
			report.Summary.Unmapped++
		}
	}
	report.Summary.Total = len(report.Results)
	if report.Summary.Total > 0 {
		total := float64(report.Summary.Total)
		report.Summary.MappedPercent = float64(report.Summary.Mapped) * 100 / total
		report.Summary.UnmappedPercent = float64(report.Summary.Unmapped) * 100 / total
		report.Summary.AmbiguousPercent = float64(report.Summary.Ambiguous) * 100 / total
	}
	return report, nil
}

// MappedSnapshot returns a copy of s containing only mapped candidates. Mapped
// candidates include their resolved OpenRouterID.
func MappedSnapshot(s *snapshot.Snapshot, report Report) *snapshot.Snapshot {
	if s == nil {
		return nil
	}
	bySlug := make(map[string]snapshot.Candidate, len(report.Results))
	for _, r := range report.Results {
		if r.Status != StatusMapped {
			continue
		}
		c := r.Candidate
		c.OpenRouterID = r.OpenRouterID
		bySlug[c.Slug] = c
	}
	out := *s
	out.Candidates = make([]snapshot.Candidate, 0, len(bySlug))
	for _, c := range s.Candidates {
		if mapped, ok := bySlug[c.Slug]; ok {
			out.Candidates = append(out.Candidates, mapped)
		}
	}
	return &out
}

func resolveCandidate(c snapshot.Candidate, models []OpenRouterModel) Result {
	if c.OpenRouterID != "" {
		result := Result{
			Candidate:    c,
			Status:       StatusMapped,
			OpenRouterID: c.OpenRouterID,
			Reason:       "candidate supplied OpenRouter ID",
		}
		for _, m := range models {
			if m.ID == c.OpenRouterID {
				result.OpenRouterName = m.Name
				result.Matches = []Match{modelMatch(m)}
				break
			}
		}
		return result
	}

	candidateKeys := candidateLookupKeys(c)
	matchesByID := make(map[string]OpenRouterModel)
	for _, m := range models {
		if m.ID == "" || !creatorMatchesProvider(c.Creator, providerFromID(m.ID)) {
			continue
		}
		if keysetsIntersect(candidateKeys, modelLookupKeys(m)) {
			matchesByID[m.ID] = m
		}
	}

	matches := make([]OpenRouterModel, 0, len(matchesByID))
	for _, m := range matchesByID {
		matches = append(matches, m)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })

	result := Result{Candidate: c}
	switch len(matches) {
	case 0:
		result.Status = StatusUnmapped
		result.Reason = "no deterministic OpenRouter catalog match"
	case 1:
		result.Status = StatusMapped
		result.OpenRouterID = matches[0].ID
		result.OpenRouterName = matches[0].Name
		result.Matches = []Match{modelMatch(matches[0])}
	default:
		result.Status = StatusAmbiguous
		result.Reason = "multiple deterministic OpenRouter catalog matches"
		result.Matches = make([]Match, 0, len(matches))
		for _, m := range matches {
			result.Matches = append(result.Matches, modelMatch(m))
		}
	}
	return result
}

func candidateLookupKeys(c snapshot.Candidate) map[string]struct{} {
	keys := make(map[string]struct{})
	addLookupKey(keys, c.Slug)
	addLookupKey(keys, c.Name)
	return keys
}

func modelLookupKeys(m OpenRouterModel) map[string]struct{} {
	keys := make(map[string]struct{})
	addLookupKey(keys, m.ID)
	addLookupKey(keys, stripProviderPrefix(m.ID))
	addLookupKey(keys, m.CanonicalSlug)
	addLookupKey(keys, stripProviderPrefix(m.CanonicalSlug))
	addLookupKey(keys, m.Name)
	return keys
}

func addLookupKey(keys map[string]struct{}, value string) {
	if key := normalizeModelName(value); key != "" {
		keys[key] = struct{}{}
	}
}

func keysetsIntersect(left, right map[string]struct{}) bool {
	for key := range left {
		if _, ok := right[key]; ok {
			return true
		}
	}
	return false
}

func modelMatch(m OpenRouterModel) Match {
	return Match{ID: m.ID, CanonicalSlug: m.CanonicalSlug, Name: m.Name}
}

func stripProviderPrefix(value string) string {
	if before, after, ok := strings.Cut(value, "/"); ok && before != "" && after != "" {
		return after
	}
	return value
}

func providerFromID(id string) string {
	provider, _, ok := strings.Cut(id, "/")
	if !ok {
		return ""
	}
	return provider
}

func normalizeModelName(value string) string {
	var spaced strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			spaced.WriteRune(r)
		} else {
			spaced.WriteByte(' ')
		}
	}
	fields := strings.Fields(spaced.String())
	kept := fields[:0]
	for _, field := range fields {
		if effortLabels[field] {
			continue
		}
		kept = append(kept, field)
	}
	return strings.Join(kept, "")
}

var effortLabels = map[string]bool{
	"xhigh":        true,
	"high":         true,
	"medium":       true,
	"low":          true,
	"minimal":      true,
	"adaptive":     true,
	"reasoning":    true,
	"non":          true,
	"nonreasoning": true,
}

func creatorMatchesProvider(creator, provider string) bool {
	if creator == "" {
		return true
	}
	allowed, ok := creatorProviders[normalizeCreator(creator)]
	if !ok {
		return true
	}
	for _, candidate := range allowed {
		if provider == candidate {
			return true
		}
	}
	return false
}

func normalizeCreator(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var creatorProviders = map[string][]string{
	"ai21labs":       {"ai21"},
	"alibaba":        {"qwen", "alibaba"},
	"anthropic":      {"anthropic"},
	"cohere":         {"cohere"},
	"deepseek":       {"deepseek"},
	"google":         {"google"},
	"googledeepmind": {"google"},
	"meta":           {"meta-llama", "meta"},
	"metallama":      {"meta-llama", "meta"},
	"microsoft":      {"microsoft"},
	"mistral":        {"mistralai", "mistral"},
	"mistralai":      {"mistralai", "mistral"},
	"moonshot":       {"moonshotai"},
	"moonshotai":     {"moonshotai"},
	"nvidia":         {"nvidia"},
	"openai":         {"openai"},
	"qwen":           {"qwen"},
	"xai":            {"x-ai"},
	"zai":            {"z-ai"},
	"zhipuai":        {"z-ai", "zhipuai"},
}
