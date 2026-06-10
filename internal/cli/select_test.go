package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vegerot/coding-model-router/internal/cli"
)

func TestSelectPrintsPlanFromCache(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{"--cache", path, "--p", "0.5"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}

	got := out.String()
	for _, want := range []string{"ROLE", "primary", "mid", "fallback-1", "pricey-top", "Artificial Analysis"} {
		if !strings.Contains(got, want) {
			t.Errorf("select output missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "cheap-low") {
		t.Errorf("cheap-low should not qualify at p=0.5\n---\n%s", got)
	}
}

func TestSelectJSON(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{"--cache", path, "--p", "1", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}

	var decoded struct {
		Attribution string `json:"attribution"`
		Plan        struct {
			P       float64 `json:"p"`
			Primary struct {
				Slug string `json:"slug"`
			} `json:"primary"`
			Fallbacks []struct {
				Slug string `json:"slug"`
			} `json:"fallbacks"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}
	if decoded.Plan.P != 1 || decoded.Plan.Primary.Slug != "pricey-top" {
		t.Errorf("decoded plan wrong: %+v", decoded.Plan)
	}
	if len(decoded.Plan.Fallbacks) != 0 {
		t.Errorf("p=1 should have no fallbacks in sample snapshot, got %+v", decoded.Plan.Fallbacks)
	}
	if !strings.Contains(decoded.Attribution, "Artificial Analysis") {
		t.Errorf("missing attribution in JSON: %q", decoded.Attribution)
	}
}

func TestSelectRejectsInvalidP(t *testing.T) {
	t.Setenv("AA_API_KEY", "")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	seedSnapshot(t, path)

	var out, errOut bytes.Buffer
	code := cli.Select([]string{"--cache", path, "--p", "2"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "p must be in [0,1]") {
		t.Errorf("stderr missing p validation error: %s", errOut.String())
	}
}
