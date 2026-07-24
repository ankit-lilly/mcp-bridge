package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")

	if err := AtomicWrite(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(got))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("expected 0644 perms, got %o", info.Mode().Perm())
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")

	if err := AtomicWrite(path, []byte("first"), 0600); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := AtomicWrite(path, []byte("second"), 0600); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("expected %q, got %q", "second", string(got))
	}
}

func TestAtomicWrite_BadDir(t *testing.T) {
	err := AtomicWrite("/nonexistent/dir/file.txt", []byte("data"), 0600)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}
