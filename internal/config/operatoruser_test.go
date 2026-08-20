package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeOperatorUserFixtures lays down a WOR_HOME with a host.env and a
// separate personal config file, writing wor_user into whichever of the
// two the caller asks for (empty string = leave that file without the
// key), and points Load at both through the environment.
func writeOperatorUserFixtures(t *testing.T, hostValue, userValue string) {
	t.Helper()

	worHome := t.TempDir()
	configs := filepath.Join(worHome, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	hostEnv := "HOST_PROVIDER=nginx\n"
	if hostValue != "" {
		hostEnv += "WOR_USER=" + hostValue + "\n"
	}
	if err := os.WriteFile(filepath.Join(configs, "host.env"), []byte(hostEnv), 0o644); err != nil {
		t.Fatalf("writing host.env: %v", err)
	}

	userCfg := "wor_home = " + worHome + "\n"
	if userValue != "" {
		userCfg += "wor_user = " + userValue + "\n"
	}
	userPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(userPath, []byte(userCfg), 0o644); err != nil {
		t.Fatalf("writing user config: %v", err)
	}

	t.Setenv("WOR_CONFIG_FILE", userPath)
	t.Setenv("WOR_HOME", worHome)
	t.Setenv("WOR_USER", "")
}

// TestOperatorUserPrefersHostEnvOverPersonalConfig pins the one place
// wor's usual precedence is inverted. Every other setting lets a
// personal ~/.wor/config override host.env; this one must not, because
// each admin account has its own personal config and the operator
// account only means anything if all of them resolve it identically.
func TestOperatorUserPrefersHostEnvOverPersonalConfig(t *testing.T) {
	writeOperatorUserFixtures(t, "worsvc", "someone-elses-idea")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OperatorUser != "worsvc" {
		t.Errorf("OperatorUser = %q, want the host.env value %q", cfg.OperatorUser, "worsvc")
	}
}

// TestOperatorUserFallsBackToPersonalConfig covers the host that has not
// had the key written into host.env yet -- a personal config is still
// better than nothing, and this is what lets an operator try the setting
// out before committing it to the shared file.
func TestOperatorUserFallsBackToPersonalConfig(t *testing.T) {
	writeOperatorUserFixtures(t, "", "worsvc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OperatorUser != "worsvc" {
		t.Errorf("OperatorUser = %q, want the personal-config value %q", cfg.OperatorUser, "worsvc")
	}
}

// TestOperatorUserEnvWinsOverBothFiles keeps a migration rehearsable
// without editing any file on disk.
func TestOperatorUserEnvWinsOverBothFiles(t *testing.T) {
	writeOperatorUserFixtures(t, "from-host-env", "from-user-config")
	t.Setenv("WOR_USER", "from-environment")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OperatorUser != "from-environment" {
		t.Errorf("OperatorUser = %q, want the environment value", cfg.OperatorUser)
	}
}

// TestOperatorUserUnsetStaysEmpty is the safety property the whole
// change rests on: a host that has not configured an operator account
// must resolve to "", which is what makes every ownership decision fall
// back to wor's previous behaviour instead of moving files around after
// an upgrade nobody asked for.
func TestOperatorUserUnsetStaysEmpty(t *testing.T) {
	writeOperatorUserFixtures(t, "", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OperatorUser != "" {
		t.Errorf("OperatorUser = %q, want empty when nothing configures it", cfg.OperatorUser)
	}
}
