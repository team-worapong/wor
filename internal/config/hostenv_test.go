package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetHostEnvKeyAppendsToAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	original := "HOST_PROVIDER=nginx\n# PHP_FPM_ENDPOINT=unix:/run/php/php8.4-fpm.sock\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding host.env: %v", err)
	}

	if err := SetHostEnvKey(path, "WOR_USER", "worsvc"); err != nil {
		t.Fatalf("SetHostEnvKey: %v", err)
	}

	got := readHostEnv(t, path)
	if !strings.Contains(got, "WOR_USER=worsvc") {
		t.Errorf("key not written; file is:\n%s", got)
	}
	// The commented override hints SaveHostEnv scaffolds are the file's
	// only documentation. Losing them on an unrelated write would be a
	// quiet regression nobody notices until they go looking for one.
	if !strings.Contains(got, "HOST_PROVIDER=nginx") ||
		!strings.Contains(got, "# PHP_FPM_ENDPOINT=") {
		t.Errorf("existing lines were not preserved; file is:\n%s", got)
	}
}

func TestSetHostEnvKeyReplacesRatherThanDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(path, []byte("HOST_PROVIDER=nginx\nWOR_USER=wor\n"), 0o644); err != nil {
		t.Fatalf("seeding host.env: %v", err)
	}

	if err := SetHostEnvKey(path, "WOR_USER", "worsvc"); err != nil {
		t.Fatalf("SetHostEnvKey: %v", err)
	}

	got := readHostEnv(t, path)
	if strings.Count(got, "WOR_USER=") != 1 {
		t.Errorf("expected exactly one WOR_USER line, file is:\n%s", got)
	}
	if !strings.Contains(got, "WOR_USER=worsvc") {
		t.Errorf("value not updated; file is:\n%s", got)
	}
}

// TestSetHostEnvKeyReplacesTheOtherSpelling matters because ParseKV
// accepts both `wor_user` and `WOR_USER`. Leaving the old spelling
// behind would put two contradictory lines in one file and let line
// order decide which one wins.
func TestSetHostEnvKeyReplacesTheOtherSpelling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(path, []byte("wor_user = wor\n"), 0o644); err != nil {
		t.Fatalf("seeding host.env: %v", err)
	}

	if err := SetHostEnvKey(path, "WOR_USER", "worsvc"); err != nil {
		t.Fatalf("SetHostEnvKey: %v", err)
	}

	got := readHostEnv(t, path)
	if strings.Contains(got, "wor_user") {
		t.Errorf("the lower-case spelling was left behind; file is:\n%s", got)
	}
	if strings.Count(got, "worsvc") != 1 {
		t.Errorf("expected exactly one operator account line, file is:\n%s", got)
	}
}

func TestSetHostEnvKeyCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")

	if err := SetHostEnvKey(path, "WOR_USER", "worsvc"); err != nil {
		t.Fatalf("SetHostEnvKey: %v", err)
	}

	if got := readHostEnv(t, path); !strings.Contains(got, "WOR_USER=worsvc") {
		t.Errorf("key not written to a new file; file is:\n%s", got)
	}
}

// TestSetHostEnvKeyRoundTripsThroughParseKV closes the loop: what was
// written has to be what Load later reads back.
func TestSetHostEnvKeyRoundTripsThroughParseKV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(path, []byte("HOST_PROVIDER=nginx\n"), 0o644); err != nil {
		t.Fatalf("seeding host.env: %v", err)
	}
	if err := SetHostEnvKey(path, "WOR_USER", "worsvc"); err != nil {
		t.Fatalf("SetHostEnvKey: %v", err)
	}

	kv, err := ParseKV(path)
	if err != nil {
		t.Fatalf("ParseKV: %v", err)
	}
	if v, ok := lookup(kv, "wor_user", "WOR_USER"); !ok || v != "worsvc" {
		t.Errorf("round trip gave %q (found=%v), want %q", v, ok, "worsvc")
	}
	if v, ok := lookup(kv, "HOST_PROVIDER"); !ok || v != "nginx" {
		t.Errorf("HOST_PROVIDER did not survive: %q (found=%v)", v, ok)
	}
}

func readHostEnv(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading host.env: %v", err)
	}
	return string(data)
}
