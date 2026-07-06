package osutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")

	if err := WriteFileAtomic(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Errorf("content = %q, want %q", data, `{"a":1}`)
	}
}

func TestWriteFileAtomicOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteFileAtomic(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("first WriteFileAtomic: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("second WriteFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", data, "new")
	}
}

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteFileAtomic(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".wor-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Errorf("dir contents = %v, want exactly [config.json]", entries)
	}
}

func TestWriteFilePrivilegedUnprivilegedPathSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "file.txt")

	if err := WriteFilePrivileged(path, []byte("hello")); err != nil {
		t.Fatalf("WriteFilePrivileged: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", data, "hello")
	}
}
