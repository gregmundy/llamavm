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
