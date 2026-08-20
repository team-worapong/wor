package cliapp

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
	"wor/internal/osutil"
)

// newOperatorTestApp builds an App whose prompts read from input and
// whose WOR_HOME is a real temp directory.
func newOperatorTestApp(t *testing.T, input, configured string) *App {
	t.Helper()
	worHome := t.TempDir()
	return &App{
		Cfg:   &config.Config{WorHome: worHome, OperatorUser: configured},
		Store: domainmodel.NewStore(worHome + "/domains"),
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
		In:    bufio.NewReader(strings.NewReader(input)),
	}
}

func requireLinux(t *testing.T) {
	t.Helper()
	if !osutil.IsLinux() {
		t.Skip("the operator account step is Linux-only (see setupOperatorAccount)")
	}
}

// TestSetupOperatorAccountDefaultsToTheConfiguredValue is what makes
// `wor setup` safe to re-run: pressing enter has to keep the account the
// host already uses, never silently reset it to the built-in default.
func TestSetupOperatorAccountDefaultsToTheConfiguredValue(t *testing.T) {
	requireLinux(t)
	app := newOperatorTestApp(t, "\n", "worsvc")

	app.setupOperatorAccount()

	if app.Cfg.OperatorUser != "worsvc" {
		t.Errorf("OperatorUser = %q, want the already-configured %q", app.Cfg.OperatorUser, "worsvc")
	}
}

// TestSetupOperatorAccountDefaultsForAFreshHost covers the host that has
// never chosen one.
func TestSetupOperatorAccountDefaultsForAFreshHost(t *testing.T) {
	requireLinux(t)
	app := newOperatorTestApp(t, "\n", "")

	app.setupOperatorAccount()

	if app.Cfg.OperatorUser != DefaultOperatorUser {
		t.Errorf("OperatorUser = %q, want the default %q", app.Cfg.OperatorUser, DefaultOperatorUser)
	}
}

// TestSetupOperatorAccountAcceptsNone is the opt-out. An existing
// single-admin host must be able to decline and keep behaving exactly as
// it did before this step existed.
func TestSetupOperatorAccountAcceptsNone(t *testing.T) {
	requireLinux(t)
	for _, answer := range []string{"none\n", "NONE\n"} {
		app := newOperatorTestApp(t, answer, "worsvc")

		app.setupOperatorAccount()

		if app.Cfg.OperatorUser != "" {
			t.Errorf("answering %q: OperatorUser = %q, want it cleared", strings.TrimSpace(answer), app.Cfg.OperatorUser)
		}
	}
}

// TestSetupOperatorAccountTakesACustomName covers migrating from one
// account to another, which is the whole reason the answer is not just
// a yes/no.
func TestSetupOperatorAccountTakesACustomName(t *testing.T) {
	requireLinux(t)
	app := newOperatorTestApp(t, "  worsvc  \n", "wor")

	app.setupOperatorAccount()

	if app.Cfg.OperatorUser != "worsvc" {
		t.Errorf("OperatorUser = %q, want %q with surrounding space trimmed", app.Cfg.OperatorUser, "worsvc")
	}
}

// TestApplyOperatorAccountIsANoopWhenDeclined guards the opt-out all the
// way through: declining must not create an account or write host.env.
func TestApplyOperatorAccountIsANoopWhenDeclined(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	app.Cfg.Configs = app.Cfg.WorHome + "/configs"

	if err := app.applyOperatorAccount(); err != nil {
		t.Fatalf("applyOperatorAccount with no account configured: %v", err)
	}
	if _, err := os.Stat(app.Cfg.Configs + "/host.env"); !os.IsNotExist(err) {
		t.Error("declining an operator account must not write host.env")
	}
}

// TestPreviousOperatorUIDsExcludesTargetAndRoot pins the two exclusions
// that keep alignTreeOwnership from doing damage: it must never sweep
// files that already belong to the target (pointless sudo on every
// setup) and never sweep root-owned files, which include directories
// under WOR_HOME that wor did not create.
func TestPreviousOperatorUIDsExcludesTargetAndRoot(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	me := os.Getuid()

	for _, uid := range app.previousOperatorUIDs(me, nil) {
		if uid == me {
			t.Error("the target account's own uid must not be swept")
		}
		if uid == 0 {
			t.Error("root-owned files must never be swept")
		}
	}
}

