package cli

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/vegerot/coding-model-router/internal/proxy"
)

// Serve implements `router serve`: load the cached/refreshed snapshot, resolve
// to the routable candidate set, and run an OpenAI-compatible HTTP proxy that
// routes pareto@p requests to OpenRouter.
func Serve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		addr                     = fs.String("addr", "127.0.0.1:4000", "listen address")
		p                        = fs.Float64("p", 0.67, "default quality floor for a bare `pareto` model name")
		doRefresh                = fs.Bool("refresh", false, "refresh the snapshot and any required OpenRouter catalog cache")
		cachePath                = fs.String("cache", "", "snapshot cache path (default: per-user cache dir)")
		benchmarkProvider        = fs.String("benchmark-provider", defaultBenchmarkProvider, "benchmark provider: aa or openrouter")
		artificialAnalysisAPIKey = fs.String("aa-api-key", "", "Artificial Analysis API key (default: $AA_API_KEY)")
		openRouterKey            = fs.String("openrouter-api-key", "", "OpenRouter API key (default: $OPENROUTER_API_KEY)")
		openRouterPath           = fs.String("openrouter-cache", "", "OpenRouter catalog cache path (default: per-user cache dir)")
	)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	mapped, _, code := loadMappedSnapshot(*cachePath, *openRouterPath, *doRefresh, *benchmarkProvider, *artificialAnalysisAPIKey, *openRouterKey, stderr)
	if mapped == nil {
		return code
	}
	if len(mapped.Candidates) == 0 {
		fmt.Fprintln(stderr, "router: no candidates resolved to OpenRouter IDs; nothing to route")
		return 1
	}

	key := *openRouterKey
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		fmt.Fprintln(stderr, "router: WARNING: no OpenRouter API key (set OPENROUTER_API_KEY or --openrouter-api-key); requests must supply their own Authorization header")
	}

	srv, err := proxy.NewServer(proxy.Config{
		Snapshot:      mapped,
		DefaultP:      *p,
		OpenRouterKey: key,
		Referer:       "https://github.com/vegerot/coding-model-router",
		Title:         "coding-model-router",
		Logger:        stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "router: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "router serve: listening on http://%s · default p=%.4g · %d mapped candidates\n",
		*addr, *p, len(mapped.Candidates))
	fmt.Fprintln(stdout, mapped.Attribution)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		fmt.Fprintf(stderr, "router: serve: %v\n", err)
		return 1
	}
	return code
}
