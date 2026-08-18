package hostprovider

import (
	"os"
	"path/filepath"
	"testing"

	"wor/internal/config"
)

// newTestProvider builds an nginx provider whose sites-available and
// sites-enabled point into t.TempDir(), so the snapshot/restore tests
// touch nothing outside the test.
func newTestProvider(t *testing.T, separateEnabled bool) (*Provider, string, string) {
	t.Helper()
	root := t.TempDir()
	available := filepath.Join(root, "sites-available")
	enabled := available
	if separateEnabled {
		enabled = filepath.Join(root, "sites-enabled")
	}
	for _, dir := range []string{available, enabled} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cfg := &config.Config{
		NginxSitesAvailable: available,
		NginxSitesEnabled:   enabled,
	}
	p, err := New("nginx", cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p, available, enabled
}

// A host that did not exist before the change must be removed entirely
// by a restore -- otherwise a rejected `host add` would leave a broken
// vhost behind that fails the next reload for every site.
func TestRestoreRemovesAHostThatDidNotExist(t *testing.T) {
	p, _, _ := newTestProvider(t, true)
	host := "new.example.test"

	snapshot := p.SnapshotHostConfig(host)

	available := p.SiteAvailableFile(host)
	if err := os.WriteFile(available, []byte("broken config"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.EnableHost(available, p.SiteEnabledFile(host)); err != nil {
		t.Fatalf("EnableHost: %v", err)
	}

	if err := p.RestoreHostConfig(snapshot); err != nil {
		t.Fatalf("RestoreHostConfig: %v", err)
	}
	if _, err := os.Lstat(available); !os.IsNotExist(err) {
		t.Errorf("sites-available entry still present after restore (err=%v)", err)
	}
	if _, err := os.Lstat(p.SiteEnabledFile(host)); !os.IsNotExist(err) {
		t.Errorf("sites-enabled entry still present after restore (err=%v)", err)
	}
}

// A host that already existed must get its exact previous bytes back,
// so a rejected `ssl issue` leaves the working config in place.
func TestRestorePutsBackThePreviousContent(t *testing.T) {
	p, _, _ := newTestProvider(t, true)
	host := "existing.example.test"
	available := p.SiteAvailableFile(host)

	original := []byte("server { listen 80; }\n")
	if err := os.WriteFile(available, original, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.EnableHost(available, p.SiteEnabledFile(host)); err != nil {
		t.Fatalf("EnableHost: %v", err)
	}

	snapshot := p.SnapshotHostConfig(host)

	if err := os.WriteFile(available, []byte("server { ssl_certificate /unreadable; }\n"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	if err := p.RestoreHostConfig(snapshot); err != nil {
		t.Fatalf("RestoreHostConfig: %v", err)
	}
	got, err := os.ReadFile(available)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("content = %q, want %q", got, original)
	}
	// The host was enabled before the change, so it must stay enabled.
	if _, err := os.Stat(p.SiteEnabledFile(host)); err != nil {
		t.Errorf("host is no longer enabled after restore: %v", err)
	}
}

// macOS and Windows use one flat directory (DESIGN.md section 3), where
// the available and enabled paths are the same file. Restore must not
// delete the file it just wrote back by also treating it as a stray
// enabled entry.
func TestRestoreWithFlatDirectoryLayout(t *testing.T) {
	p, available, enabled := newTestProvider(t, false)
	if available != enabled {
		t.Fatalf("test setup: expected a flat layout, got %s and %s", available, enabled)
	}
	host := "flat.example.test"
	file := p.SiteAvailableFile(host)

	original := []byte("server { listen 80; }\n")
	if err := os.WriteFile(file, original, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	snapshot := p.SnapshotHostConfig(host)
	if err := os.WriteFile(file, []byte("broken\n"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if err := p.RestoreHostConfig(snapshot); err != nil {
		t.Fatalf("RestoreHostConfig: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("content = %q, want %q", got, original)
	}
}

// Some layouts enable a host by copying the file instead of symlinking
// it (a2ensite variants, platforms without symlinks). A rollback has to
// put that copy back too -- restoring only sites-available would leave
// the rejected config live while reporting success.
func TestRestoreRewritesACopiedEnabledEntry(t *testing.T) {
	p, _, _ := newTestProvider(t, true)
	host := "copied.example.test"
	available := p.SiteAvailableFile(host)
	enabled := p.SiteEnabledFile(host)

	original := []byte("server { listen 80; }\n")
	for _, f := range []string{available, enabled} {
		if err := os.WriteFile(f, original, 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	snapshot := p.SnapshotHostConfig(host)

	broken := []byte("server { ssl_certificate /gone; }\n")
	for _, f := range []string{available, enabled} {
		if err := os.WriteFile(f, broken, 0o644); err != nil {
			t.Fatalf("overwrite %s: %v", f, err)
		}
	}

	if err := p.RestoreHostConfig(snapshot); err != nil {
		t.Fatalf("RestoreHostConfig: %v", err)
	}
	for _, f := range []string{available, enabled} {
		got, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if string(got) != string(original) {
			t.Errorf("%s = %q, want %q", f, got, original)
		}
	}
}
