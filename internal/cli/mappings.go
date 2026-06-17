package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/vegerot/coding-model-router/internal/mapping"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

// Mappings implements `router mappings`: resolve cached/refreshed AA snapshot
// candidates to OpenRouter IDs and print diagnostics.
func Mappings(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mappings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		doRefresh      = fs.Bool("refresh", false, "refresh both snapshot and OpenRouter catalog caches")
		asJSON         = fs.Bool("json", false, "emit mapping diagnostics as JSON instead of a table")
		cachePath      = fs.String("cache", "", "snapshot cache path (default: per-user cache dir)")
		apiKey         = fs.String("aa-api-key", "", "Artificial Analysis API key (default: $AA_API_KEY)")
		openRouterPath = fs.String("openrouter-cache", "", "OpenRouter catalog cache path (default: per-user cache dir)")
	)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	s, report, code := loadMappingReport(*cachePath, *openRouterPath, *doRefresh, *apiKey, stderr)
	if s == nil {
		return code
	}

	if *asJSON {
		out := mappingsJSON{
			Attribution: s.Attribution,
			Sources:     s.Sources,
			Report:      report,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "router: encode JSON: %v\n", err)
			return 1
		}
		return code
	}

	renderMappings(stdout, s, report)
	return code
}

type mappingsJSON struct {
	Attribution string              `json:"attribution"`
	Sources     snapshot.SourceMeta `json:"sources"`
	Report      mapping.Report      `json:"report"`
}

func renderMappings(w io.Writer, s *snapshot.Snapshot, report mapping.Report) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tSTATUS\tOPENROUTER_ID\tQUALITY\tREASON")
	for _, r := range report.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f\t%s\n",
			r.Candidate.Slug, r.Status, r.OpenRouterID, r.Candidate.Quality, r.Reason)
	}
	tw.Flush()

	unmapped := topUnmapped(report, 10)
	if len(unmapped) > 0 {
		fmt.Fprintln(w, "\nTop unmapped by AA coding quality:")
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "MODEL\tQUALITY\tCREATOR")
		for _, r := range unmapped {
			fmt.Fprintf(tw, "%s\t%.1f\t%s\n", r.Candidate.Slug, r.Candidate.Quality, r.Candidate.Creator)
		}
		tw.Flush()
	}

	fmt.Fprintf(w, "\n%d/%d mapped (%.1f%%) · %d unmapped · %d ambiguous · OpenRouter catalog fetched %s · snapshot fetched %s\n",
		report.Summary.Mapped, report.Summary.Total, report.Summary.MappedPercent,
		report.Summary.Unmapped, report.Summary.Ambiguous,
		report.CatalogFetchedAt.Format(time.RFC3339), s.FetchedAt.Format(time.RFC3339))
	fmt.Fprintln(w, s.Attribution)
}

func topUnmapped(report mapping.Report, limit int) []mapping.Result {
	var rows []mapping.Result
	for _, r := range report.Results {
		if r.Status == mapping.StatusUnmapped || r.Status == mapping.StatusAmbiguous {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Candidate.Quality != rows[j].Candidate.Quality {
			return rows[i].Candidate.Quality > rows[j].Candidate.Quality
		}
		return rows[i].Candidate.Slug < rows[j].Candidate.Slug
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func loadMappedSnapshot(cachePath, openRouterPath string, doRefresh bool, apiKey string, stderr io.Writer) (*snapshot.Snapshot, mapping.Report, int) {
	s, report, code := loadMappingReport(cachePath, openRouterPath, doRefresh, apiKey, stderr)
	if s == nil {
		return nil, mapping.Report{}, code
	}
	return mapping.MappedSnapshot(s, report), report, code
}

func loadMappingReport(cachePath, openRouterPath string, doRefresh bool, apiKey string, stderr io.Writer) (*snapshot.Snapshot, mapping.Report, int) {
	path, err := resolveSnapshotPath(cachePath)
	if err != nil {
		fmt.Fprintf(stderr, "router: %v\n", err)
		return nil, mapping.Report{}, 1
	}
	catalogPath, err := resolveOpenRouterCatalogPath(openRouterPath)
	if err != nil {
		fmt.Fprintf(stderr, "router: %v\n", err)
		return nil, mapping.Report{}, 1
	}

	s, _, code := load(path, doRefresh, apiKey, stderr)
	if s == nil {
		return nil, mapping.Report{}, code
	}
	catalog, catalogCode := loadOpenRouterCatalog(catalogPath, doRefresh, stderr)
	if catalog == nil {
		return nil, mapping.Report{}, catalogCode
	}
	code = combineCodes(code, catalogCode)
	report, err := mapping.Resolve(s, catalog)
	if err != nil {
		fmt.Fprintf(stderr, "router: %v\n", err)
		return nil, mapping.Report{}, 1
	}
	return s, report, code
}

func resolveOpenRouterCatalogPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return mapping.DefaultCatalogPath()
}

func loadOpenRouterCatalog(path string, doRefresh bool, stderr io.Writer) (*mapping.Catalog, int) {
	if !doRefresh {
		if cached, err := mapping.LoadCatalog(path); err == nil {
			return cached, 0
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fresh, err := mapping.FetchCatalog(ctx, nil)
	if err != nil {
		if cached, cacheErr := mapping.LoadCatalog(path); cacheErr == nil {
			fmt.Fprintf(stderr, "router: OpenRouter catalog refresh failed (%v); using stale cache\n", err)
			return cached, 2
		}
		fmt.Fprintf(stderr, "router: %v\n", err)
		return nil, 1
	}
	if err := mapping.SaveCatalog(path, fresh); err != nil {
		fmt.Fprintf(stderr, "router: cache OpenRouter catalog: %v\n", err)
	}
	return fresh, 0
}

func combineCodes(a, b int) int {
	if a == 1 || b == 1 {
		return 1
	}
	if a == 2 || b == 2 {
		return 2
	}
	return 0
}

func resolveSnapshotPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return snapshot.DefaultPath()
}
