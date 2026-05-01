package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrETagMismatch is returned when a dashboard save is based on stale content.
	ErrETagMismatch = errors.New("dashboard changed on disk")
)

// FileETag returns a stable content hash for optimistic saves.
func FileETag(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

// SaveAtomic writes next to path using an etag check and same-directory rename.
func SaveAtomic(path string, next []byte, expectedETag string) error {
	current, err := os.ReadFile(path)
	if err == nil && expectedETag != "" && FileETag(current) != expectedETag {
		return ErrETagMismatch
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current dashboard: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dashboard-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp dashboard: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(next); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp dashboard: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp dashboard: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace dashboard: %w", err)
	}
	cleanup = false
	return nil
}
