# llamavm M1 — install + uninstall + list Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `install`, `uninstall`, and `list` subcommands so that `llamavm install b5046` builds and atomically stages the version, `llamavm list` shows it (asterisk on active), and `llamavm uninstall b5046` removes it cleanly. No shims yet — built binaries are reachable via `~/.llamavm/versions/<tag>/bin/`.

**Architecture:** Layered Go packages following PRD §5.3. `internal/version` owns the on-disk layout under `~/.llamavm/`. `internal/github` is a tiny REST client for the llama.cpp releases API. `internal/builder` runs cmake via an injected `CommandRunner` so unit tests don't shell out. `internal/cli` defines narrow consumer-side interfaces (Store, GitHubClient, Builder, CommandRunner, Platform) and orchestrates the flow; real implementations are wired in from `cmd/llamavm/main.go`. Atomicity is enforced by building inside a `.staging-<tag>` directory and renaming onto `<tag>` only on success — failed installs never appear in `list`.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` v1.10.2, stdlib `net/http`, `encoding/json`, `os/exec`, `runtime`. No new dependencies.

**Out of scope for this plan (deferred to later milestones):** shims (M2), `use`/`current`/`pin` (M2/M3), `bench` (M4), `doctor`/Homebrew/release (M5), integration tests against a real llama.cpp tag (M5).

---

## File Structure

**Created:**
- `internal/version/store.go` — versions dir CRUD + active version file (atomic writes)
- `internal/version/store_test.go`
- `internal/github/releases.go` — releases REST client (`Latest`, `TagExists`)
- `internal/github/releases_test.go`
- `internal/builder/platform.go` — Apple Silicon + cores detection
- `internal/builder/platform_test.go`
- `internal/builder/builder.go` — cmake configure + build via `CommandRunner`
- `internal/builder/builder_test.go`
- `internal/cli/deps.go` — narrow consumer-side interfaces + `Deps` struct
- `internal/cli/root.go` — root cobra command, registers subcommands
- `internal/cli/list.go` + `list_test.go`
- `internal/cli/uninstall.go` + `uninstall_test.go`
- `internal/cli/install.go` + `install_test.go`

**Modified:**
- `cmd/llamavm/main.go` — wire real implementations into `Deps`, build root via `cli.NewRoot`

**Layout produced on disk by a successful install:**

```
~/.llamavm/
├── versions/
│   └── b5046/
│       ├── source/         # full clone (build dir lives inside)
│       └── bin/            # symlinks → source/build/bin/{llama-cli,llama-server,llama-quantize}
├── current                 # text file: "b5046\n" (set on first install)
└── logs/
    └── b5046-20260502T143030.log   # written only on failed install
```

During an install in flight, the directory at `~/.llamavm/versions/.staging-b5046/` is the work-in-progress location. It either gets renamed onto `versions/b5046/` (success) or removed entirely (failure).

---

## Task 1: Version Store

Owns all path knowledge under `<home>/.llamavm/` and provides the atomic primitives the install/uninstall flows compose. No network, no exec.

**Files:**
- Create: `internal/version/store.go`
- Test: `internal/version/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/version/store_test.go`:

```go
package version

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(t.TempDir())
}

func TestStore_PathsAndRoot(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if got, want := s.Root(), filepath.Join(home, ".llamavm"); got != want {
		t.Fatalf("Root = %q, want %q", got, want)
	}
	if got, want := s.VersionsDir(), filepath.Join(home, ".llamavm", "versions"); got != want {
		t.Fatalf("VersionsDir = %q, want %q", got, want)
	}
	if got, want := s.VersionDir("b5046"), filepath.Join(home, ".llamavm", "versions", "b5046"); got != want {
		t.Fatalf("VersionDir = %q, want %q", got, want)
	}
	if got, want := s.StagingDir("b5046"), filepath.Join(home, ".llamavm", "versions", ".staging-b5046"); got != want {
		t.Fatalf("StagingDir = %q, want %q", got, want)
	}
	if got, want := s.CurrentFile(), filepath.Join(home, ".llamavm", "current"); got != want {
		t.Fatalf("CurrentFile = %q, want %q", got, want)
	}
	if got, want := s.LogsDir(), filepath.Join(home, ".llamavm", "logs"); got != want {
		t.Fatalf("LogsDir = %q, want %q", got, want)
	}
}

func TestStore_IsInstalled(t *testing.T) {
	s := newTestStore(t)
	if s.IsInstalled("b5046") {
		t.Fatal("expected not installed on empty store")
	}
	if err := os.MkdirAll(s.VersionDir("b5046"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !s.IsInstalled("b5046") {
		t.Fatal("expected installed after mkdir")
	}
}

func TestStore_List_HidesDotEntriesAndSorts(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List()
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}

	for _, name := range []string{"b5489", "b5046", "b5400", ".staging-b9000"} {
		if err := os.MkdirAll(filepath.Join(s.VersionsDir(), name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b5046", "b5400", "b5489"}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("List not sorted: %v", got)
	}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("List[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestStore_ActiveRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Active(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Active on empty: got %v, want ErrNoActiveVersion", err)
	}
	if err := s.SetActive("b5046"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	got, err := s.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Active = %q, want b5046", got)
	}
	if err := s.ClearActive(); err != nil {
		t.Fatalf("ClearActive: %v", err)
	}
	if _, err := s.Active(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Active after clear: got %v, want ErrNoActiveVersion", err)
	}
	// ClearActive on missing file is a no-op.
	if err := s.ClearActive(); err != nil {
		t.Fatalf("ClearActive on missing: %v", err)
	}
}

func TestStore_Active_TolerantOfTrailingNewline(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.CurrentFile(), []byte("  b5046\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Active()
	if err != nil {
		t.Fatal(err)
	}
	if got != "b5046" {
		t.Fatalf("Active = %q, want b5046", got)
	}
}

func TestStore_Remove(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.VersionDir("b5046"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("b5046"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s.IsInstalled("b5046") {
		t.Fatal("expected version to be gone")
	}
	if err := s.Remove("b5046"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Remove on missing: got %v, want ErrNotInstalled", err)
	}
}

func TestStore_PromoteStaging(t *testing.T) {
	s := newTestStore(t)
	staging := s.StagingDir("b5046")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(staging, "marker")
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.PromoteStaging("b5046"); err != nil {
		t.Fatalf("PromoteStaging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.VersionDir("b5046"), "marker")); err != nil {
		t.Fatalf("marker not in final dir: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging dir should be gone, stat err = %v", err)
	}
}

func TestStore_RemoveStaging(t *testing.T) {
	s := newTestStore(t)
	staging := s.StagingDir("b5046")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveStaging("b5046"); err != nil {
		t.Fatalf("RemoveStaging: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging dir should be gone")
	}
	// Idempotent.
	if err := s.RemoveStaging("b5046"); err != nil {
		t.Fatalf("RemoveStaging idempotent call: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/version/...`
