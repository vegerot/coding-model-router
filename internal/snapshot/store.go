package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath returns the on-disk snapshot location:
// $XDG_CACHE_HOME/coding-model-router/snapshot.json (→ ~/.cache/... on Linux).
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir: %w", err)
	}
	return filepath.Join(dir, "coding-model-router", "snapshot.json"), nil
}

// Load reads and decodes a snapshot from path. It returns an error if the file
// is absent, malformed, or written by a different SchemaVersion.
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot %s: %w", path, err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode snapshot %s: %w", path, err)
	}
	if s.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("snapshot %s has schema version %d, want %d (stale cache; refresh)",
			path, s.SchemaVersion, SchemaVersion)
	}
	return &s, nil
}

// Save writes s to path atomically: it creates the parent directory, writes to
// a temp file in the same directory, fsyncs, and renames over the target. A
// failed write never corrupts or truncates an existing snapshot.
func Save(path string, s *Snapshot) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create snapshot dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "snapshot-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp snapshot: %w", err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any error path (rename makes this a no-op on success).
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		tmp.Close()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp snapshot: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp snapshot: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp snapshot: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename snapshot into place: %w", err)
	}
	return nil
}