// TestPreviousOperatorUIDsFindsTheCurrentUser is the case that matters
// on a host set up before an operator account existed: the tree belongs
// to the admin who has been running wor, and that is precisely who the
// files have to be moved away from.
func TestPreviousOperatorUIDsFindsTheCurrentUser(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	if os.Getuid() == 0 {
		t.Skip("running as root, whose uid is excluded by design")
	}

	// A target uid that cannot collide with the test account's.
	uids := app.previousOperatorUIDs(os.Getuid()+4242, nil)

	found := false
	for _, uid := range uids {
		if uid == os.Getuid() {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the current uid %d among %v", os.Getuid(), uids)
	}
}

// TestSweepDirsExcludesWorHomeItself is the boundary that keeps
// alignTreeOwnership out of directories wor did not create. find(1)
// recurses into whatever it is handed, so WOR_HOME appearing in this
// list would silently put every sibling directory -- a CI pipeline's
// upload staging area, anything else parked there -- back in scope.
func TestSweepDirsExcludesWorHomeItself(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	app.Cfg.Domains = app.Cfg.WorHome + "/domains"
	app.Cfg.Backups = app.Cfg.WorHome + "/backups"
	app.Cfg.Configs = app.Cfg.WorHome + "/configs"
	app.Cfg.Logs = app.Cfg.WorHome + "/logs"
	app.Cfg.SSL = app.Cfg.WorHome + "/ssl"
	app.Cfg.ACME = app.Cfg.SSL + "/acme"

	dirs := app.sweepDirs()

	if len(dirs) == 0 {
		t.Fatal("expected wor's own directories to be swept")
	}
	for _, d := range dirs {
		if d == app.Cfg.WorHome {
			t.Errorf("WOR_HOME itself must not be swept recursively, found %q", d)
		}
		if filepath.Dir(d) != app.Cfg.WorHome {
			t.Errorf("%q is nested deeper than WOR_HOME's own children; its parent is already swept", d)
		}
	}
}

// TestSweepDirsSkipsAForeignDirectory states the case from the host this
// came from: a `deployments/` staging area belonging to a CI account
// lives under WOR_HOME and must never appear in the sweep.
func TestSweepDirsSkipsAForeignDirectory(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	app.Cfg.Domains = app.Cfg.WorHome + "/domains"
	foreign := app.Cfg.WorHome + "/deployments"
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, d := range app.sweepDirs() {
		if d == foreign {
			t.Errorf("a directory wor never created must not be swept: %q", d)
		}
	}
}

// TestChownTreeFromUIDBuildsAnOwnerOnlyChown pins the single most
// dangerous detail of the migration: the sweep must change the owning
// user and leave the group alone.
//
// A per-service php-fpm pool reads its document root by being a member
// of that root's group (phpfpm.GrantGroupAccess). Rewriting the group
// while moving files to a new operator account silently revokes that --
// invisibly for the 0644 files a service tree is mostly made of, which
// stay readable through the `other` bits, and fatally for `src/.env` at
// 0640, which is exactly the file an application cannot start without.
//
// Asserting on the constructed argv rather than on a real chown keeps
// this checkable without root: a bare uid means "owner only", a
// "uid:gid" pair would mean the group goes too.
func TestChownTreeFromUIDBuildsAnOwnerOnlyChown(t *testing.T) {
	args := findChownArgs([]string{"/opt/wor/domains"}, 1001, 1234)

	chownAt := -1
	for i, arg := range args {
		if arg == "chown" {
			chownAt = i
		}
	}
	if chownAt < 0 {
		t.Fatalf("no chown in the constructed command: %v", args)
	}
	for _, arg := range args[chownAt:] {
		if strings.Contains(arg, ":") {
			t.Errorf("chown argument %q carries a group; the sweep must change the owner only", arg)
		}
	}
	if got := args[len(args)-3]; got != "1234" {
		t.Errorf("expected the bare target uid as the chown operand, got %q in %v", got, args)
	}
}

// TestPreviousOperatorUIDsSweepsTheDirectoryOwners is the case a
// rehearsal against a copy of the first production host caught, and the
// reason treeOwnerUIDs exists.
//
// There, WOR_HOME belonged to one admin's login while every directory
// under it belonged to a different one. Enumerating only the previous
// operator, the current user and WOR_HOME's owner collapsed to a single
// account, so the sweep moved WOR_HOME's own entry and left the entire
// service tree behind -- a half-migrated host that looks finished.
func TestPreviousOperatorUIDsSweepsTheDirectoryOwners(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	me := os.Getuid()

	// The owner of the directories inside WOR_HOME, which on a drifted
	// host is nobody the other three rules name.
	const dirOwner = 4242
	if me == dirOwner {
		t.Skip("test account collides with the stand-in uid")
	}

	uids := app.previousOperatorUIDs(me, []int{dirOwner})

	found := false
	for _, uid := range uids {
		if uid == dirOwner {
			found = true
		}
	}
	if !found {
		t.Errorf("the account owning wor's directories (%d) must be swept, got %v", dirOwner, uids)
	}
}

// TestTreeOwnerUIDsIsReadBeforeTheDirectoriesMove guards the ordering
// that makes the above work: treeOwnerUIDs reports what it sees at the
// moment it is called, so cmdSetup must call it before ensureRootDirs
// re-owns those directories. Called after, it would report the new
// operator account and the sweep would have nothing to do.
func TestTreeOwnerUIDsIsReadBeforeTheDirectoriesMove(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	app.Cfg.Domains = filepath.Join(app.Cfg.WorHome, "domains")
	if err := os.MkdirAll(app.Cfg.Domains, 0o755); err != nil {
		t.Fatal(err)
	}

	owners := app.treeOwnerUIDs()

	if len(owners) == 0 {
		t.Fatal("expected at least WOR_HOME's own owner")
	}
	for _, uid := range owners {
		if uid != os.Getuid() {
			t.Errorf("uid %d owns nothing the test created; got %v", uid, owners)
		}
	}
}
