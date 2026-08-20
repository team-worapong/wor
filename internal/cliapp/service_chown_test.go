package cliapp

import (
	"os/user"
	"strconv"
	"strings"
	"testing"
)

// TestResolveChownTargetDefaultsToTheOperator is what makes the command
// worth typing: the common case is "put this back the way setup left
// it", and that answer is already recorded on the host.
func TestResolveChownTargetDefaultsToTheOperator(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	app := newOperatorTestApp(t, "", me.Username)

	target, err := app.resolveChownTarget(nil)
	if err != nil {
		t.Fatalf("expected the configured operator to be used: %v", err)
	}
	if target.Username != me.Username {
		t.Errorf("expected the operator account %q, got %q", me.Username, target.Username)
	}
}

// TestResolveChownTargetPrefersAnExplicitAccount pins that naming an
// account overrides the recorded operator -- otherwise the argument in
// `wor service chown <svc> <user>` would be silently ignored.
func TestResolveChownTargetPrefersAnExplicitAccount(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	app := newOperatorTestApp(t, "", "some-other-operator")

	target, err := app.resolveChownTarget([]string{me.Username})
	if err != nil {
		t.Fatalf("expected the named account to be used: %v", err)
	}
	if target.Username != me.Username {
		t.Errorf("expected the named account %q, got %q", me.Username, target.Username)
	}
}

// TestResolveChownTargetRefusesAPoolAccount is the guard that keeps the
// command from being used to invert the access model.
//
// The pool account is the one most obviously "associated with" a
// service, so it is the plausible thing to type -- and handing it
// ownership would let a compromised php worker rewrite the application
// it is executing, instead of only reading it through the group.
func TestResolveChownTargetRefusesAPoolAccount(t *testing.T) {
	app := newOperatorTestApp(t, "", "")

	// Find a real account matching the pool naming convention; without
	// one there is nothing to resolve and nothing to assert.
	name := findPoolAccount(t)

	_, err := app.resolveChownTarget([]string{name})

	if err == nil {
		t.Fatalf("%s is a php-fpm pool account and must not be accepted as an owner", name)
	}
	if !strings.Contains(err.Error(), "pool account") {
		t.Errorf("expected the refusal to explain why, got %q", err)
	}
}

// TestResolveChownTargetWithoutAnOperatorAsksForAName covers the host
// that never ran the operator step: there is no default to fall back on,
// and guessing one (the current user, say) would quietly reintroduce the
// "files belong to whoever typed the command" rule the operator account
// exists to replace.
func TestResolveChownTargetWithoutAnOperatorAsksForAName(t *testing.T) {
	app := newOperatorTestApp(t, "", "")

	_, err := app.resolveChownTarget(nil)

	if err == nil {
		t.Fatal("expected a refusal when no account is named and none is configured")
	}
	if !strings.Contains(err.Error(), "wor setup") {
		t.Errorf("expected the error to point at how to record an operator, got %q", err)
	}
}

// TestIsServiceAccountNameMatchesThePoolConvention pins the one string
// both this command and doctor's ownership report depend on. They used
// to carry the prefix separately; if the pool naming convention in
// phpfpm.PoolName ever changes, this is the assertion that should fail.
func TestIsServiceAccountNameMatchesThePoolConvention(t *testing.T) {
	if !isServiceAccountName("wor_example.com_web") {
		t.Error("a pool account created by phpfpm.PoolName must be recognised")
	}
	for _, human := range []string{"wor", "worsvc", "teems", "deploy", "root"} {
		if isServiceAccountName(human) {
			t.Errorf("%q is an operator account, not a pool account", human)
		}
	}
}

// findPoolAccount returns the name of an account on this machine that
// follows the pool naming convention, skipping the test when there is
// none -- the refusal cannot be observed without a name that resolves.
func findPoolAccount(t *testing.T) string {
	t.Helper()
	for uid := 1000; uid < 65000; uid++ {
		u, err := user.LookupId(strconv.Itoa(uid))
		if err != nil {
			continue
		}
		if isServiceAccountName(u.Username) {
			return u.Username
		}
	}
	t.Skip("no php-fpm pool account on this machine to resolve")
	return ""
}
