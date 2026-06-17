// Package mapping resolves Artificial Analysis candidates to OpenRouter model
// IDs using deterministic rules and a cached OpenRouter model catalog.
package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	CatalogSchemaVersion = 2
	CatalogURL           = "https://openrouter.ai/api/v1/models"
	CatalogSource        = "openrouter"
)

// Catalog is the cached subset of OpenRouter model-list data needed for
// deterministic AA slug/name to OpenRouter ID resolution.
type Catalog struct {
	SchemaVersion int               `json:"schemaVersion"`
	FetchedAt     time.Time         `json:"fetchedAt"`
	Source        string            `json:"source"`
	Models        []OpenRouterModel `json:"models"`
}

// OpenRouterModel is the stable model identity data exposed by OpenRouter's
// /api/v1/models endpoint. Extra fields from the API are intentionally ignored.
type OpenRouterModel struct {
	ID            string       `json:"id"`
	CanonicalSlug string       `json:"canonical_slug,omitempty"`
	Name          string       `json:"name"`
	ContextLength int          `json:"context_length,omitempty"`
	Architecture  Architecture `json:"architecture,omitempty"`
	Pricing       Pricing      `json:"pricing,omitempty"`
}

type Pricing struct {
	Prompt         string `json:"prompt,omitempty"`
	Completion     string `json:"completion,omitempty"`
	InputCacheRead string `json:"input_cache_read,omitempty"`
}

type Architecture struct {
	Modality         string   `json:"modality,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	Tokenizer        string   `json:"tokenizer,omitempty"`
	InstructType     string   `json:"instruct_type,omitempty"`
}

// DefaultCatalogPath returns the on-disk OpenRouter catalog cache location.
func DefaultCatalogPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir: %w", err)
	}
	return filepath.Join(dir, "coding-model-router", "openrouter-models.json"), nil
}

// FetchCatalog downloads the current OpenRouter model catalog.
func FetchCatalog(ctx context.Context, client *http.Client) (*Catalog, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CatalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create OpenRouter catalog request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch OpenRouter catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch OpenRouter catalog: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Data []OpenRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter catalog: %w", err)
	}
	return &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		FetchedAt:     time.Now().UTC(),
		Source:        CatalogSource,
		Models:        payload.Data,
	}, nil
}

func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OpenRouter catalog %s: %w", path, err)
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode OpenRouter catalog %s: %w", path, err)
	}
	if c.SchemaVersion != CatalogSchemaVersion {
		return nil, fmt.Errorf("OpenRouter catalog %s has schema version %d, want %d (stale cache; refresh)",
			path, c.SchemaVersion, CatalogSchemaVersion)
	}
	return &c, nil
}

func SaveCatalog(path string, c *Catalog) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create OpenRouter catalog dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "openrouter-models-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp OpenRouter catalog: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		tmp.Close()
		return fmt.Errorf("encode OpenRouter catalog: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp OpenRouter catalog: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp OpenRouter catalog: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp OpenRouter catalog: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename OpenRouter catalog into place: %w", err)
	}
	return nil
}
