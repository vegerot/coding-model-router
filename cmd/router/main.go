// Command router is a Pareto-style coding-model router.
//
// It exposes a single quality knob p ∈ [0,1] and, given the Artificial Analysis
// coding-agents leaderboard plus token-usage-weighted OpenRouter pricing, selects
// the cheapest model whose normalized quality is at or above p.
//
// Subcommands:
//
//	router snapshot   build/show the candidate model snapshot (data layer; M1)
//	router serve      (future, M3) run the OpenAI-compatible proxy
package main

import (
	"context"
	"fmt"
	"io"
	"os"
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
		return cmdSnapshot(context.Background(), args[1:], stdout, stderr)
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
      Build or display the candidate model snapshot (quality + token-weighted cost).

  router help
      Show this help.

Data: Artificial Analysis Coding Agent Index (https://artificialanalysis.ai/agents/coding-agents);
pricing via models.dev / OpenRouter.
`)
}
