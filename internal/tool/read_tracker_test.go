package tool

import (
	"path/filepath"
	"testing"
)

func TestReadTrackerMarkAndQuery(t *testing.T) {
	tr := NewReadTracker()
	abs := filepath.Join(t.TempDir(), "a.go")
	if tr.WasRead(abs) {
		t.Fatal("WasRead before MarkRead = true, want false")
	}
	tr.MarkRead(abs)
	if !tr.WasRead(abs) {
		t.Fatal("WasRead after MarkRead = false, want true")
	}
}

func TestReadTrackerNormalizesRelativeAndAbs(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "pkg", "x.go")
	tr := NewReadTracker()
	tr.MarkRead(abs)

	// A messy but equivalent path should resolve equal after normalization.
	messy := filepath.Join(dir, "pkg", "..", "pkg", "x.go")
	if !tr.WasRead(messy) {
		t.Fatalf("WasRead(%q) = false, want true (should normalize to %q)", messy, abs)
	}
}

func TestReadTrackerNilSafe(t *testing.T) {
	var tr *ReadTracker
	tr.MarkRead("/whatever") // must not panic
	if !tr.WasRead("/whatever") {
		t.Fatal("nil tracker WasRead = false, want true (guard inert)")
	}
}
