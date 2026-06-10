package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/vegerot/coding-model-router/internal/cli"
)

// Serve blocks on ListenAndServe once data loads, so the unit-testable paths are
// the early returns: bad flags and missing data exit non-zero before listening.
// The handler behavior itself is covered by internal/proxy tests.

func TestServeRejectsBadFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cli.Serve([]string{"--nope"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit for bad flag")
	}
}

func TestServeExitsWhenNoData(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	dir := t.TempDir()

	var out, errOut bytes.Buffer
	code := cli.Serve([]string{
		"--cache", filepath.Join(dir, "absent-snapshot.json"),
		"--openrouter-cache", filepath.Join(dir, "absent-catalog.json"),
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit when no snapshot/key is available")
	}
}
