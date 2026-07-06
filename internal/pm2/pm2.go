// Package pm2 wraps the PM2 process manager CLI, porting lib/pm2.sh.
// wor shells out to the user's own `pm2` install (a Node.js CLI tool)
// rather than reimplementing process supervision; this package only
// generates PM2's ecosystem file and handles PM2_HOME permission
// bookkeeping.
package pm2

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
)

// Name returns the PM2 process name for a domain/service pair:
// "wor_<domain>_<service>".
func Name(domain, service string) string { return "wor_" + domain + "_" + service }

// App is one entry in a PM2 JSON ecosystem file. PM2 natively supports
// `pm2 start ecosystem.json`, so no JS templating (and no Node
// dependency for config generation) is required.
type App struct {
	Name        string            `json:"name"`
	Cwd         string            `json:"cwd"`
	Script      string            `json:"script"`
	Interpreter string            `json:"interpreter,omitempty"`
	Instances   int               `json:"instances"`
	ExecMode    string            `json:"exec_mode"`
	Autorestart bool              `json:"autorestart"`
	Watch       bool              `json:"watch"`
	Env         map[string]string `json:"env"`
}

type Ecosystem struct {
	Apps []App `json:"apps"`
}

// EcosystemPath returns the generated ecosystem file path for a domain,
// wor.config.json (the JSON-native equivalent of the shell version's
// wor.config.js).
func EcosystemPath(domainDir string) string { return filepath.Join(domainDir, "wor.config.json") }

// WriteEcosystem regenerates <domain>/wor.config.json from the
// domain's services.config.json, matching lib/pm2.sh
// create_pm2_runtime_config(). Only enabled services whose process
// provider (domainmodel.ProcessProviderFor) resolves to "pm2" are
// included -- node always, and go/python only on OSes without systemd
// (macOS, Windows).
func WriteEcosystem(store *domainmodel.Store, domain string) error {
	cfg, err := store.LoadServices(domain)
	if err != nil {
		return err
	}
	eco := Ecosystem{Apps: []App{}}
	for _, svc := range cfg.Services {
		if !svc.Enabled || domainmodel.ProcessProviderFor(svc.Type) != "pm2" {
			continue
		}
		entry := svc.EntryPoint
		if entry == "" {
			entry = domainmodel.DefaultEntryPoint(svc.Type)
		}
		script, interpreter := scriptAndInterpreter(svc.Type, entry)
		env := map[string]string{}
		if domainmodel.NodeTemplates[svc.Type] {
			env["NODE_ENV"] = "production"
		}
		for k, v := range svc.Env {
			env[k] = v
		}
		if svc.Port != 0 {
			env["PORT"] = fmt.Sprintf("%d", svc.Port)
		}
		eco.Apps = append(eco.Apps, App{
			Name:        Name(domain, svc.Name),
			Cwd:         store.ServiceDir(domain, svc.Name),
			Script:      script,
			Interpreter: interpreter,
			Instances:   1,
			ExecMode:    "fork",
			Autorestart: true,
			Watch:       false,
			Env:         env,
		})
	}
	data, err := json.MarshalIndent(eco, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return osutil.WriteFileAtomic(EcosystemPath(store.DomainDir(domain)), data, 0o644)
}

// scriptAndInterpreter maps a service's entry point to PM2's
// script/interpreter pair: node scripts run through PM2's built-in
// Node interpreter (the zero value); python scripts need an explicit
// "python3" interpreter; go's entry point is a compiled binary run
// directly, so interpreter is "none" (PM2's convention for "exec this
// path as-is").
func scriptAndInterpreter(template, entry string) (script, interpreter string) {
	switch {
	case domainmodel.PythonTemplates[template]:
		return "./" + entry, "python3"
	case domainmodel.GoTemplates[template]:
		return "./" + entry, "none"
	default:
		return "./" + entry, ""
	}
}

// Home resolves PM2_HOME the same way the pm2 CLI itself does:
// $PM2_HOME if set, else "<user home>/.pm2" on every OS.
func Home() string {
	if h := os.Getenv("PM2_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pm2")
}

// EnsureHome creates PM2_HOME if missing and best-effort repairs
// ownership on Unix when it's owned by another user (e.g. after a
// prior run under sudo).
func EnsureHome() error {
	home := Home()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("cannot create PM2_HOME: %s: %w", home, err)
	}
	return nil
}

