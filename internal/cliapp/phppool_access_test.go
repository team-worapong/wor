package cliapp

import (
	"bytes"
	"strings"
	"testing"

	"wor/internal/osutil"
)

// newPoolAccessTestApp builds a test App with a single registered
// service under a real (temp) WOR_HOME, so reapplyPHPPoolAccess has a
// services.config.json to read.
func newPoolAccessTestApp(t *testing.T, domain, service, template string) *App {
	t.Helper()
	app := newTestServiceApp(t)
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService(domain, service, "", 0, template, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	return app
}

// TestReapplyPHPPoolAccessSkipsServicesWithoutAPool pins the guard that
// keeps `wor deploy` on a node/go/python/static service from touching
// file permissions at all. Without it, every deploy of every service
// would shell out to chgrp/chmod (and so to sudo) for a pool that does
// not exist.
func TestReapplyPHPPoolAccessSkipsServicesWithoutAPool(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "webapp", "node")

	app.reapplyPHPPoolAccess("shop-example-com", "webapp")

	if out := app.Err.(*bytes.Buffer).String(); out != "" {
		t.Errorf("a service with no php-fpm pool should produce no output, got %q", out)
	}
}

// TestReapplyPHPPoolAccessWarnsWhenPoolGroupMissing covers the service
// that has a pool but no recorded group. Re-deriving the group from the
// document root's current owner is exactly what must NOT happen here --
// that owner is whichever operator account last wrote to the tree -- so
// the only safe move is to say so and leave the permissions alone.
func TestReapplyPHPPoolAccessWarnsWhenPoolGroupMissing(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")
	if err := app.Store.SetServicePHPFPM("shop-example-com", "site", "8.3", "", 0); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}

	app.reapplyPHPPoolAccess("shop-example-com", "site")

	out := app.Err.(*bytes.Buffer).String()
	if !strings.Contains(out, "no recorded pool group") {
		t.Errorf("expected a warning about the missing pool group, got %q", out)
	}
}

// TestReapplyPHPPoolAccessStopsAtAMissingDocumentRoot pins the order of
// the two guards: a pool whose document root is gone must be reported,
// not handed to chgrp/chmod as a path that does not exist.
func TestReapplyPHPPoolAccessStopsAtAMissingDocumentRoot(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")
	if err := app.Store.SetServicePHPFPM("shop-example-com", "site", "8.3", "wor_shop-example-com_site", 0); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}

	app.reapplyPHPPoolAccess("shop-example-com", "site")

	out := app.Err.(*bytes.Buffer).String()
	if !strings.Contains(out, "no document root") {
		t.Errorf("expected a warning about the missing document root, got %q", out)
	}
}

// TestReapplyPHPPoolAccessCoversEveryServiceOnABareDomain covers the
// bare-domain target that `wor source pull <domain>` and `wor source
// clone <domain>` both accept: those rewrite the trees of every service
// under the domain at once, so every pooled service beneath it has to be
// re-granted, not just a named one. Services without a pool must still
// be passed over in silence.
func TestReapplyPHPPoolAccessCoversEveryServiceOnABareDomain(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "api", "node")
	if err := app.Store.AddService("shop-example-com", "site", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "blog", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	for _, svc := range []string{"site", "blog"} {
		if err := app.Store.SetServicePHPFPM("shop-example-com", svc, "8.3", "", 0); err != nil {
			t.Fatalf("SetServicePHPFPM(%s): %v", svc, err)
		}
	}

	app.reapplyPHPPoolAccess("shop-example-com", "")

	out := app.Err.(*bytes.Buffer).String()
	for _, svc := range []string{"site", "blog"} {
		if !strings.Contains(out, "shop-example-com/"+svc) {
			t.Errorf("bare-domain re-grant skipped pooled service %q; output was %q", svc, out)
		}
	}
	if strings.Contains(out, "shop-example-com/api") {
		t.Errorf("bare-domain re-grant should stay silent about the non-pooled service, got %q", out)
	}
}
