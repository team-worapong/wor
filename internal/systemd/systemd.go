// Package systemd generates and manages systemd unit files for wor's
// process-supervised service templates (go, python) on Linux. It plays
// the same role here that internal/pm2 plays for node: given a
// domain/service pair, produce a process-manager-native definition and
// wrap the handful of CLI verbs (start/stop/restart/status/logs) needed
// to run it.
//
// Every privileged systemctl/journalctl invocation goes through
// osutil.SudoCommand, so it participates in the same confirm-once
// elevation gate as every other privileged operation in wor (see
// DESIGN.md section 4) -- callers do not need to handle sudo
// themselves.
package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wor/internal/osutil"
)

// Name returns the systemd unit name for a domain/service pair,
// mirroring PM2's naming convention (pm2.Name): "wor_<domain>_<service>".
func Name(domain, service string) string {
	return "wor_" + domain + "_" + service + ".service"
}

const unitDir = "/etc/systemd/system"

// UnitPath returns the absolute path where domain/service's unit file
// is installed.
func UnitPath(domain, service string) string {
	return filepath.Join(unitDir, Name(domain, service))
}

// Unit describes the fields wor needs to render a unit file. ExecStart
// must be an absolute or ./-relative command line; WorkingDirectory is
// set so relative paths (the entry point, a "public/" directory, etc.)
// resolve the same way PM2's `cwd` does.
type Unit struct {
	Domain      string
	Service     string
	Description string
	WorkingDir  string
	ExecStart   string
	Env         map[string]string
}

func unitFileContent(u Unit) string {
	keys := make([]string, 0, len(u.Env))
	for k := range u.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var env strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&env, "Environment=%s=%s\n", k, u.Env[k])
	}
	return fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
%sRestart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`, u.Description, u.WorkingDir, u.ExecStart, env.String())
}

func runSystemctl(args ...string) error {
	cmd, err := osutil.SudoCommand("systemctl", args...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func captureSystemctl(args ...string) (string, error) {
	cmd, err := osutil.SudoCommand("systemctl", args...)
	if err != nil {
		return "", err
	}
	out, runErr := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), runErr
}

// WriteUnit installs domain/service's unit file and reloads systemd so
// it picks up the change. Requires root (via sudo).
func WriteUnit(u Unit) error {
	content := unitFileContent(u)
	if err := osutil.WriteFilePrivileged(UnitPath(u.Domain, u.Service), []byte(content)); err != nil {
		return fmt.Errorf("cannot write systemd unit (%s): %w", osutil.ElevationHint(), err)
	}
	return runSystemctl("daemon-reload")
}

// Enable runs `systemctl enable` for domain/service, so it survives a
// server reboot the same way a PM2 process does after `pm2 save` +
// `pm2 startup`.
func Enable(domain, service string) error {
	return runSystemctl("enable", Name(domain, service))
}

// Disable runs `systemctl disable` for domain/service.
func Disable(domain, service string) error {
	return runSystemctl("disable", Name(domain, service))
}

// Start runs `systemctl start` for domain/service.
func Start(domain, service string) error {
	return runSystemctl("start", Name(domain, service))
}

// Stop runs `systemctl stop` for domain/service.
func Stop(domain, service string) error {
	return runSystemctl("stop", Name(domain, service))
}

// Restart runs `systemctl restart` for domain/service.
func Restart(domain, service string) error {
	return runSystemctl("restart", Name(domain, service))
}

// IsActive reports whether domain/service's unit is currently running.
func IsActive(domain, service string) bool {
	out, err := captureSystemctl("is-active", Name(domain, service))
	return err == nil && out == "active"
}

// Status returns the raw `systemctl status` output for domain/service,
// for `wor service status`-style reporting.
func Status(domain, service string) (string, error) {
	return captureSystemctl("status", Name(domain, service), "--no-pager", "-l")
}

// RemoveUnit stops, disables, and deletes domain/service's unit file --
// used by `wor service remove`, `wor clean`, and `wor reset`. Stop and
// disable failures are ignored (matching pm2.Run's "process may not
// exist" tolerance in the same callers) so a partially-created service
// can still be removed cleanly.
func RemoveUnit(domain, service string) error {
	name := Name(domain, service)
	_ = runSystemctl("stop", name)
	_ = runSystemctl("disable", name)
	path := UnitPath(domain, service)
	if _, err := os.Stat(path); err == nil {
		if err := osutil.RemoveFilePrivileged(path); err != nil {
			return fmt.Errorf("cannot remove systemd unit (%s): %w", osutil.ElevationHint(), err)
		}
	}
	return runSystemctl("daemon-reload")
}

// ListUnits returns every "wor_*.service" unit file name (without the
// directory) currently installed, for `wor clean`/`wor reset`'s orphan
// cleanup -- the systemd equivalent of PM2's `pm2 jlist` scan
// (pm2helpers.go). Reading /etc/systemd/system's directory listing
// itself needs no elevation; only writing/removing units does.
func ListUnits() ([]string, error) {
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "wor_") && strings.HasSuffix(name, ".service") {
			out = append(out, name)
		}
	}
	return out, nil
}

// Logs tails domain/service's journal, mirroring pm2.Run("logs", ...).
func Logs(domain, service, lines string) error {
	cmd, err := osutil.SudoCommand("journalctl", "-u", Name(domain, service), "-n", lines, "-f")
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
