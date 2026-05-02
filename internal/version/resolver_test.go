package version

import (
	"errors"
	"testing"
)

func TestResolver_NoActiveVersion(t *testing.T) {
	r := NewResolver(New(t.TempDir()))
	if _, err := r.Resolve(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve on empty: got %v, want ErrNoActiveVersion", err)
	}
}

func TestResolver_ReadsCurrentFile(t *testing.T) {
	s := New(t.TempDir())
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s)
	got, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}