Expected: FAIL with `undefined: New`, `undefined: Store`, `undefined: ErrNoActiveVersion`, `undefined: ErrNotInstalled`.

- [ ] **Step 3: Implement `internal/version/store.go`**

```go
// Package version owns the on-disk layout under <home>/.llamavm/.
package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrNoActiveVersion means ~/.llamavm/current is missing or empty.
	ErrNoActiveVersion = errors.New("no active version")
	// ErrNotInstalled means the requested version directory does not exist.
	ErrNotInstalled = errors.New("version not installed")
)

// Store is rooted at <home>/.llamavm. <home> is normally os.UserHomeDir().
type Store struct {
	home string
}

// New returns a Store rooted under home (which should be the user's home directory).
func New(home string) *Store {
	return &Store{home: home}
}

func (s *Store) Root() string         { return filepath.Join(s.home, ".llamavm") }
func (s *Store) VersionsDir() string  { return filepath.Join(s.Root(), "versions") }
func (s *Store) LogsDir() string      { return filepath.Join(s.Root(), "logs") }
func (s *Store) CurrentFile() string  { return filepath.Join(s.Root(), "current") }

// VersionDir is the final install directory for a tag.
func (s *Store) VersionDir(tag string) string {
	return filepath.Join(s.VersionsDir(), tag)
}

// StagingDir is the work-in-progress directory for an install. It is renamed
// onto VersionDir on success and removed on failure. The leading dot keeps it
// out of List() so a partial install never appears.
func (s *Store) StagingDir(tag string) string {
	return filepath.Join(s.VersionsDir(), ".staging-"+tag)
}

// IsInstalled reports whether the final version directory exists.
func (s *Store) IsInstalled(tag string) bool {
	info, err := os.Stat(s.VersionDir(tag))
	return err == nil && info.IsDir()
}

// List returns installed tags sorted ascending. Hidden entries (leading '.')
// are skipped so staging directories never surface.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.VersionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read versions dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// Active returns the tag in ~/.llamavm/current. Trailing whitespace is trimmed.
// Returns ErrNoActiveVersion when the file is missing or empty.
func (s *Store) Active() (string, error) {
	b, err := os.ReadFile(s.CurrentFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoActiveVersion
		}
		return "", fmt.Errorf("read current: %w", err)
	}
	tag := strings.TrimSpace(string(b))
	if tag == "" {
		return "", ErrNoActiveVersion
	}
	return tag, nil
}

// SetActive writes tag to ~/.llamavm/current atomically (temp + rename).
func (s *Store) SetActive(tag string) error {
	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		return fmt.Errorf("ensure root: %w", err)
	}
	tmp, err := os.CreateTemp(s.Root(), ".current-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(tag + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.CurrentFile()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename current: %w", err)
	}
	return nil
}

// ClearActive removes ~/.llamavm/current. Missing file is not an error.
func (s *Store) ClearActive() error {
	err := os.Remove(s.CurrentFile())
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("remove current: %w", err)
}

// Remove deletes the final version directory. Returns ErrNotInstalled when absent.
func (s *Store) Remove(tag string) error {
	if !s.IsInstalled(tag) {
		return ErrNotInstalled
	}
	if err := os.RemoveAll(s.VersionDir(tag)); err != nil {
		return fmt.Errorf("remove version dir: %w", err)
	}
	return nil
}

// PromoteStaging renames the staging directory onto the final version directory.
func (s *Store) PromoteStaging(tag string) error {
	if err := os.MkdirAll(s.VersionsDir(), 0o755); err != nil {
		return fmt.Errorf("ensure versions dir: %w", err)
	}
	if err := os.Rename(s.StagingDir(tag), s.VersionDir(tag)); err != nil {
		return fmt.Errorf("promote staging: %w", err)
	}
	return nil
}

// RemoveStaging deletes the staging directory. Missing dir is not an error.
func (s *Store) RemoveStaging(tag string) error {
	err := os.RemoveAll(s.StagingDir(tag))
	if err == nil {
		return nil
	}
	return fmt.Errorf("remove staging: %w", err)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/... -v`
Expected: PASS for all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/version/
git commit -m "feat(version): add filesystem store for installed versions and active marker"
```

---

## Task 2: GitHub Releases Client

Tiny REST client that resolves `latest` and verifies a tag exists. Honors `GITHUB_TOKEN` if set (PRD §5.6) — important to avoid the 60-req/hr unauthenticated rate limit during development.

**Files:**
- Create: `internal/github/releases.go`
- Test: `internal/github/releases_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/github/releases_test.go`:

```go
package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Latest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ggml-org/llama.cpp/releases/latest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"b5489","name":"b5489"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Latest = %q, want b5489", got)
	}
}

func TestClient_Latest_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestClient_TagExists_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ggml-org/llama.cpp/releases/tags/b5046" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"b5046"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if err := c.TagExists(context.Background(), "b5046"); err != nil {
		t.Fatalf("TagExists: %v", err)
	}
}

func TestClient_TagExists_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	err := c.TagExists(context.Background(), "bogus")
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("TagExists = %v, want ErrTagNotFound", err)
	}
}

func TestClient_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	_, err := c.Latest(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Latest = %v, want ErrRateLimited", err)
	}
}

