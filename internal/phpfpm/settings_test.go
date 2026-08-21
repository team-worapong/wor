package phpfpm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSettingsAcceptsAllowedKeys(t *testing.T) {
	got, err := ParseSettings([]byte(`
; a comment
# another comment

memory_limit = 512M
upload_max_filesize = "64M"
max_file_uploads = 40
`))
	if err != nil {
		t.Fatalf("ParseSettings() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ParseSettings() returned %d settings, want 3: %+v", len(got), got)
	}
	// File order is preserved, quotes are stripped, and the directive
	// each key needs comes from the allowlist rather than the file.
	want := []Setting{
		{Key: "memory_limit", Value: "512M", admin: false},
		{Key: "upload_max_filesize", Value: "64M", admin: false},
		{Key: "max_file_uploads", Value: "40", admin: true},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("setting %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestParseSettingsRejects(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantText string
	}{
		{"unknown key", "memroy_limit = 512M", "not one of the settings"},
		{"rejected key names the reason", "error_log = /etc/shadow", "as root"},
		{"rejected prefix", "opcache.memory_consumption = 256", "allocated once by the php-fpm master"},
		{"section header", "[www]\nmemory_limit = 512M", "section headers are not supported"},
		{"no equals sign", "memory_limit 512M", "expected `key = value`"},
		{"duplicate key", "memory_limit = 512M\nmemory_limit = 1G", "already set on line 1"},
		{"empty value", "memory_limit =", "has no value"},
		{"key with brackets", "php_admin_value[memory_limit] = 512M", "not a valid setting name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSettings([]byte(tc.input))
			if err == nil {
				t.Fatalf("ParseSettings(%q) succeeded, want an error", tc.input)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("ParseSettings(%q) error = %q, want it to mention %q", tc.input, err, tc.wantText)
			}
		})
	}
}

// A value carrying a newline is the injection this parser exists to
// stop: pasted into the pool file it would end the php_value directive
// and let the rest of the line become a pool directive of the author's
// choosing. Line-based parsing splits it first, so what remains has to
// fail as its own malformed line rather than as a value.
func TestParseSettingsCannotInjectPoolDirectives(t *testing.T) {
	_, err := ParseSettings([]byte("memory_limit = 512M\nuser = root\n"))
	if err == nil {
		t.Fatal("ParseSettings() accepted an injected `user = root` line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want it to name line 2", err)
	}
}

func TestParseSettingsRejectsControlCharacters(t *testing.T) {
	if _, err := ParseSettings([]byte("memory_limit = 512M\x00user = root")); err == nil {
		t.Fatal("ParseSettings() accepted a value containing a NUL byte")
	}
}

func TestLoadSettingsMissingFileIsNotAnError(t *testing.T) {
	settings, err := LoadSettings(filepath.Join(t.TempDir(), "php.ini"))
	if err != nil {
		t.Fatalf("LoadSettings() error = %v, want nil for a missing file", err)
	}
	if settings != nil {
		t.Errorf("LoadSettings() = %+v, want no settings", settings)
	}
}

func TestLoadSettingsNamesTheFileInErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "php.ini")
	if err := os.WriteFile(path, []byte("nonsense = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSettings(path)
	if err == nil {
		t.Fatal("LoadSettings() succeeded on an invalid file")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name %s", err, path)
	}
}

func TestPoolFileContentRendersSettings(t *testing.T) {
	settings, err := ParseSettings([]byte("memory_limit = 512M\nmax_file_uploads = 40"))
	if err != nil {
		t.Fatal(err)
	}
	content := PoolFileContent(Pool{
		Domain:   "example.com",
		Service:  "web",
		Version:  Version{SockDir: "/run/php"},
		User:     "wor_example.com_web",
		Group:    "wor_example.com_web",
		Settings: settings,
	})
	for _, want := range []string{
		"php_value[memory_limit] = 512M",
		"php_admin_value[max_file_uploads] = 40",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("PoolFileContent() missing %q, got:\n%s", want, content)
		}
	}
	// The settings block must come after the pool's identity
	// directives, never before them.
	if strings.Index(content, "php_value[memory_limit]") < strings.Index(content, "listen.mode") {
		t.Errorf("settings were rendered before the pool's own directives:\n%s", content)
	}
}

// A service with no php.ini must get byte-for-byte the pool file it got
// before per-service settings existed.
func TestPoolFileContentUnchangedWithoutSettings(t *testing.T) {
	p := Pool{
		Domain:  "example.com",
		Service: "web",
		Version: Version{SockDir: "/run/php"},
		User:    "wor_example.com_web",
		Group:   "wor_example.com_web",
	}
	content := PoolFileContent(p)
	if strings.Contains(content, "php_value") || strings.Contains(content, "php_admin_value") {
		t.Errorf("PoolFileContent() emitted php settings for a pool with none:\n%s", content)
	}
	if !strings.HasSuffix(content, "pm.max_spare_servers = 3\n") {
		t.Errorf("PoolFileContent() should end at the last pm directive, got:\n%s", content)
	}
}

func TestPoolUpToDate(t *testing.T) {
	settings, err := ParseSettings([]byte("memory_limit = 512M"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := Pool{
		Domain: "example.com", Service: "web",
		Version:  Version{SockDir: "/run/php", PoolDir: dir},
		User:     "wor_example.com_web",
		Group:    "wor_example.com_web",
		Settings: settings,
	}
	path := PoolFilePath(p.Version, p.Domain, p.Service)
	if err := os.WriteFile(path, []byte(PoolFileContent(p)), 0o644); err != nil {
		t.Fatal(err)
	}

	upToDate, readable := PoolUpToDate(p)
	if !readable || !upToDate {
		t.Errorf("PoolUpToDate() = (%t, %t) for a pool file wor just wrote, want (true, true)", upToDate, readable)
	}

	// Any change to what the pool would render to counts, including one
	// that never touches a php_value line.
	drifted := p
	drifted.PoolSettings = mustParsePoolSettings(t, "pm.max_children = 40")
	if upToDate, readable := PoolUpToDate(drifted); !readable || upToDate {
		t.Errorf("PoolUpToDate() = (%t, %t) after changing the process manager, want (false, true)", upToDate, readable)
	}
}

// An unreadable or missing pool file is "wor cannot tell", which every
// caller has to distinguish from "the wrong thing is applied".
func TestPoolUpToDateUnreadableFile(t *testing.T) {
	p := Pool{Domain: "example.com", Service: "web", Version: Version{SockDir: "/run/php", PoolDir: t.TempDir()}}
	if upToDate, readable := PoolUpToDate(p); readable || upToDate {
		t.Errorf("PoolUpToDate() = (%t, %t) for a missing pool file, want (false, false)", upToDate, readable)
	}
}

func mustParsePoolSettings(t *testing.T, body string) []PoolSetting {
	t.Helper()
	settings, err := ParsePoolSettings([]byte(body))
	if err != nil {
		t.Fatalf("ParsePoolSettings(%q): %v", body, err)
	}
	return settings
}
