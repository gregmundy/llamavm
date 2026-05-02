// Package bench implements per-version benchmarking and result caching.
package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// fingerprintBytes is the prefix of the model file hashed into the cache key.
// PRD §3.10.1 specifies "first 4KB" — using a small prefix keeps fingerprinting
// cheap on multi-gigabyte models.
const fingerprintBytes = 4096

// ErrCacheMiss signals that no cached result exists for (tag, fingerprint),
// or that the cache file is unreadable / malformed.
var ErrCacheMiss = errors.New("cache miss")

// Result is the persisted benchmark outcome; one file per (Version, ModelFingerprint).
type Result struct {
	Version          string    `json:"version"`
	ModelFingerprint string    `json:"model_fingerprint"`
	TokensPerSec     float64   `json:"tokens_per_sec"`
	TotalTimeSeconds float64   `json:"total_time_seconds"`
	RanAt            time.Time `json:"ran_at"`
	// Cached is true when this Result was returned from the cache rather than
	// from a fresh run. Not persisted.
	Cached bool `json:"-"`
}

// Fingerprint returns the lowercase-hex SHA-256 of the first fingerprintBytes
// bytes of the file at path. If the file is shorter than fingerprintBytes,
// the entire file is hashed. Returns an error if the file cannot be opened.
func Fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyN(h, f, fingerprintBytes); err != nil && err != io.EOF {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cache reads and writes benchmark results in Dir. Dir is created on first
// Store(); Lookup tolerates a missing Dir by returning ErrCacheMiss.
type Cache struct {
	Dir string
}

// Path returns the cache file path for (tag, fingerprint).
func (c *Cache) Path(tag, fingerprint string) string {
	return filepath.Join(c.Dir, tag+"_"+fingerprint+".json")
}

// Lookup returns the cached Result for (tag, fingerprint), or ErrCacheMiss if
// the file is missing or malformed. A malformed cache file is treated as a
// miss (not an error) so a manual edit can't break future runs.
func (c *Cache) Lookup(tag, fingerprint string) (Result, error) {
	body, err := os.ReadFile(c.Path(tag, fingerprint))
	if err != nil {
		return Result{}, ErrCacheMiss
	}
	var r Result
	if err := json.Unmarshal(body, &r); err != nil {
		return Result{}, ErrCacheMiss
	}
	r.Cached = true
	return r, nil
}

// Store writes the Result to disk atomically (temp + rename).
func (c *Cache) Store(r Result) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("ensure cache dir: %w", err)
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	dst := c.Path(r.Version, r.ModelFingerprint)
	tmp, err := os.CreateTemp(c.Dir, ".bench-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
