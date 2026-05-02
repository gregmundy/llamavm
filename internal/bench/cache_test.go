package bench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeModelFile(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFingerprint_HexAndLength(t *testing.T) {
	path := writeModelFile(t, bytes(8192, 'x'))
	fp, err := Fingerprint(path)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if len(fp) != 64 {
		t.Fatalf("len(Fingerprint) = %d, want 64 hex chars", len(fp))
	}
	for _, c := range fp {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("Fingerprint contains non-hex char %q (full=%q)", c, fp)
		}
	}
}

func TestFingerprint_DependsOnFirst4KBOnly(t *testing.T) {
	// First 4096 bytes identical, tail differs → fingerprint should match.
	head := bytes(4096, 'a')
	a := writeModelFile(t, append(append([]byte(nil), head...), bytes(1024, 'b')...))
	b := writeModelFile(t, append(append([]byte(nil), head...), bytes(1024, 'c')...))
	fpA, err := Fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	fpB, err := Fingerprint(b)
	if err != nil {
		t.Fatal(err)
	}
	if fpA != fpB {
		t.Fatalf("Fingerprint(a)=%q != Fingerprint(b)=%q (only tail differs)", fpA, fpB)
	}
}

func TestFingerprint_DiffersOnFirst4KBChange(t *testing.T) {
	a := writeModelFile(t, bytes(4096, 'a'))
	b := writeModelFile(t, bytes(4096, 'b'))
	fpA, _ := Fingerprint(a)
	fpB, _ := Fingerprint(b)
	if fpA == fpB {
		t.Fatalf("Fingerprint should differ when first 4KB differs (both=%q)", fpA)
	}
}

func TestFingerprint_FileShorterThan4KB(t *testing.T) {
	// 1KB file: hash whatever is there; do not error.
	path := writeModelFile(t, bytes(1024, 'a'))
	fp, err := Fingerprint(path)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if len(fp) != 64 {
		t.Fatalf("len(Fingerprint) = %d, want 64", len(fp))
	}
}

func TestFingerprint_MissingFile(t *testing.T) {
	if _, err := Fingerprint("/no/such/path/model.gguf"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCache_LookupMissReturnsErrNotFound(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	if _, err := c.Lookup("b5046", "deadbeef"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Lookup miss: got %v, want ErrCacheMiss", err)
	}
}

func TestCache_StoreThenLookup(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	want := Result{
		Version:          "b5046",
		ModelFingerprint: "deadbeef",
		TokensPerSec:     41.7,
		TotalTimeSeconds: 16.9,
		RanAt:            time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
	}
	if err := c.Store(want); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := c.Lookup("b5046", "deadbeef")
	if err != nil {
		t.Fatalf("Lookup hit: %v", err)
	}
	// RanAt round-trips through JSON in RFC3339 form; compare via Equal.
	if got.Version != want.Version ||
		got.ModelFingerprint != want.ModelFingerprint ||
		got.TokensPerSec != want.TokensPerSec ||
		got.TotalTimeSeconds != want.TotalTimeSeconds ||
		!got.RanAt.Equal(want.RanAt) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestCache_CorruptFileIsTreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a malformed JSON file at the path Lookup would read.
	bad := filepath.Join(dir, "b5046_deadbeef.json")
	if err := os.WriteFile(bad, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Lookup("b5046", "deadbeef"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Lookup of corrupt file: got %v, want ErrCacheMiss", err)
	}
}

func TestCache_CacheFilenameShape(t *testing.T) {
	c := &Cache{Dir: "/cache"}
	got := c.Path("b5046", "abcd1234")
	want := "/cache/b5046_abcd1234.json"
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestCache_StoreCreatesDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "deep", "benchmarks") // does not exist yet
	c := &Cache{Dir: dir}
	res := Result{Version: "b5046", ModelFingerprint: "fp", TokensPerSec: 1, TotalTimeSeconds: 1, RanAt: time.Now()}
	if err := c.Store(res); err != nil {
		t.Fatalf("Store on non-existent dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Store should create the cache dir: %v", err)
	}
}

func TestResult_JSONShape(t *testing.T) {
	// Lock down the exact field names so cache files written today stay
	// readable later.
	c := &Cache{Dir: t.TempDir()}
	res := Result{
		Version:          "b5046",
		ModelFingerprint: "fp",
		TokensPerSec:     1.5,
		TotalTimeSeconds: 2.5,
		RanAt:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := c.Store(res); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(c.Path("b5046", "fp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"version"`, `"model_fingerprint"`, `"tokens_per_sec"`, `"total_time_seconds"`, `"ran_at"`,
	} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("cache JSON missing %s; body=%s", key, body)
		}
	}
}

// bytes returns a slice of length n filled with c.
func bytes(n int, c byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return b
}
