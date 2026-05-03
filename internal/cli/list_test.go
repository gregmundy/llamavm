package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/bench"
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
	shimsDir      string
	benchmarksDir string
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
func (s *fakeStore) ClearActive() error {
	s.hasActive = false
	s.active = ""
	s.clearActive = true
	return nil
}
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
func (s *fakeStore) ShimsDir() string {
	if s.shimsDir != "" {
		return s.shimsDir
	}
	return "/fake/shims"
}
func (s *fakeStore) BenchmarksDir() string {
	if s.benchmarksDir != "" {
		return s.benchmarksDir
	}
	return "/fake/benchmarks"
}

// fakeResolver implements Resolver. lastCwd records what Resolve was called
// with so tests can assert that subcommands threaded cwd through correctly.
// source/sourcePath let tests script ResolveDetailed responses; they default
// to SourceCurrent + a sentinel path when unset.
type fakeResolver struct {
	tag        string
	err        error
	lastCwd    string
	source     version.Source
	sourcePath string
}

func (r *fakeResolver) Resolve(cwd string) (string, error) {
	r.lastCwd = cwd
	return r.tag, r.err
}

func (r *fakeResolver) ResolveDetailed(cwd string) (version.Resolution, error) {
	r.lastCwd = cwd
	if r.err != nil {
		return version.Resolution{}, r.err
	}
	path := r.sourcePath
	if path == "" {
		path = "/fake/.llamavm/current"
	}
	return version.Resolution{Tag: r.tag, Source: r.source, Path: path}, nil
}

// fakeShimInstaller implements ShimInstaller; records each call's
// shimsDir AND the names list it received.
type fakeShimInstaller struct {
	calls    []string
	gotNames [][]string
	err      error
}

func (i *fakeShimInstaller) EnsureInstalled(shimsDir string, names []string) error {
	i.calls = append(i.calls, shimsDir)
	i.gotNames = append(i.gotNames, append([]string(nil), names...))
	return i.err
}

// fakeBenchmarker implements Benchmarker. Records each call's args; returns
// the next result from results in order, or err if non-nil.
type fakeBenchmarker struct {
	results []bench.Result
	errs    []error
	calls   []struct {
		tag      string
		model    string
		useCache bool
	}
}

func (b *fakeBenchmarker) Run(_ context.Context, tag, model string, useCache bool) (bench.Result, error) {
	idx := len(b.calls)
	b.calls = append(b.calls, struct {
		tag      string
		model    string
		useCache bool
	}{tag, model, useCache})
	if idx < len(b.errs) && b.errs[idx] != nil {
		return bench.Result{}, b.errs[idx]
	}
	if idx < len(b.results) {
		return b.results[idx], nil
	}
	return bench.Result{Version: tag}, nil
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
