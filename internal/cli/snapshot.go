// Package cli implements the router subcommands. Keeping the command logic here
// (rather than in package main) lets it be tested black-box from cli_test.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
	"github.com/vegerot/coding-model-router/internal/refresh"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

// Snapshot implements `router snapshot`. It returns a process exit code:
//
//	0  ok
//	1  no data (fetch failed and no cached snapshot to fall back to), or a usage error
//	2  served a stale cached snapshot because a requested refresh failed
//
// Behavior: with a cached snapshot present and no --refresh, it prints from cache
// (no network). Otherwise it fetches via the Artificial Analysis provider, caches
// the result, and prints it.
func Snapshot(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		doRefresh = fs.Bool("refresh", false, "force a re-fetch even if a cached snapshot exists")
		asJSON    = fs.Bool("json", false, "emit the raw snapshot JSON instead of a table")
		cachePath = fs.String("cache", "", "snapshot cache path (default: per-user cache dir)")
		aaApiKey  = fs.String("aa-api-key", "", "Artificial Analysis API key (default: $AA_API_KEY)")
	)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	path := *cachePath
	if path == "" {
		p, err := snapshot.DefaultPath()
		if err != nil {
			fmt.Fprintf(stderr, "router: %v\n", err)
			return 1
		}
		path = p
	}

	s, code := load(path, *doRefresh, *aaApiKey, stderr)
	if s == nil {
		return code
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			fmt.Fprintf(stderr, "router: encode JSON: %v\n", err)
			return 1
		}
		return code
	}

	renderTable(stdout, s)
	return code
}

// load returns the snapshot to display plus the exit code. A nil snapshot means
// fatal (code is 1). When a refresh fails but a cached snapshot is served, stale
// is true and code is 2.
func load(path string, doRefresh bool, aaApiKey string, stderr io.Writer) (s *snapshot.Snapshot, code int) {
	// Fast path: print from cache without touching the network.
	if !doRefresh {
		if cached, err := snapshot.Load(path); err == nil {
			return cached, 0
		}
		// No usable cache → fall through and fetch.
	}

	key := aaApiKey
	if key == "" {
		key = os.Getenv("AA_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fresh, wasStale, err := refresh.Refresh(ctx, refresh.Options{
		Provider:  benchmark_provider.NewAA(key),
		CachePath: path,
		Stderr:    stderr,
	})
	if err != nil {
		if fresh != nil && wasStale {
			// Served last-good; warnings were already printed by Refresh.
			return fresh, 2
		}
		fmt.Fprintf(stderr, "router: %v\n", err)
		return nil, 1
	}
	return fresh, 0
}

func renderTable(w io.Writer, s *snapshot.Snapshot) {
	norm := snapshot.NormalizedQuality(s.Candidates)
	cands := append([]snapshot.Candidate(nil), s.Candidates...)
	sort.Slice(cands, func(i, j int) bool {
		left, right := cands[i], cands[j]
		if norm[left.Slug] != norm[right.Slug] {
			return norm[left.Slug] > norm[right.Slug]
		}
		return left.Slug < right.Slug
	})

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tQUALITY\tNORM\tPROVIDER")
	for _, c := range cands {
		fmt.Fprintf(tw, "%s\t%.1f\t%.2f\t%s\n",
			c.Slug, c.Quality, norm[c.Slug], c.Provider)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%d candidates from %d models · provider %s · fetched %s · %d dropped\n",
		len(s.Candidates), s.Sources.ModelCount, s.Sources.Provider,
		s.FetchedAt.Format(time.RFC3339), len(s.Dropped))
	fmt.Fprintln(w, s.Attribution)
}
