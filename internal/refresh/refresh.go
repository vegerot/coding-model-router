package refresh

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vegerot/coding-model-router/internal/benchmark_provider"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

// Options configures a Refresh. Provider and CachePath are required; the rest
// have sensible zero-value defaults.
type Options struct {
	Provider  benchmark_provider.BenchmarkProvider // benchmark data source (required)
	CachePath string                               // where the snapshot is persisted (required)
	Client    *http.Client                         // HTTP client; nil → http.DefaultClient
	Now       func() time.Time                     // clock; nil → time.Now
	Stderr    io.Writer                            // warning sink; nil → io.Discard
}

// Refresh fetches from the provider, builds and validates a snapshot, and saves
// it atomically.
//
// On success it returns (snapshot, false, nil). On any failure (fetch, validate,
// or save) it attempts to serve the last-good cached snapshot: if one exists it
// returns (lastGood, true, causalErr); otherwise (nil, false, causalErr). The
// existing cache is never deleted or overwritten on failure.
func (o Options) normalize() Options {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	return o
}

// Refresh runs the fetch → build → validate → save pipeline. See Options.
func Refresh(ctx context.Context, opts Options) (*snapshot.Snapshot, bool, error) {
	o := opts.normalize()

	s, err := buildValidated(ctx, o)
	if err != nil {
		// Try to serve the last-good snapshot.
		if cached, loadErr := snapshot.Load(o.CachePath); loadErr == nil {
			age := o.Now().Sub(cached.FetchedAt).Round(time.Second)
			fmt.Fprintf(o.Stderr, "WARNING: refresh failed: %v\n", err)
			fmt.Fprintf(o.Stderr, "WARNING: serving cached snapshot from %s (%s old)\n",
				cached.FetchedAt.Format(time.RFC3339), age)
			return cached, true, err
		}
		return nil, false, err
	}

	if err := snapshot.Save(o.CachePath, s); err != nil {
		// Build succeeded but persistence failed; serve what we built, flag stale
		// only if we fell back, which we didn't — return the fresh snapshot but
		// surface the save error.
		return s, false, fmt.Errorf("refresh: built snapshot but failed to save: %w", err)
	}
	return s, false, nil
}

func buildValidated(ctx context.Context, o Options) (*snapshot.Snapshot, error) {
	models, err := o.Provider.Fetch(ctx, o.Client)
	if err != nil {
		return nil, fmt.Errorf("refresh: fetch from %s: %w", o.Provider.Name(), err)
	}
	s := Build(models, o.Provider.Name(), o.Now())
	if err := Validate(s); err != nil {
		return nil, err
	}
	return s, nil
}
