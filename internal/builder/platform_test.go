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
