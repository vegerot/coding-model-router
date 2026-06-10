// Package proxy implements the M4 OpenAI-compatible HTTP proxy. It reads the
// quality knob p off each /v1/chat/completions request, selects the cheapest
// model at or above the floor over the mapped candidate set, rewrites the model
// field to its OpenRouter ID, and forwards the request to OpenRouter with a
// flushing streaming passthrough.
package proxy

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// knobModelName is the magic model name that triggers routing.
const knobModelName = "pareto"

// Decision is the routing outcome for one request's model field plus headers.
type Decision struct {
	Route bool    // true → run the engine at P; false → forward the model unchanged
	P     float64 // the quality floor to route at; valid only when Route is true
}

// ParseKnob interprets the request's model field and X-Pareto-P header.
//
//   - "pareto"        → route at defaultP
//   - "pareto@0.7"    → route at 0.7
//   - any other model → passthrough (Route=false); the header is ignored
//
// A present, non-empty X-Pareto-P header overrides the "@" suffix when the
// request routes. A malformed p — non-numeric, or outside [0,1] — returns an
// error, which the handler maps to HTTP 400.
func ParseKnob(model, paretoHeader string, defaultP float64) (Decision, error) {
	model = strings.TrimSpace(model)

	p := defaultP
	switch {
	case model == knobModelName:
		// bare pareto → default p (possibly overridden by the header below)
	case strings.HasPrefix(model, knobModelName+"@"):
		v, err := parseP(strings.TrimPrefix(model, knobModelName+"@"))
		if err != nil {
			return Decision{}, fmt.Errorf("invalid model %q: %w", model, err)
		}
		p = v
	default:
		return Decision{Route: false}, nil
	}

	if h := strings.TrimSpace(paretoHeader); h != "" {
		v, err := parseP(h)
		if err != nil {
			return Decision{}, fmt.Errorf("invalid X-Pareto-P header %q: %w", paretoHeader, err)
		}
		p = v
	}
	return Decision{Route: true, P: p}, nil
}

func parseP(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("p must be a number in [0,1], got %q", s)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return 0, fmt.Errorf("p must be in [0,1], got %v", v)
	}
	return v, nil
}
