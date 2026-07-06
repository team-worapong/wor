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
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
CPUAccounting=yes
MemoryAccounting=yes

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

// Info is the live-state subset of `systemctl show` that `wor service
// status` needs: whether the unit is active, systemd's own state label
// (e.g. "active", "inactive", "failed"), the main process's PID (0 if
// not running), and CPU/memory usage -- CPUKnown/MemKnown are false
// when the unit's accounting data isn't available yet (e.g. a
// previously-installed unit that predates this package enabling
// CPUAccounting/MemoryAccounting in unitFileContent; it needs its unit
// file rewritten -- see WriteUnit -- before these populate).
type Info struct {
	Active      bool
	State       string
	PID         int
	CPUPercent  float64
	CPUKnown    bool
	MemoryBytes int64
	MemKnown    bool
}

// sampleShowProperties is what `systemctl show` reports for one unit at
// one point in time -- the raw material for both GetInfo (a single
// sample, no CPU%) and GetInfoBatch (two samples, so it can derive a
// CPU% the way `top`/`docker stats` do).
type sampleShowProperties struct {
	state    string
	pid      int
	cpuNSec  uint64
	cpuKnown bool
	memBytes int64
	memKnown bool
}

const showProperties = "ActiveState,MainPID,CPUUsageNSec,MemoryCurrent"

// querySample runs `systemctl show` for domain/service's unit once.
// Like GetInfo, this runs unelevated (plain exec.Command, not
// runSystemctl/captureSystemctl's SudoCommand wrapper): querying unit
// state is a read-only operation systemd itself does not require root
// for, and routing it through the sudo gate would force a confirm-once
// elevation prompt just to run `wor service status`.
func querySample(domain, service string) (sampleShowProperties, error) {
	out, err := exec.Command("systemctl", "show", Name(domain, service), "--property="+showProperties).Output()
	if err != nil {
		return sampleShowProperties{}, err
	}
	return parseSample(string(out)), nil
}

// parseSample parses `systemctl show --property=...`'s "KEY=VALUE"
// per-line output. Split out from querySample so the parsing logic can
// be unit tested without invoking the real systemctl binary.
func parseSample(out string) sampleShowProperties {
	s := sampleShowProperties{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			s.state = value
		case "MainPID":
			fmt.Sscanf(value, "%d", &s.pid)
		case "CPUUsageNSec":
			if n, ok := parseSystemdUint(value); ok {
				s.cpuNSec, s.cpuKnown = n, true
			}
		case "MemoryCurrent":
			if n, ok := parseSystemdUint(value); ok {
				s.memBytes, s.memKnown = int64(n), true
			}
		}
	}
	return s
}

// parseSystemdUint parses one of systemd's numeric property values,
// treating both "[not set]" (the property was never recorded -- e.g.
// accounting is disabled for this unit) and systemd's UINT64_MAX
// sentinel (its convention for "unknown"/"unbounded") as "not
// available" rather than a real 0 or huge number.
func parseSystemdUint(value string) (uint64, bool) {
	if value == "" || value == "[not set]" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == math.MaxUint64 {
		return 0, false
	}
	return n, true
}

// GetInfo queries domain/service's unit for its current state (active,
// PID, memory) without a CPU%, which needs two samples spaced apart --
// see GetInfoBatch for that.
func GetInfo(domain, service string) (Info, error) {
	s, err := querySample(domain, service)
	if err != nil {
		return Info{}, err
	}
	info := Info{State: s.state, Active: s.state == "active", PID: s.pid}
	if s.memKnown {
		info.MemoryBytes = s.memBytes
		info.MemKnown = true
	}
	return info, nil
}

// Ref identifies one domain/service pair's systemd unit, for batched
// queries (GetInfoBatch).
type Ref struct {
	Domain  string
	Service string
}

// cpuSampleInterval is how far apart GetInfoBatch's two `systemctl show`
// samples are taken. Long enough that CPUUsageNSec's delta isn't
// dominated by measurement noise, short enough that `wor service
// status` still feels instant.
const cpuSampleInterval = 200 * time.Millisecond