func TestClient_TokenHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer testtoken" {
			t.Errorf("Authorization = %q, want Bearer testtoken", got)
		}
		w.Write([]byte(`{"tag_name":"b1"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	c.Token = "testtoken"
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github/...`
Expected: FAIL with `undefined: New`, `undefined: Client`, `undefined: ErrTagNotFound`, `undefined: ErrRateLimited`.

- [ ] **Step 3: Implement `internal/github/releases.go`**

```go
// Package github is a tiny REST client for the llama.cpp releases endpoint.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Sentinel errors.
var (
	ErrTagNotFound = errors.New("tag not found")
	ErrRateLimited = errors.New("github rate limited")
)

// Client queries the public llama.cpp release endpoints.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string // optional; falls back to GITHUB_TOKEN env at New()
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Token:   os.Getenv("GITHUB_TOKEN"),
	}
}

type release struct {
	TagName string `json:"tag_name"`
}

// Latest resolves the most recent release tag (e.g. "b5489").
func (c *Client) Latest(ctx context.Context) (string, error) {
	url := c.BaseURL + "/repos/ggml-org/llama.cpp/releases/latest"
	body, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	var r release
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode latest: %w", err)
	}
	if r.TagName == "" {
		return "", fmt.Errorf("github: empty tag_name in response")
	}
	return r.TagName, nil
}

// TagExists returns nil if the tag exists, ErrTagNotFound on 404,
// ErrRateLimited when GitHub reports the request as rate-limited, or another
// error for other failures.
func (c *Client) TagExists(ctx context.Context, tag string) error {
	url := fmt.Sprintf("%s/repos/ggml-org/llama.cpp/releases/tags/%s", c.BaseURL, tag)
	_, err := c.get(ctx, url)
	return err
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, ErrTagNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, ErrRateLimited
		}
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, body)
	default:
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, body)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/github/... -v`
Expected: PASS for all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat(github): add releases client with token + rate-limit handling"
```

---

## Task 3: Builder — Platform Detection

Trivial helpers that the install flow uses to refuse non-Apple-Silicon hosts and to pass `-j N` to cmake.

**Files:**
- Create: `internal/builder/platform.go`
- Test: `internal/builder/platform_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/builder/platform_test.go`:

```go
package builder

import (
	"runtime"
	"testing"
)

func TestPlatform_IsAppleSilicon(t *testing.T) {
	want := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	if got := DefaultPlatform.IsAppleSilicon(); got != want {
		t.Fatalf("IsAppleSilicon = %v, want %v (GOOS=%s, GOARCH=%s)", got, want, runtime.GOOS, runtime.GOARCH)
	}
}

func TestPlatform_Cores(t *testing.T) {
	got := DefaultPlatform.Cores()
	if got < 1 {
		t.Fatalf("Cores = %d, want >= 1", got)
	}
	if got != runtime.NumCPU() {
		t.Fatalf("Cores = %d, want runtime.NumCPU() = %d", got, runtime.NumCPU())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/...`
Expected: FAIL with `undefined: DefaultPlatform`.

- [ ] **Step 3: Implement `internal/builder/platform.go`**

```go
package builder

import "runtime"

// Platform exposes host-environment facts the build flow cares about.
type Platform interface {
	IsAppleSilicon() bool
	Cores() int
}

type defaultPlatform struct{}

func (defaultPlatform) IsAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func (defaultPlatform) Cores() int { return runtime.NumCPU() }

// DefaultPlatform is the production Platform implementation.
var DefaultPlatform Platform = defaultPlatform{}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -run TestPlatform`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/builder/platform.go internal/builder/platform_test.go
git commit -m "feat(builder): detect Apple Silicon host and CPU count"
```

---

## Task 4: Builder — cmake configure + build

Runs `cmake -B build -DGGML_METAL=ON -DCMAKE_BUILD_TYPE=Release` then `cmake --build build --config Release -j N`. Real `os/exec` is hidden behind a `CommandRunner` interface so unit tests verify command shape without shelling out. Stdout and stderr are tee'd to `logWriter` so the install flow can preserve them on failure.

**Files:**
- Create: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/builder/builder_test.go` (new file):

```go
package builder

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type recordedCmd struct {
	Name string
	Args []string
	Dir  string
}

type fakeRunner struct {
	calls []recordedCmd
	// fail at this 0-based call index. -1 means never.
	failAt    int
	failErr   error
	stderrOut string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	idx := len(f.calls)
	f.calls = append(f.calls, recordedCmd{Name: name, Args: append([]string(nil), args...), Dir: dir})
	if f.stderrOut != "" {
		stderr.Write([]byte(f.stderrOut))
	}
	if f.failAt >= 0 && idx == f.failAt {
		return f.failErr
	}
	return nil
}

func TestBuilder_Build_RunsConfigureThenBuild(t *testing.T) {
	r := &fakeRunner{failAt: -1}
	b := &Builder{Runner: r, Platform: DefaultPlatform}

	var log bytes.Buffer
	if err := b.Build(context.Background(), "/tmp/src", &log); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(r.calls), r.calls)
	}

	cfg := r.calls[0]
	if cfg.Name != "cmake" {
		t.Errorf("configure name = %q, want cmake", cfg.Name)
	}
	if cfg.Dir != "/tmp/src" {
		t.Errorf("configure dir = %q, want /tmp/src", cfg.Dir)
	}
	wantConfigure := []string{"-B", "build", "-DGGML_METAL=ON", "-DCMAKE_BUILD_TYPE=Release"}
	if !equalArgs(cfg.Args, wantConfigure) {
		t.Errorf("configure args = %v, want %v", cfg.Args, wantConfigure)
	}

	bld := r.calls[1]
	if bld.Name != "cmake" {
		t.Errorf("build name = %q, want cmake", bld.Name)
	}
	if bld.Dir != "/tmp/src" {
		t.Errorf("build dir = %q, want /tmp/src", bld.Dir)
	}
	// Build args: --build build --config Release -j <cores>
	if len(bld.Args) != 6 || bld.Args[0] != "--build" || bld.Args[1] != "build" ||
		bld.Args[2] != "--config" || bld.Args[3] != "Release" || bld.Args[4] != "-j" {
		t.Errorf("build args = %v, want [--build build --config Release -j N]", bld.Args)
	}
}

func TestBuilder_Build_ConfigureFailureCarriesStderr(t *testing.T) {
	r := &fakeRunner{failAt: 0, failErr: errors.New("cmake exited 1"), stderrOut: "metal not found\n"}
	b := &Builder{Runner: r, Platform: DefaultPlatform}

	var log bytes.Buffer
	err := b.Build(context.Background(), "/tmp/src", &log)
	if err == nil {
		t.Fatal("expected error from configure failure")
	}
	if !strings.Contains(err.Error(), "configure") {
		t.Errorf("err = %v, want it to mention configure phase", err)
	}
	if !strings.Contains(log.String(), "metal not found") {
		t.Errorf("log = %q, want it to contain stderr from cmake", log.String())
	}
	if len(r.calls) != 1 {
		t.Errorf("expected 1 call (configure failed; build skipped), got %d", len(r.calls))
	}
}

func TestBuilder_Build_BuildFailureCarriesStderr(t *testing.T) {
	r := &fakeRunner{failAt: 1, failErr: errors.New("ld: error"), stderrOut: "linker barf\n"}
	b := &Builder{Runner: r, Platform: DefaultPlatform}

	var log bytes.Buffer
	err := b.Build(context.Background(), "/tmp/src", &log)
	if err == nil {
		t.Fatal("expected error from build failure")
	}
	if !strings.Contains(err.Error(), "build") {
		t.Errorf("err = %v, want it to mention build phase", err)
	}
	if !strings.Contains(log.String(), "linker barf") {
		t.Errorf("log = %q, want it to contain stderr", log.String())
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/...`
Expected: FAIL with `undefined: Builder`, `undefined: CommandRunner` (Runner field type).

- [ ] **Step 3: Implement `internal/builder/builder.go`**

```go
package builder

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
)

// CommandRunner abstracts os/exec for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run invokes name with args in dir, tee-ing stdout/stderr to the supplied writers.
func (ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Builder runs cmake configure + build inside a llama.cpp source tree.
type Builder struct {
	Runner   CommandRunner
	Platform Platform
}

// Build runs cmake configure and then cmake --build inside srcDir. Both
// cmake invocations write combined stdout+stderr to logWriter so callers can
// preserve build output on failure.
func (b *Builder) Build(ctx context.Context, srcDir string, logWriter io.Writer) error {
	configure := []string{"-B", "build", "-DGGML_METAL=ON", "-DCMAKE_BUILD_TYPE=Release"}
	if err := b.Runner.Run(ctx, "cmake", configure, srcDir, logWriter, logWriter); err != nil {
		return fmt.Errorf("cmake configure: %w", err)
	}
	build := []string{"--build", "build", "--config", "Release", "-j", strconv.Itoa(b.Platform.Cores())}
	if err := b.Runner.Run(ctx, "cmake", build, srcDir, logWriter, logWriter); err != nil {
		return fmt.Errorf("cmake build: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v`
Expected: PASS for all five tests (two platform + three builder).

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): run cmake configure + build via injected CommandRunner"
```

---

## Task 5: CLI — Dependency Wiring + Root Command

Defines the consumer-side interfaces so the cli package never imports its concrete dependencies. Provides a `Deps` struct and a `NewRoot(deps)` constructor that the binary will use.

**Files:**
- Create: `internal/cli/deps.go`
- Create: `internal/cli/root.go`

(No tests in this task — the testable behavior arrives with each subcommand.)

- [ ] **Step 1: Implement `internal/cli/deps.go`**

```go
// Package cli builds the cobra command tree and orchestrates llamavm flows.
// The package never imports its concrete dependencies — instead it consumes
// the narrow interfaces below, which the binary wires up at main.go.
package cli

import (
	"context"
	"io"
	"time"
)

// Store is the version store contract used by the cli package.
type Store interface {
	IsInstalled(tag string) bool
	List() ([]string, error)
	Active() (string, error)
	SetActive(tag string) error
	ClearActive() error
	Remove(tag string) error
	VersionDir(tag string) string
	StagingDir(tag string) string
	PromoteStaging(tag string) error
	RemoveStaging(tag string) error
	LogsDir() string
}

// GitHubClient resolves and validates llama.cpp release tags.
type GitHubClient interface {
	Latest(ctx context.Context) (string, error)
	TagExists(ctx context.Context, tag string) error
}

// Builder runs the cmake configure + build sequence in a source tree.
type Builder interface {
	Build(ctx context.Context, srcDir string, logWriter io.Writer) error
}

// CommandRunner runs an external command. Used for `git clone` in install.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Platform answers host-environment questions the install flow cares about.
type Platform interface {
	IsAppleSilicon() bool
}

// Deps collects everything the cli subcommands need.
type Deps struct {
	Store    Store
	GitHub   GitHubClient
	Builder  Builder
	Git      CommandRunner
	Platform Platform
	Stdout   io.Writer
	Stderr   io.Writer
	Now      func() time.Time
}
```

- [ ] **Step 2: Implement `internal/cli/root.go`**

```go
package cli

import (
	"github.com/spf13/cobra"
)

// NewRoot returns the root cobra command with all M1 subcommands wired up.
// Pass the llamavm version string (e.g. "v1.0.0") for `--version`.
func NewRoot(deps *Deps, llamavmVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamavm",
		Short:         "A version manager for llama.cpp on Apple Silicon",
		Version:       llamavmVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)

	root.AddCommand(newInstallCmd(deps))
	root.AddCommand(newUninstallCmd(deps))
	root.AddCommand(newListCmd(deps))
	return root
}
```

- [ ] **Step 3: Add stubs so the package compiles**

Create `internal/cli/install.go`:

```go
package cli

import "github.com/spf13/cobra"

func newInstallCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "install", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
```

Create `internal/cli/uninstall.go`:

```go
package cli

import "github.com/spf13/cobra"

func newUninstallCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "uninstall", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
```

Create `internal/cli/list.go`:

```go
package cli

import "github.com/spf13/cobra"

func newListCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "list", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
```

These stubs are replaced in Tasks 6–8.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): scaffold root command + dependency interfaces"
```

---

## Task 6: `list` Subcommand

Implements PRD §3.5: prints installed versions one per line, asterisk on the active one, friendly message when empty.

**Files:**
- Modify: `internal/cli/list.go`
- Test: `internal/cli/list_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/list_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

// fakeStore implements just enough of Store for list/uninstall tests.
type fakeStore struct {
	installed []string
	active    string
	hasActive bool

	removed       []string
	removeErr     error
	clearActive   bool
	listErr       error
	versionDirFn  func(string) string
	stagingDirFn  func(string) string
	logsDir       string
	promoteErr    error
	removeStagErr error
}

func (s *fakeStore) IsInstalled(tag string) bool {
	for _, t := range s.installed {
		if t == tag {
			return true
		}
	}
	return false
}
func (s *fakeStore) List() ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]string(nil), s.installed...), nil
}
func (s *fakeStore) Active() (string, error) {
	if !s.hasActive {
		return "", version.ErrNoActiveVersion
	}
	return s.active, nil
}
func (s *fakeStore) SetActive(tag string) error { s.active = tag; s.hasActive = true; return nil }
func (s *fakeStore) ClearActive() error         { s.hasActive = false; s.active = ""; s.clearActive = true; return nil }
func (s *fakeStore) Remove(tag string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	for i, t := range s.installed {
		if t == tag {
			s.installed = append(s.installed[:i], s.installed[i+1:]...)
			s.removed = append(s.removed, tag)
			return nil
		}
	}
	return version.ErrNotInstalled
}
func (s *fakeStore) VersionDir(tag string) string {
	if s.versionDirFn != nil {
		return s.versionDirFn(tag)
	}
	return "/fake/versions/" + tag
}
func (s *fakeStore) StagingDir(tag string) string {
	if s.stagingDirFn != nil {
		return s.stagingDirFn(tag)
	}
	return "/fake/versions/.staging-" + tag
}
func (s *fakeStore) PromoteStaging(tag string) error {
	if s.promoteErr != nil {
		return s.promoteErr
	}
	s.installed = append(s.installed, tag)
	return nil
}
func (s *fakeStore) RemoveStaging(string) error { return s.removeStagErr }
func (s *fakeStore) LogsDir() string {
	if s.logsDir != "" {
		return s.logsDir
	}
	return "/fake/logs"
}

func runRoot(t *testing.T, deps *Deps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &errBuf
	root := NewRoot(deps, "test")
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetContext(context.Background())
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestList_Empty(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	out, _, err := runRoot(t, deps, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "No versions installed") {
		t.Fatalf("output = %q, want it to mention 'No versions installed'", out)
	}
	if !strings.Contains(out, "llamavm install latest") {
		t.Fatalf("empty-list output should suggest install latest, got %q", out)
	}
}

func TestList_WithActiveMarker(t *testing.T) {
	deps := &Deps{Store: &fakeStore{
		installed: []string{"b5489", "b5046", "b5400"},
		active:    "b5046",
		hasActive: true,
	}}
	out, _, err := runRoot(t, deps, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Sorted ascending; active marked with "* ", others with "  ".
	want := "  b5046\n  b5400\n  b5489\n"
	// Active is b5046, so flip its prefix.
	want = strings.Replace(want, "  b5046", "* b5046", 1)
	if out != want {
		t.Fatalf("list output =\n%q\nwant\n%q", out, want)
	}
}

func TestList_NoActive(t *testing.T) {
	deps := &Deps{Store: &fakeStore{installed: []string{"b5046"}}}
	out, _, err := runRoot(t, deps, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out != "  b5046\n" {
		t.Fatalf("list output = %q, want '  b5046\\n'", out)
	}
}

func TestList_StorePropagatesError(t *testing.T) {
	deps := &Deps{Store: &fakeStore{listErr: errors.New("boom")}}
	if _, _, err := runRoot(t, deps, "list"); err == nil {
		t.Fatal("expected error to propagate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run TestList`
Expected: FAIL — current `list` stub does nothing, so `TestList_Empty` and `TestList_WithActiveMarker` fail on output assertions.

- [ ] **Step 3: Replace `internal/cli/list.go`**

```go
package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show installed versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(deps)
		},
	}
}

func runList(deps *Deps) error {
	tags, err := deps.Store.List()
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	if len(tags) == 0 {
		fmt.Fprintln(deps.Stdout, "No versions installed. Run 'llamavm install latest' to install one.")
		return nil
	}
	sort.Strings(tags)

	active, err := deps.Store.Active()
	if err != nil && !errors.Is(err, version.ErrNoActiveVersion) {
		return fmt.Errorf("read active: %w", err)
	}

	for _, t := range tags {
		marker := "  "
		if t == active {
			marker = "* "
		}
		fmt.Fprintf(deps.Stdout, "%s%s\n", marker, t)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestList -v`
Expected: PASS for all four `TestList_*`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "feat(cli): list installed versions with active marker"
```

---

## Task 7: `uninstall` Subcommand

Implements PRD §3.4: removes the version directory; if it was the active version, clears active; errors if not installed. (PRD also mentions removing benchmark cache — bench is M4, so no benchmark cache exists yet to clean.)

**Files:**
- Modify: `internal/cli/uninstall.go`
- Test: `internal/cli/uninstall_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/uninstall_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestUninstall_RequiresVersionArg(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	if _, _, err := runRoot(t, deps, "uninstall"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestUninstall_NotInstalled(t *testing.T) {
	store := &fakeStore{}
	deps := &Deps{Store: store}
	_, errBuf, err := runRoot(t, deps, "uninstall", "b5046")
	if err == nil {
		t.Fatal("expected error when version not installed")
	}
	if !strings.Contains(err.Error(), "not installed") && !strings.Contains(errBuf, "not installed") {
		t.Fatalf("err/stderr should mention not installed; err=%v stderr=%q", err, errBuf)
	}
}

func TestUninstall_RemovesAndClearsActive(t *testing.T) {
	store := &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
	}
	deps := &Deps{Store: store}
	out, _, err := runRoot(t, deps, "uninstall", "b5046")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(store.removed) != 1 || store.removed[0] != "b5046" {
		t.Fatalf("expected b5046 removed, removed=%v", store.removed)
	}
	if !store.clearActive {
		t.Fatal("expected active to be cleared")
	}
	if !strings.Contains(out, "Uninstalled b5046") {
		t.Fatalf("stdout = %q, want 'Uninstalled b5046'", out)
	}
}

func TestUninstall_KeepsActiveIfDifferent(t *testing.T) {
	store := &fakeStore{
		installed: []string{"b5046", "b5489"},
		active:    "b5489",
		hasActive: true,
	}
	deps := &Deps{Store: store}
	if _, _, err := runRoot(t, deps, "uninstall", "b5046"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if store.clearActive {
		t.Fatal("expected active untouched when removing a non-active version")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run TestUninstall`
Expected: FAIL — stub does nothing.

- [ ] **Step 3: Replace `internal/cli/uninstall.go`**

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newUninstallCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <version>",
		Short: "Remove an installed version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(deps, args[0])
		},
	}
}

func runUninstall(deps *Deps, tag string) error {
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed", tag)
	}
	if err := deps.Store.Remove(tag); err != nil {
		if errors.Is(err, version.ErrNotInstalled) {
			return fmt.Errorf("%s is not installed", tag)
		}
		return fmt.Errorf("remove %s: %w", tag, err)
	}

	active, err := deps.Store.Active()
	if err != nil && !errors.Is(err, version.ErrNoActiveVersion) {
		return fmt.Errorf("read active: %w", err)
	}
	if active == tag {
		if err := deps.Store.ClearActive(); err != nil {
			return fmt.Errorf("clear active: %w", err)
		}
	}

	fmt.Fprintf(deps.Stdout, "Uninstalled %s\n", tag)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestUninstall -v`
Expected: PASS for all four `TestUninstall_*`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/uninstall.go internal/cli/uninstall_test.go
git commit -m "feat(cli): uninstall version and clear active when needed"
```

---

## Task 8: `install` Subcommand (Orchestration)

The big one. Implements PRD §3.3 minus shim creation (M2). Flow:

1. Resolve `latest` via GitHub if needed.
2. Validate tag exists via GitHub (404 → user error).
3. If already installed: print message, exit 0.
4. Refuse non-Apple-Silicon hosts.
5. Create staging dir at `versions/.staging-<tag>/source/`.
6. `git clone --depth 1 --branch <tag> https://github.com/ggml-org/llama.cpp.git <staging>/source`.
7. Run cmake configure + build inside `<staging>/source`, tee'ing output to a buffer.
8. Create `<staging>/bin/` and symlink `source/build/bin/{llama-cli,llama-server,llama-quantize}` into it.
9. PromoteStaging — atomic rename onto final dir.
10. If no active version, SetActive(tag).
11. Print success.

On any failure between step 5 and step 9: drain the cmake/git output to `<logsDir>/<tag>-<timestamp>.log`, RemoveStaging, return an error pointing at the log path.

**Files:**
- Modify: `internal/cli/install.go`
- Test: `internal/cli/install_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/install_test.go`:

```go
package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/gregmundy/llamavm/internal/github"
)

// fakeGitHub implements GitHubClient.
type fakeGitHub struct {
	latest      string
	latestErr   error
	tagErr      error
	calledTag   string
	latestCalls int
}

func (g *fakeGitHub) Latest(ctx context.Context) (string, error) {
	g.latestCalls++
	return g.latest, g.latestErr
}
func (g *fakeGitHub) TagExists(ctx context.Context, tag string) error {
	g.calledTag = tag
	return g.tagErr
}

// fakeBuilder implements Builder.
type fakeBuilder struct {
	srcDir string
	err    error
	logOut string
}

func (b *fakeBuilder) Build(ctx context.Context, srcDir string, log io.Writer) error {
	b.srcDir = srcDir
	if b.logOut != "" {
		log.Write([]byte(b.logOut))
	}
	return b.err
}

// fakeRunner implements CommandRunner. Used for git clone.
type fakeCmdRunner struct {
	calls   []recordedCall
	cloneFn func(args []string, dir string) error
}

type recordedCall struct {
	Name string
	Args []string
	Dir  string
}

func (r *fakeCmdRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	r.calls = append(r.calls, recordedCall{Name: name, Args: append([]string(nil), args...), Dir: dir})
	if r.cloneFn != nil {
		return r.cloneFn(args, dir)
	}
	return nil
}

// fakePlatform implements Platform.
type fakePlatform struct{ apple bool }

func (p fakePlatform) IsAppleSilicon() bool { return p.apple }

// realPathStore is a Store that uses real temp dirs so install can create
// staging directories on disk.
type realPathStore struct {
	*fakeStore
	root string
}

func newRealPathStore(t *testing.T, installed ...string) *realPathStore {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	rps := &realPathStore{
		fakeStore: &fakeStore{installed: append([]string(nil), installed...)},
		root:      root,
	}
	rps.fakeStore.versionDirFn = func(tag string) string {
		return filepath.Join(root, "versions", tag)
	}
	rps.fakeStore.stagingDirFn = func(tag string) string {
		return filepath.Join(root, "versions", ".staging-"+tag)
	}
	rps.fakeStore.logsDir = filepath.Join(root, "logs")
	return rps
}

// PromoteStaging on realPathStore actually renames so install can verify outcome.
func (s *realPathStore) PromoteStaging(tag string) error {
	from := filepath.Join(s.root, "versions", ".staging-"+tag)
	to := filepath.Join(s.root, "versions", tag)
	if err := os.Rename(from, to); err != nil {
		return err
	}
	s.installed = append(s.installed, tag)
	return nil
}

func (s *realPathStore) RemoveStaging(tag string) error {
	return os.RemoveAll(filepath.Join(s.root, "versions", ".staging-"+tag))
}

func newInstallDeps(t *testing.T, store Store) (*Deps, *fakeGitHub, *fakeBuilder, *fakeCmdRunner) {
	t.Helper()
	g := &fakeGitHub{latest: "b5489"}
	b := &fakeBuilder{}
	r := &fakeCmdRunner{cloneFn: func(args []string, dir string) error {
		// Simulate git clone: create the destination directory with a build/bin/ tree.
		// Last arg of git clone is destination.
		dst := args[len(args)-1]
		bin := filepath.Join(dst, "build", "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return err
		}
		for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				return err
			}
		}
		return nil
	}}
	deps := &Deps{
		Store:    store,
		GitHub:   g,
		Builder:  b,
		Git:      r,
		Platform: fakePlatform{apple: true},
		Now:      func() time.Time { return time.Date(2026, 5, 2, 14, 30, 30, 0, time.UTC) },
	}
	return deps, g, b, r
}

func TestInstall_RequiresVersion(t *testing.T) {
	deps, _, _, _ := newInstallDeps(t, &fakeStore{})
	if _, _, err := runRoot(t, deps, "install"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestInstall_RefusesNonAppleSilicon(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _ := newInstallDeps(t, store)
	deps.Platform = fakePlatform{apple: false}
	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
		t.Fatalf("err = %v, want one mentioning Apple Silicon", err)
	}
}

func TestInstall_AlreadyInstalledIsIdempotent(t *testing.T) {
	store := newRealPathStore(t, "b5046")
	deps, g, b, r := newInstallDeps(t, store)
	out, _, err := runRoot(t, deps, "install", "b5046")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "already installed") {
		t.Fatalf("stdout = %q, want 'already installed'", out)
	}
	if g.calledTag != "" || len(r.calls) != 0 || b.srcDir != "" {
		t.Fatalf("expected no network/clone/build; gh=%q, calls=%d, build=%q",
			g.calledTag, len(r.calls), b.srcDir)
	}
}

func TestInstall_LatestResolvesViaGitHub(t *testing.T) {
	store := newRealPathStore(t)
	deps, g, _, r := newInstallDeps(t, store)
	g.latest = "b5489"
	if _, _, err := runRoot(t, deps, "install", "latest"); err != nil {
		t.Fatalf("install latest: %v", err)
	}
	if g.latestCalls != 1 {
		t.Fatalf("expected 1 Latest call, got %d", g.latestCalls)
	}
	// Final installed dir should be the resolved tag.
	if !store.IsInstalled("b5489") {
		t.Fatal("expected b5489 to be installed")
	}
	// Git clone should have been called with --branch b5489.
	if len(r.calls) == 0 || !contains(r.calls[0].Args, "b5489") {
		t.Fatalf("expected git clone to use resolved tag; calls=%+v", r.calls)
	}
}

func TestInstall_TagNotFound(t *testing.T) {
	store := newRealPathStore(t)
	deps, g, _, _ := newInstallDeps(t, store)
	g.tagErr = gh.ErrTagNotFound
	_, _, err := runRoot(t, deps, "install", "b9999")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want one mentioning 'not found'", err)
	}
}

func TestInstall_HappyPath(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, r := newInstallDeps(t, store)
	out, _, err := runRoot(t, deps, "install", "b5046")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !store.IsInstalled("b5046") {
		t.Fatal("expected b5046 installed after happy path")
	}
	// Active should be set on first install.
	active, _ := store.Active()
	if active != "b5046" {
		t.Fatalf("active = %q, want b5046", active)
	}
	// Clone target should be staging/source.
	if len(r.calls) == 0 {
		t.Fatal("expected git clone")
	}
	cloneDst := r.calls[0].Args[len(r.calls[0].Args)-1]
	if !strings.HasSuffix(cloneDst, filepath.Join("versions", "b5046", "source")) {
		t.Fatalf("clone dst = %q, want it to end at versions/b5046/source", cloneDst)
	}
	// Builder should have run inside source dir.
	if !strings.HasSuffix(b.srcDir, "source") {
		t.Fatalf("builder srcDir = %q", b.srcDir)
	}
	// Bin symlinks must exist in final dir.
	finalBin := filepath.Join(store.root, "versions", "b5046", "bin")
	for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
		fi, err := os.Lstat(filepath.Join(finalBin, name))
		if err != nil {
			t.Fatalf("expected symlink %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink (mode=%v)", name, fi.Mode())
		}
	}
	if !strings.Contains(out, "Installed b5046") {
		t.Fatalf("stdout = %q, want 'Installed b5046'", out)
	}
}

func TestInstall_KeepsActiveOnSecondInstall(t *testing.T) {
	store := newRealPathStore(t)
	if err := store.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	store.installed = append(store.installed, "b5046")
	deps, _, _, _ := newInstallDeps(t, store)
	if _, _, err := runRoot(t, deps, "install", "b5489"); err != nil {
		t.Fatalf("install: %v", err)
	}
	active, _ := store.Active()
	if active != "b5046" {
		t.Fatalf("active = %q, want b5046 (unchanged)", active)
	}
}

func TestInstall_BuildFailureIsAtomic(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, _ := newInstallDeps(t, store)
	b.err = errors.New("cmake exited 1")
	b.logOut = "metal not found\n"

	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error on build failure")
	}
	if store.IsInstalled("b5046") {
		t.Fatal("failed install should not appear as installed")
	}
	stagingPath := filepath.Join(store.root, "versions", ".staging-b5046")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging should be removed on failure, stat err = %v", err)
	}
	// Build log should be preserved under logs/.
	logPath := filepath.Join(store.root, "logs", "b5046-20260502T143030.log")
	body, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("expected build log at %s: %v", logPath, readErr)
	}
	if !strings.Contains(string(body), "metal not found") {
		t.Fatalf("log body = %q, want it to contain stderr", string(body))
	}
	if !strings.Contains(err.Error(), logPath) {
		t.Fatalf("err = %v, want it to mention log path %s", err, logPath)
	}
}

func TestInstall_GitFailureIsAtomic(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, r := newInstallDeps(t, store)
	r.cloneFn = func(args []string, dir string) error { return errors.New("clone refused") }

	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error on clone failure")
	}
	if store.IsInstalled("b5046") {
		t.Fatal("failed install should not appear as installed")
	}
	stagingPath := filepath.Join(store.root, "versions", ".staging-b5046")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging should be removed, stat err = %v", err)
	}
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run TestInstall`
Expected: FAIL — stub does nothing.

- [ ] **Step 3: Replace `internal/cli/install.go`**

```go
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/version"
)

const llamaCppRepoURL = "https://github.com/ggml-org/llama.cpp.git"

var binaryNames = []string{"llama-cli", "llama-server", "llama-quantize"}

func newInstallCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "install <version>",
		Short: "Build and install a llama.cpp version from source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.Context(), deps, args[0])
		},
	}
}

func runInstall(ctx context.Context, deps *Deps, requested string) error {
	tag := requested
	if tag == "latest" {
		resolved, err := deps.GitHub.Latest(ctx)
		if err != nil {
			return fmt.Errorf("resolve latest: %w", err)
		}
		tag = resolved
	}

	if deps.Store.IsInstalled(tag) {
		fmt.Fprintf(deps.Stdout, "%s is already installed\n", tag)
		return nil
	}

	if err := deps.GitHub.TagExists(ctx, tag); err != nil {
		if errors.Is(err, gh.ErrTagNotFound) {
			return fmt.Errorf("version %s not found upstream", tag)
		}
		return fmt.Errorf("validate %s: %w", tag, err)
	}

	if !deps.Platform.IsAppleSilicon() {
		return fmt.Errorf("llamavm v1 requires Apple Silicon (darwin/arm64)")
	}

	staging := deps.Store.StagingDir(tag)
	source := filepath.Join(staging, "source")
	// Ensure no leftover from a previous failure.
	if err := deps.Store.RemoveStaging(tag); err != nil {
		return fmt.Errorf("clean staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging: %w", err)
	}

	var buildLog bytes.Buffer
	start := time.Now()
	fmt.Fprintf(deps.Stdout, "Installing %s...\n", tag)

	cleanup := func(phase string, runErr error) error {
		logPath, logErr := writeFailureLog(deps, tag, &buildLog)
		_ = deps.Store.RemoveStaging(tag)
		if logErr != nil {
			return fmt.Errorf("%s failed: %w (also failed to write log: %v)", phase, runErr, logErr)
		}
		return fmt.Errorf("%s failed: %w (build log: %s)", phase, runErr, logPath)
	}

	// git clone --depth 1 --branch <tag> <repo> <source>
	cloneArgs := []string{"clone", "--depth", "1", "--branch", tag, llamaCppRepoURL, source}
	if err := deps.Git.Run(ctx, "git", cloneArgs, "", &buildLog, &buildLog); err != nil {
		return cleanup("git clone", err)
	}

	if err := deps.Builder.Build(ctx, source, &buildLog); err != nil {
		return cleanup("build", err)
	}

	if err := symlinkBinaries(staging, source); err != nil {
		return cleanup("link binaries", err)
	}

	if err := deps.Store.PromoteStaging(tag); err != nil {
		return cleanup("promote staging", err)
	}

	// Set active if no active version is set.
	if _, err := deps.Store.Active(); errors.Is(err, version.ErrNoActiveVersion) {
		if err := deps.Store.SetActive(tag); err != nil {
			return fmt.Errorf("set active: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read active: %w", err)
	}

	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(deps.Stdout, "Installed %s in %s\n", tag, elapsed)
	return nil
}

// symlinkBinaries creates staging/bin/<name> -> staging/source/build/bin/<name>
// for each binary in binaryNames.
func symlinkBinaries(staging, source string) error {
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	for _, name := range binaryNames {
		target := filepath.Join(source, "build", "bin", name)
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("missing built binary %s: %w", name, err)
		}
		link := filepath.Join(binDir, name)
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("symlink %s: %w", name, err)
		}
	}
	return nil
}

// writeFailureLog drains the in-memory build log into <logsDir>/<tag>-<ts>.log.
func writeFailureLog(deps *Deps, tag string, body io.Reader) (string, error) {
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	ts := now.Format("20060102T150405")
	logsDir := deps.Store.LogsDir()
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(logsDir, fmt.Sprintf("%s-%s.log", tag, ts))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, body); err != nil {
		return path, err
	}
	return path, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v`
Expected: PASS for all `TestInstall_*` (8 tests), `TestList_*` (4), `TestUninstall_*` (4).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/install.go internal/cli/install_test.go
git commit -m "feat(cli): atomic install with staging dir and preserved failure logs"
```

---

## Task 9: Wire `cmd/llamavm/main.go`

Replace the placeholder root command in `cmd/llamavm/main.go` with one built from real implementations of the cli interfaces.

**Files:**
- Modify: `cmd/llamavm/main.go`

- [ ] **Step 1: Replace `cmd/llamavm/main.go`**

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregmundy/llamavm/internal/builder"
	"github.com/gregmundy/llamavm/internal/cli"
	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/version"
)

var llamavmVersion = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamavm: cannot resolve home directory:", err)
		os.Exit(1)
	}

	platform := builder.DefaultPlatform
	store := version.New(home)
	runner := builder.ExecRunner{}

	deps := &cli.Deps{
		Store:    store,
		GitHub:   gh.New(),
		Builder:  &builder.Builder{Runner: runner, Platform: platform},
		Git:      runner,
		Platform: platform,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Now:      time.Now,
	}

	root := cli.NewRoot(deps, llamavmVersion)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		// Cobra already prints the error via SilenceUsage=false default; we still
		// translate to PRD §6.4 exit codes.
		var notInstalled interface {
			Is(error) bool
		}
		if errors.Is(err, version.ErrNotInstalled) || errors.As(err, &notInstalled) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify build, vet, and the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: clean build, clean vet, all tests pass (version: 8, github: 6, builder: 5, cli: 16 = 35).

- [ ] **Step 3: Smoke-test the help surface**

Run: `go run ./cmd/llamavm --help`
Expected: lists `install`, `uninstall`, `list` as available commands plus `help` and `completion`.

Run: `go run ./cmd/llamavm list`
Expected: `No versions installed. Run 'llamavm install latest' to install one.` (assuming clean `~/.llamavm/`)

- [ ] **Step 4: Commit**

```bash
git add cmd/llamavm/main.go
git commit -m "feat(cmd): wire real implementations into root command"
```

- [ ] **Step 5: Manual end-to-end verification**

This step is acceptance per PRD §9 items 3, 5, 9, 11. Cannot be automated in unit tests because it requires real cmake + git + 5–15 minute build.

Pick one llama.cpp tag known to build cleanly on current Xcode CLT — `b5046` per the PRD acceptance.

Run:

```bash
go install ./cmd/llamavm
~/go/bin/llamavm install b5046
```

Expected: prints "Installing b5046...", clones, builds for several minutes, prints "Installed b5046 in <time>".

Run: `~/go/bin/llamavm list`
Expected: `* b5046`

Run: `~/.llamavm/versions/b5046/bin/llama-cli --version`
Expected: `version: 5046 (...)` — proves the symlinks point at a working binary.

Run: `~/go/bin/llamavm uninstall b5046`
Expected: `Uninstalled b5046`. Then `~/go/bin/llamavm list` → "No versions installed."

To verify the failure-is-atomic behavior: run `~/go/bin/llamavm install b5046`, then in another terminal `kill -9` the cmake process during the build. Then `~/go/bin/llamavm list` should still show no b5046, and `~/.llamavm/logs/` should contain a build log.

Record the outcomes in a short note appended to this plan or in `docs/release-checks/` (per PRD §8.3) — that note will be reused at v1.0.0 cut.

---

## Self-Review

**Spec coverage** (against PRD sections referenced in the request):

- §3.3.1 install steps 1-11: covered. Steps 1, 2, 4-8, 10, 11 are in `runInstall`. Step 9 (shim install) is explicitly deferred to M2 per the request. Step 3 (create version dir + source/) is satisfied by the staging dir + git clone creating `<staging>/source`.
- §3.3.2 `install latest`: covered by the `tag == "latest"` branch.
- §3.3.3 failure handling: covered. We preserve the build log under `~/.llamavm/logs/` and remove the staging dir (a more sensible interpretation of the PRD's contradictory "write log to version dir AND remove version dir" — see plan rationale near `writeFailureLog`).
- §3.3.4 network failures: covered. Both GitHub API and git clone errors are surfaced and result in cleanup. The error wraps the underlying network error.
- §3.3.5 build prerequisites: NOT covered as a pre-flight check. PRD says "If `cmake` is missing, `install` exits with an error message instructing the user to install it." Status: deferred — `cmake` invocation will fail with a clear error from `os/exec`, and `doctor` (M5) is the canonical place for prerequisite checks. Non-blocking for M1 acceptance, which only requires that the command works on a properly-set-up Mac.

  **Resolution:** add this as a follow-up note in the M5 plan rather than pad M1. Documented here.

- §3.4 uninstall: covered.
- §3.5 list: covered.
- §4.2 reliability/atomicity: covered. Staging directory, atomic rename via `os.Rename`, `SetActive` uses temp file + rename. Tests `TestInstall_BuildFailureIsAtomic` and `TestInstall_GitFailureIsAtomic` enforce the invariant.
- §5.2 filesystem layout: covered. `Store` enforces the layout. The staging dir is hidden so `List` never reports a partial install.

**Placeholder scan:** None of TBD/TODO/implement-later/etc. appear in any task body. Code blocks complete in every step.

**Type consistency:**

- `Store` interface in `internal/cli/deps.go` and concrete `*version.Store` — checked: `IsInstalled`, `List`, `Active`, `SetActive`, `ClearActive`, `Remove`, `VersionDir`, `StagingDir`, `PromoteStaging`, `RemoveStaging`, `LogsDir` are all defined identically in both.
- `Builder` interface (cli) and `*builder.Builder` concrete type — both have `Build(ctx, srcDir, logWriter) error`. ✓
- `GitHubClient` interface and `*github.Client` — both have `Latest(ctx)` and `TagExists(ctx, tag)`. ✓
- `CommandRunner` is defined twice: once in `internal/builder` (used by `Builder`) and once in `internal/cli` (used for git). Both have the same signature. They are separate Go types but structurally identical so `builder.ExecRunner` satisfies both — confirmed in main.go where the same `runner` is used for both `Builder.Runner` and `Deps.Git`.
- Sentinel errors: `version.ErrNoActiveVersion`, `version.ErrNotInstalled`, `gh.ErrTagNotFound`, `gh.ErrRateLimited` — all referenced consistently with the `errors.Is` pattern.

No issues found.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-02-m1-install-uninstall-list.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Good fit here because tasks are loosely coupled (each package builds in isolation) and the test surface keeps subagents honest.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
