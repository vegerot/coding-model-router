// Command router is a Pareto-style coding-model router.
//
// It exposes a single quality knob p ∈ [0,1] and, given Artificial Analysis's
// coding index plus blended per-token pricing, selects the cheapest model whose
// normalized quality is at or above p.
//
// Subcommands:
//
//	router snapshot   build/show the candidate model snapshot (data layer; M1)
//	router select     choose a model from the cached/refreshed snapshot (M2)
//	router mappings   resolve candidates to OpenRouter IDs (M3)
//	router serve      (future, M4) run the OpenAI-compatible proxy
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/vegerot/coding-model-router/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "snapshot":
		return cli.Snapshot(args[1:], stdout, stderr)
	case "select":
		return cli.Select(args[1:], stdout, stderr)
	case "mappings":
		return cli.Mappings(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "router: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `router — Pareto-style coding-model router

Usage:
  router snapshot [--refresh] [--json] [--cache PATH]
      Build or display the candidate model snapshot (quality + blended cost).

  router select [--p P] [--refresh] [--json] [--cache PATH] [--mapped-only]
      Select the cheapest model at or above quality floor P.

  router mappings [--refresh] [--json] [--cache PATH] [--openrouter-cache PATH]
      Resolve snapshot candidates to OpenRouter model IDs.

  router help
      Show this help.

Data: Artificial Analysis (https://artificialanalysis.ai).
	`)
}