// GetInfoBatch queries every ref's live state -- including a CPU%
// derived from two systemctl samples cpuSampleInterval apart -- in one
// pass: first samples are taken for every ref, then (after a single
// shared sleep, not one per ref) second samples are taken and each
// ref's CPU% comes from its own CPUUsageNSec delta over that shared
// interval. This mirrors pm2.List()'s "query everything once" shape, so
// `wor service status` pays the sampling latency exactly once no matter
// how many systemd-managed services it reports on.
func GetInfoBatch(refs []Ref) map[Ref]Info {
	first := make(map[Ref]sampleShowProperties, len(refs))
	for _, ref := range refs {
		if s, err := querySample(ref.Domain, ref.Service); err == nil {
			first[ref] = s
		}
	}

	time.Sleep(cpuSampleInterval)

	result := make(map[Ref]Info, len(refs))
	for _, ref := range refs {
		second, err := querySample(ref.Domain, ref.Service)
		if err != nil {
			result[ref] = Info{}
			continue
		}
		info := Info{State: second.state, Active: second.state == "active", PID: second.pid}
		if second.memKnown {
			info.MemoryBytes = second.memBytes
			info.MemKnown = true
		}
		if firstSample, ok := first[ref]; ok {
			if pct, ok := cpuPercentFromDelta(firstSample, second, cpuSampleInterval); ok {
				info.CPUPercent = pct
				info.CPUKnown = true
			}
		}
		result[ref] = info
	}
	return result
}

// cpuPercentFromDelta turns two CPUUsageNSec samples into a CPU% over
// interval (100% == one full core saturated throughout the interval,
// same convention as `top`). Split out from GetInfoBatch so the math
// can be unit tested without sleeping/shelling out. Returns false if
// either sample lacks CPU accounting data, or the second reading is
// somehow behind the first (a unit restart between samples, clock
// weirdness, etc.).
func cpuPercentFromDelta(first, second sampleShowProperties, interval time.Duration) (float64, bool) {
	if !first.cpuKnown || !second.cpuKnown || interval <= 0 || second.cpuNSec < first.cpuNSec {
		return 0, false
	}
	deltaNSec := second.cpuNSec - first.cpuNSec
	return float64(deltaNSec) / float64(interval.Nanoseconds()) * 100, true
}

// DiagState is the failure-analysis subset of `systemctl show` that
// `wor diagnose` needs -- unlike Info (live resource usage for status
// rows), this answers "why is this unit not running": systemd's own
// Result verdict ("success", "exit-code", "oom-kill", "signal",
// "start-limit-hit", ...), the main process's last exit status, and how
// many times systemd has auto-restarted it.
type DiagState struct {
	ActiveState    string // "active", "inactive", "failed", "activating", ...
	SubState       string // "running", "dead", "auto-restart", ...
	Result         string // why the last run ended the way it did
	NRestarts      int
	ExecMainStatus int // exit status of the main process's last run
}

// ShowDiagState queries domain/service's unit for DiagState. Like
// querySample, this runs unelevated: `systemctl show` is read-only and
// never needs root, and `wor diagnose` must stay non-interactive (no
// confirm-once sudo prompt) so it can run from cron/monitoring.
func ShowDiagState(domain, service string) (DiagState, error) {
	out, err := exec.Command("systemctl", "show", Name(domain, service),
		"--property=ActiveState,SubState,Result,NRestarts,ExecMainStatus").Output()
	if err != nil {
		return DiagState{}, err
	}
	return parseDiagState(string(out)), nil
}

// parseDiagState parses `systemctl show --property=...`'s "KEY=VALUE"
// per-line output for ShowDiagState. Split out (like parseSample) so it
// can be unit tested without invoking the real systemctl binary.
func parseDiagState(out string) DiagState {
	var d DiagState
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			d.ActiveState = value
		case "SubState":
			d.SubState = value
		case "Result":
			d.Result = value
		case "NRestarts":
			if n, err := strconv.Atoi(value); err == nil {
				d.NRestarts = n
			}
		case "ExecMainStatus":
			if n, err := strconv.Atoi(value); err == nil {
				d.ExecMainStatus = n
			}
		}
	}
	return d
}

// RecentLogs returns the last n journal lines for domain/service's
// unit, for `wor diagnose`'s evidence gathering. Unelevated and
// non-following (unlike Logs): reading the system journal as a normal
// user works when that user is in the systemd-journal/adm group
// (common on Debian for the admin account); when it isn't, the caller
// gets an error/empty result and reports "not readable" instead of
// triggering a sudo prompt mid-diagnosis.
func RecentLogs(domain, service string, n int) (string, error) {
	out, err := exec.Command("journalctl", "-u", Name(domain, service),
		"-n", strconv.Itoa(n), "--no-pager", "--output=cat").Output()
	return strings.TrimSpace(string(out)), err
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
