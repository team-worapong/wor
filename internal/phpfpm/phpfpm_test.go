package phpfpm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPoolNameShort(t *testing.T) {
	got := PoolName("example.com", "web")
	want := "wor_example.com_web"
	if got != want {
		t.Errorf("PoolName() = %q, want %q", got, want)
	}
}

func TestPoolNameTruncatesLongNames(t *testing.T) {
	domain := strings.Repeat("a", 40)
	got := PoolName(domain, "web")
	if len(got) > maxPoolNameLen {
		t.Errorf("PoolName() length = %d, want <= %d", len(got), maxPoolNameLen)
	}
	// Two different long names that share the same truncated prefix
	// must not collide once the hash suffix is appended.
	other := PoolName(strings.Repeat("a", 39)+"b", "web")
	if got == other {
		t.Errorf("PoolName collision: both truncated to %q", got)
	}
}

func TestPoolNameStableForSameInput(t *testing.T) {
	domain := strings.Repeat("b", 50)
	if PoolName(domain, "web") != PoolName(domain, "web") {
		t.Error("PoolName() is not deterministic for identical input")
	}
}

func TestPoolFileContent(t *testing.T) {
	p := Pool{
		Domain:  "example.com",
		Service: "web",
		Version: Version{SockDir: "/run/php"},
		User:    "wor_example.com_web",
		Group:   "wor_example.com_web",
	}
	content := poolFileContent(p)
	for _, want := range []string{
		"[wor_example.com_web]",
		"user = wor_example.com_web",
		"group = wor_example.com_web",
		"listen = /run/php/wor_example.com_web.sock",
		"listen.owner = wor_example.com_web",
		"pm.max_children = 5",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("poolFileContent() missing %q, got:\n%s", want, content)
		}
	}
}

func TestPoolFileContentCustomMaxChildren(t *testing.T) {
	p := Pool{Domain: "d", Service: "s", Version: Version{SockDir: "/run/php"}, MaxChildren: 20}
	if !strings.Contains(poolFileContent(p), "pm.max_children = 20") {
		t.Error("expected custom MaxChildren to be honored")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestScanVersionDirs(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "8.3", "fpm"))
	mustMkdir(t, filepath.Join(root, "8.4", "fpm"))
	mustMkdir(t, filepath.Join(root, "not-a-version", "fpm"))
	mustMkdir(t, filepath.Join(root, "8.2")) // no "fpm" subdir -- should be skipped

	got := scanVersionDirs(root, "fpm")
	want := map[string]bool{"8.3": true, "8.4": true}
	if len(got) != len(want) {
		t.Fatalf("scanVersionDirs() = %v, want keys of %v", got, want)
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected version dir %q", v)
		}
	}
}

func TestScanVersionDirsMissingRoot(t *testing.T) {
	if got := scanVersionDirs(filepath.Join(t.TempDir(), "does-not-exist"), "fpm"); got != nil {
		t.Errorf("scanVersionDirs() on missing root = %v, want nil", got)
	}
}

func TestSocketPathAndPoolFilePath(t *testing.T) {
	v := Version{SockDir: "/run/php", PoolDir: "/etc/php/8.3/fpm/pool.d"}
	if got, want := SocketPath(v, "example.com", "web"), "/run/php/wor_example.com_web.sock"; got != want {
		t.Errorf("SocketPath() = %q, want %q", got, want)
	}
	if got, want := PoolFilePath(v, "example.com", "web"), "/etc/php/8.3/fpm/pool.d/wor_example.com_web.conf"; got != want {
		t.Errorf("PoolFilePath() = %q, want %q", got, want)
	}
}

func TestDetectVersionsReturnsNilOnUnsupportedPlatform(t *testing.T) {
	// This only asserts DetectVersions doesn't panic and returns a slice
	// (possibly empty/nil) on whatever platform the test happens to run
	// on -- the real Linux/macOS detection paths are exercised via
	// scanVersionDirs above, since they don't depend on this machine
	// actually having PHP-FPM installed at all.
	_ = DetectVersions()
}
