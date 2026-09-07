package cliapp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/worlock"
)

func TestCommandNeedsLock(t *testing.T) {
	cases := []struct {
		cmd  string
		rest []string
		want bool
	}{
		{"version", nil, false},
		{"--version", nil, false},
		{"-v", nil, false},
		{"help", nil, false},
		{"-h", nil, false},
		{"--help", nil, false},
		{"", nil, false},
		{"service", []string{"logs", "shop/web"}, false},
		{"host", []string{"logs", "shop.test"}, false},
		{"service", []string{"status"}, true},
		{"service", nil, true},
		{"host", []string{"list"}, true},
		{"create", nil, true},
		{"setup", nil, true},
		{"deploy", []string{"shop.test"}, true},
		{"run", nil, true},
		{"doctor", nil, true},
		{"diagnose", []string{"shop.test"}, false},
		{"health", nil, false},
		// `ssl renew` hands control to `certbot renew`, which runs
		// `wor ssl sync` as its renewal hook -- a second wor process
		// that needs this same lock. Locking here would deadlock wor
		// against its own hook. Every other ssl action writes state
		// and must keep the lock.
		{"ssl", []string{"renew", "shop.test"}, false},
		{"ssl", []string{"issue", "shop.test"}, true},
		{"ssl", []string{"sync", "shop.test"}, true},
		{"ssl", []string{"remove", "shop.test"}, true},
		{"ssl", nil, true},
	}
	for _, c := range cases {
		got := commandNeedsLock(c.cmd, c.rest)
		if got != c.want {
			t.Errorf("commandNeedsLock(%q, %v) = %v, want %v", c.cmd, c.rest, got, c.want)
		}
	}
}

func TestRequiresInitializedWorkspace(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"version", false},
		{"--version", false},
		{"-v", false},
		{"help", false},
		{"-h", false},
		{"--help", false},
		{"", false},
		{"setup", false},
		{"doctor", false},
		{"env", true},
		{"clean", true},
		{"reset", true},
		{"create", true},
		{"domain", true},
		{"service", true},
		{"run", true},
		{"host", true},
		{"database", true},
		{"source", true},
		{"deploy", true},
		{"rollback", true},
		{"ssl", true},
		{"info", true},
		{"diagnose", true},
		{"health", true},
	}
	for _, c := range cases {
		got := requiresInitializedWorkspace(c.cmd)
		if got != c.want {
			t.Errorf("requiresInitializedWorkspace(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// The renewal hook is the only caller allowed to treat a busy lock as
// success, and it says so with an explicit flag rather than being
// recognised by shape. Anything an operator types keeps the normal
// behaviour, which is to fail and say another command is running.
func TestSkipsWhenLockBusy(t *testing.T) {
	cases := []struct {
		cmd  string
		rest []string
		want bool
	}{
		{"ssl", []string{"sync", "shop.test", "--skip-if-busy"}, true},
		{"ssl", []string{"sync", "shop.test"}, false},
		{"ssl", []string{"issue", "shop.test", "--skip-if-busy"}, false},
		{"ssl", []string{"sync"}, false},
		{"ssl", nil, false},
		{"service", []string{"add", "shop/web", "--skip-if-busy"}, false},
		{"deploy", []string{"--skip-if-busy"}, false},
	}
	for _, c := range cases {
		if got := skipsWhenLockBusy(c.cmd, c.rest); got != c.want {
			t.Errorf("skipsWhenLockBusy(%q, %v) = %v, want %v", c.cmd, c.rest, got, c.want)
		}
	}
}

// What certbot does with the hook's exit status is the whole point: a
// non-zero status is reported to the operator as a failed hook on an
// issuance that succeeded. So the contract pinned here is the exit
// code, through Run, with the lock genuinely held by another open file
// description -- not just the decision function above.
func TestRunExitsZeroWhenTheHookFindsTheLockBusy(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{
		WorHome: home,
		Domains: filepath.Join(home, "domains"),
		Backups: filepath.Join(home, "backups"),
		Configs: filepath.Join(home, "configs"),
		Logs:    filepath.Join(home, "logs"),
		SSL:     filepath.Join(home, "ssl"),
	}
	for _, d := range []string{cfg.Domains, cfg.Backups, cfg.Configs, cfg.Logs, cfg.SSL} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	held, err := worlock.Acquire(home)
	if err != nil {
		t.Fatalf("could not take the lock the test needs held: %v", err)
	}
	defer held.Release()

	var out bytes.Buffer
	app := &App{Cfg: cfg, Out: &out, Err: io.Discard}
	if code := app.Run([]string{"ssl", "sync", "app.example.com", "--skip-if-busy"}); code != 0 {
		t.Errorf("exit code = %d, want 0 -- certbot reports any other value as a failed hook", code)
	}

	// The message has to leave the reader with something to do, not
	// just the news that nothing happened.
	for _, want := range []string{
		"wor ssl status app.example.com",
		"wor ssl sync app.example.com",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("skip message must tell the reader how to check and repair; missing %q in:\n%s", want, out.String())
		}
	}
}

// Without the flag the same command must still fail: for an operator
// typing it, a busy lock is a real answer -- wait for the other command
// -- and silently doing nothing would be worse than the error.
func TestRunStillFailsForAHandTypedSyncWhenTheLockIsBusy(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{
		WorHome: home,
		Domains: filepath.Join(home, "domains"),
		Backups: filepath.Join(home, "backups"),
		Configs: filepath.Join(home, "configs"),
		Logs:    filepath.Join(home, "logs"),
		SSL:     filepath.Join(home, "ssl"),
	}
	for _, d := range []string{cfg.Domains, cfg.Backups, cfg.Configs, cfg.Logs, cfg.SSL} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	held, err := worlock.Acquire(home)
	if err != nil {
		t.Fatalf("could not take the lock the test needs held: %v", err)
	}
	defer held.Release()

	app := &App{Cfg: cfg, Out: io.Discard, Err: io.Discard}
	if code := app.Run([]string{"ssl", "sync", "app.example.com"}); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
