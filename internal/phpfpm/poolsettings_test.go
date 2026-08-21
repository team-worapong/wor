package phpfpm

import (
	"strings"
	"testing"
)

func TestParsePoolSettingsAcceptsAllowedKeys(t *testing.T) {
	got, err := ParsePoolSettings([]byte(`
; tuning for a busy site
pm = static
pm.max_children = 40
pm.max_requests = 500
`))
	if err != nil {
		t.Fatalf("ParsePoolSettings() error = %v", err)
	}
	want := []PoolSetting{
		{Key: "pm", Value: "static"},
		{Key: "pm.max_children", Value: "40"},
		{Key: "pm.max_requests", Value: "500"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParsePoolSettings() returned %d settings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("setting %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePoolSettingsRejects(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantText string
	}{
		{"pool identity: user", "user = root", "run as another service's account"},
		{"pool identity: listen", "listen = /run/php/other.sock", "take over another service's socket"},
		{"pool identity: listen.owner", "listen.owner = nobody", "socket ownership"},
		{"privileged log path", "error_log = /etc/shadow", "as root"},
		{"php ini setting in the wrong file", "php_admin_value[memory_limit] = 512M", "not a valid setting name"},
		{"php ini key in the wrong file", "memory_limit = 512M", "not one of the pool settings"},
		{"unknown process manager", "pm = turbo", "static, dynamic or ondemand"},
		{"unknown key", "pm.max_kids = 40", "not one of the pool settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePoolSettings([]byte(tc.input))
			if err == nil {
				t.Fatalf("ParsePoolSettings(%q) succeeded, want an error", tc.input)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("ParsePoolSettings(%q) error = %q, want it to mention %q", tc.input, err, tc.wantText)
			}
		})
	}
}

// A php.ini key landing in php-fpm.ini is the mistake this split
// invites, so the error has to point at the other file rather than just
// saying no.
func TestParsePoolSettingsPointsAtTheOtherFile(t *testing.T) {
	_, err := ParsePoolSettings([]byte("php_value[memory_limit] = 512M"))
	if err == nil {
		t.Fatal("ParsePoolSettings() accepted a php_value directive")
	}
	// php_value[...] fails the key-shape check first (brackets), which
	// is fine -- but the bare-key form has to name php.ini.
	if _, err := ParsePoolSettings([]byte("php_value = 1")); err == nil {
		t.Fatal("ParsePoolSettings() accepted php_value")
	} else if !strings.Contains(err.Error(), SettingsFileName) {
		t.Errorf("error = %q, want it to name %s", err, SettingsFileName)
	}
}

func TestPoolSettingLinesDefaults(t *testing.T) {
	got := PoolSettingLines(nil)
	want := []string{
		"pm = dynamic",
		"pm.max_children = 5",
		"pm.start_servers = 2",
		"pm.min_spare_servers = 1",
		"pm.max_spare_servers = 3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("PoolSettingLines(nil) = %q, want %q", got, want)
	}
}

// Switching the process manager switches which directives the block
// carries: listing dynamic-only settings under `pm = static` is not an
// error to php-fpm, but it tells whoever reads the pool file next that
// settings are in force which are not.
func TestPoolSettingLinesStaticDropsDynamicOnlySettings(t *testing.T) {
	got := PoolSettingLines(mustParsePoolSettings(t, "pm = static\npm.max_children = 40"))
	want := []string{"pm = static", "pm.max_children = 40"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("PoolSettingLines() = %q, want %q", got, want)
	}
}

func TestPoolSettingLinesOndemandUsesIdleTimeout(t *testing.T) {
	got := strings.Join(PoolSettingLines(mustParsePoolSettings(t, "pm = ondemand")), "\n")
	if !strings.Contains(got, "pm.process_idle_timeout = 10s") {
		t.Errorf("PoolSettingLines() = %q, want it to carry the ondemand idle timeout", got)
	}
	if strings.Contains(got, "pm.start_servers") {
		t.Errorf("PoolSettingLines() = %q, want no dynamic-only settings under ondemand", got)
	}
}

// Overrides replace the default in place rather than being appended, so
// a directive never appears twice with two different values.
func TestPoolSettingLinesOverrideReplacesDefault(t *testing.T) {
	got := PoolSettingLines(mustParsePoolSettings(t, "pm.max_children = 40"))
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "pm.max_children = 40") {
		t.Errorf("PoolSettingLines() = %q, want the override", joined)
	}
	if strings.Count(joined, "pm.max_children") != 1 {
		t.Errorf("PoolSettingLines() = %q, want pm.max_children exactly once", joined)
	}
}

// Directives outside the process-manager block follow it in file order.
func TestPoolSettingLinesExtraDirectivesFollowInFileOrder(t *testing.T) {
	got := PoolSettingLines(mustParsePoolSettings(t, "request_terminate_timeout = 60s\npm.max_requests = 500"))
	joined := strings.Join(got, "\n")
	if !strings.HasSuffix(joined, "request_terminate_timeout = 60s\npm.max_requests = 500") {
		t.Errorf("PoolSettingLines() = %q, want the extra directives last, in file order", joined)
	}
}

func TestPoolSettingsRenderIntoPoolFile(t *testing.T) {
	content := PoolFileContent(Pool{
		Domain: "example.com", Service: "web",
		Version:      Version{SockDir: "/run/php"},
		User:         "wor_example.com_web",
		Group:        "wor_example.com_web",
		PoolSettings: mustParsePoolSettings(t, "pm.max_children = 40"),
	})
	if !strings.Contains(content, "pm.max_children = 40") {
		t.Errorf("PoolFileContent() missing the override, got:\n%s", content)
	}
	// Nothing a service supplies may precede the directives that decide
	// who the pool runs as.
	if strings.Index(content, "pm = dynamic") < strings.Index(content, "listen.mode") {
		t.Errorf("the process manager block was rendered before the pool's identity:\n%s", content)
	}
}
