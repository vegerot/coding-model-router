package proxy_test

import (
	"testing"

	"github.com/vegerot/coding-model-router/internal/proxy"
)

func TestParseKnob(t *testing.T) {
	const def = 0.67
	tests := []struct {
		name      string
		model     string
		header    string
		wantRoute bool
		wantP     float64
		wantErr   bool
	}{
		{name: "bare pareto uses default", model: "pareto", wantRoute: true, wantP: 0.67},
		{name: "trimmed bare pareto", model: "  pareto  ", wantRoute: true, wantP: 0.67},
		{name: "pareto with suffix", model: "pareto@0.7", wantRoute: true, wantP: 0.7},
		{name: "suffix zero", model: "pareto@0", wantRoute: true, wantP: 0},
		{name: "suffix one", model: "pareto@1", wantRoute: true, wantP: 1},
		{name: "header overrides suffix", model: "pareto@0.7", header: "0.95", wantRoute: true, wantP: 0.95},
		{name: "header overrides bare", model: "pareto", header: "0.2", wantRoute: true, wantP: 0.2},
		{name: "passthrough model", model: "openai/gpt-4o", wantRoute: false},
		{name: "passthrough ignores header", model: "openai/gpt-4o", header: "0.5", wantRoute: false},
		{name: "malformed suffix non-numeric", model: "pareto@abc", wantErr: true},
		{name: "suffix above range", model: "pareto@2", wantErr: true},
		{name: "suffix negative", model: "pareto@-0.1", wantErr: true},
		{name: "empty suffix", model: "pareto@", wantErr: true},
		{name: "malformed header", model: "pareto", header: "nope", wantErr: true},
		{name: "header above range", model: "pareto", header: "1.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := proxy.ParseKnob(tt.model, tt.header, def)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Route != tt.wantRoute {
				t.Fatalf("Route = %v, want %v", got.Route, tt.wantRoute)
			}
			if got.Route && got.P != tt.wantP {
				t.Fatalf("P = %v, want %v", got.P, tt.wantP)
			}
		})
	}
}
