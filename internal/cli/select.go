package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/vegerot/coding-model-router/internal/engine"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

// Select implements `router select`: load a snapshot, run the pure engine, and
// print the selected primary model plus ordered fallbacks.
func Select(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		p         = fs.Float64("p", 0, "quality floor in [0,1]")
		doRefresh = fs.Bool("refresh", false, "force a re-fetch even if a cached snapshot exists")
		asJSON    = fs.Bool("json", false, "emit the selection plan as JSON instead of a table")
		cachePath = fs.String("cache", "", "snapshot cache path (default: per-user cache dir)")
		apiKey    = fs.String("api-key", "", "Artificial Analysis API key (default: $AA_API_KEY)")
	)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	path := *cachePath
	if path == "" {
		defaultPath, err := snapshot.DefaultPath()
		if err != nil {
			fmt.Fprintf(stderr, "router: %v\n", err)
			return 1
		}
		path = defaultPath
	}

	s, _, code := load(path, *doRefresh, *apiKey, stderr)
	if s == nil {
		return code
	}

	plan, err := engine.Select(s, *p, engine.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "router: %v\n", err)
		return 1
	}

	if *asJSON {
		out := selectJSON{
			Attribution: s.Attribution,
			FetchedAt:   s.FetchedAt,
			Sources:     s.Sources,
			Plan:        plan,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "router: encode JSON: %v\n", err)
			return 1
		}
		return code
	}

	renderPlan(stdout, s, plan)
	return code
}

type selectJSON struct {
	Attribution string              `json:"attribution"`
	FetchedAt   time.Time           `json:"fetchedAt"`
	Sources     snapshot.SourceMeta `json:"sources"`
	Plan        engine.Plan         `json:"plan"`
}

func renderPlan(w io.Writer, s *snapshot.Snapshot, plan engine.Plan) {
	norm := snapshot.NormalizedQuality(s.Candidates)

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ROLE\tMODEL\tQUALITY\tNORM\tBLENDED$/1M\tPROVIDER")
	printPlanRow(tw, "primary", plan.Primary, norm)
	for i, c := range plan.Fallbacks {
		printPlanRow(tw, fmt.Sprintf("fallback-%d", i+1), c, norm)
	}
	tw.Flush()

	fmt.Fprintf(w, "\np=%.4g · %d fallbacks · fetched %s\n",
		plan.P, len(plan.Fallbacks), s.FetchedAt.Format(time.RFC3339))
	fmt.Fprintln(w, s.Attribution)
}

func printPlanRow(w io.Writer, role string, c snapshot.Candidate, norm map[string]float64) {
	fmt.Fprintf(w, "%s\t%s\t%.1f\t%.2f\t%.4g\t%s\n",
		role, c.Slug, c.Quality, norm[c.Slug], c.BlendedPricePer1M, c.Provider)
}