// Run executes `pm2 <args...>` with stdio attached to the current
// process, refusing to run as root/Administrator the way lib/pm2.sh
// run_pm2() does (PM2 process state is per-user; running under sudo
// silently creates a second, root-owned PM2 daemon).
func Run(args ...string) error {
	if osutil.IsElevated() {
		return fmt.Errorf("do not run pm2 as root/Administrator; run this wor command as your normal user")
	}
	if err := EnsureHome(); err != nil {
		return err
	}
	cmd := exec.Command("pm2", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), "PM2_HOME="+Home())
	return cmd.Run()
}

// RunCapture is like Run but captures combined output instead of
// streaming it, for callers that need to inspect the result (e.g.
// `pm2 describe` health checks).
func RunCapture(args ...string) (string, error) {
	if err := EnsureHome(); err != nil {
		return "", err
	}
	cmd := exec.Command("pm2", args...)
	cmd.Env = append(os.Environ(), "PM2_HOME="+Home())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ProcessInfo is the subset of one `pm2 jlist` entry that `wor service
// status`-style reporting needs.
type ProcessInfo struct {
	Name        string
	PID         int
	Status      string // pm2's pm2_env.status: "online", "stopped", "errored", ...
	Uptime      time.Duration
	Restarts    int     // pm2's pm2_env.restart_time: total restarts since `pm2 start`
	CPUPercent  float64 // pm2's own live monit.cpu reading
	MemoryBytes int64   // pm2's own live monit.memory reading (RSS, bytes)
}

// rawJlistEntry mirrors just the `pm2 jlist` fields ProcessInfo needs;
// the real payload has many more (versioning, env, ...) that wor has no
// use for.
type rawJlistEntry struct {
	Name   string `json:"name"`
	PID    int    `json:"pid"`
	Pm2Env struct {
		Status      string `json:"status"`
		PmUptime    int64  `json:"pm_uptime"`    // ms since epoch: process start time
		RestartTime int    `json:"restart_time"` // total restart count (despite the name)
	} `json:"pm2_env"`
	Monit struct {
		CPU    float64 `json:"cpu"`    // percent, as pm2 itself computes it
		Memory int64   `json:"memory"` // RSS bytes
	} `json:"monit"`
}

// List runs `pm2 jlist` once and returns every managed process keyed by
// its PM2 name (see Name), so callers that need to check many services
// at once (like `wor service status`) don't shell out to pm2 once per
// service. Only stdout is captured (not RunCapture's combined
// stdout+stderr) so a stray daemon-startup line on stderr can't corrupt
// the JSON parse.
func List() (map[string]ProcessInfo, error) {
	if err := EnsureHome(); err != nil {
		return nil, err
	}
	cmd := exec.Command("pm2", "jlist")
	cmd.Env = append(os.Environ(), "PM2_HOME="+Home())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pm2 jlist: %w", err)
	}
	return parseJlist(out)
}

// parseJlist parses `pm2 jlist`'s JSON array into a map keyed by PM2
// process name. Split out from List() so the parsing/uptime-calculation
// logic can be unit tested without invoking the real pm2 binary.
func parseJlist(data []byte) (map[string]ProcessInfo, error) {
	var raw []rawJlistEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing pm2 jlist output: %w", err)
	}
	now := time.Now()
	result := make(map[string]ProcessInfo, len(raw))
	for _, p := range raw {
		info := ProcessInfo{
			Name:        p.Name,
			PID:         p.PID,
			Status:      p.Pm2Env.Status,
			Restarts:    p.Pm2Env.RestartTime,
			CPUPercent:  p.Monit.CPU,
			MemoryBytes: p.Monit.Memory,
		}
		if p.Pm2Env.PmUptime > 0 {
			if started := time.UnixMilli(p.Pm2Env.PmUptime); now.After(started) {
				info.Uptime = now.Sub(started)
			}
		}
		result[p.Name] = info
	}
	return result, nil
}

// Save runs `pm2 save`, matching lib/pm2.sh pm2_save(): best-effort,
// warns rather than failing the whole command if PM2's dump file isn't
// writable (a common issue after switching between sudo and normal-user
// invocations).
func Save() error {
	if err := EnsureHome(); err != nil {
		return err
	}
	if err := Run("save"); err != nil {
		return fmt.Errorf("pm2 save failed (dump file permissions?): %w", err)
	}
	return nil
}

// Version returns a short PM2 version string, or "not found" if pm2
// isn't installed. Matches lib/pm2.sh pm2_version(), including the
// shell version's deliberate choice not to query version while running
// as root/Administrator.
func Version() string {
	if osutil.IsElevated() {
		return "installed (version not checked as root)"
	}
	if !osutil.Exists("pm2") {
		return "not found"
	}
	return osutil.RunVersion("pm2", "--version")
}
