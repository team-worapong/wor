package domainmodel

import "testing"

func TestListAllServices(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles(shop): %v", err)
	}
	if err := store.AddService("shop-example-com", "webapp", "", 3000, "node", ""); err != nil {
		t.Fatalf("AddService(webapp): %v", err)
	}
	if err := store.AddService("shop-example-com", "api-gateway", "", 8080, "go", ""); err != nil {
		t.Fatalf("AddService(api-gateway): %v", err)
	}

	if err := store.MakeDomainFiles("blog-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles(blog): %v", err)
	}
	if err := store.AddService("blog-example-com", "cms", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService(cms): %v", err)
	}

	refs, err := store.ListAllServices()
	if err != nil {
		t.Fatalf("ListAllServices: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 services, got %d", len(refs))
	}

	byTarget := map[string]ServiceRef{}
	for _, ref := range refs {
		byTarget[ref.Domain+"/"+ref.Service.Name] = ref
	}

	if _, ok := byTarget["shop-example-com/webapp"]; !ok {
		t.Error("missing shop-example-com/webapp")
	}
	if _, ok := byTarget["shop-example-com/api-gateway"]; !ok {
		t.Error("missing shop-example-com/api-gateway")
	}
	cms, ok := byTarget["blog-example-com/cms"]
	if !ok {
		t.Fatal("missing blog-example-com/cms")
	}
	if cms.Service.Type != "php" {
		t.Errorf("cms.Type = %q, want php", cms.Service.Type)
	}
	if !cms.Service.Enabled {
		t.Error("cms.Enabled = false, want true (AddService defaults to enabled)")
	}
}

func TestListAllServicesNoDomains(t *testing.T) {
	store := NewStore(t.TempDir())
	refs, err := store.ListAllServices()
	if err != nil {
		t.Fatalf("ListAllServices: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 services for an empty domains dir, got %d", len(refs))
	}
}

func TestServicePHPFPMDefaultsToFallback(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.MakeDomainFiles("blog-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := store.AddService("blog-example-com", "cms", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService(cms): %v", err)
	}

	// A freshly created php service has no per-service pool yet -- it
	// must fall back to the host-wide PHP_FPM_ENDPOINT, matching the
	// no-forced-migration decision for existing php services.
	if v := store.GetServicePHPVersion("blog-example-com", "cms"); v != "" {
		t.Errorf("GetServicePHPVersion() = %q, want \"\" before SetServicePHPFPM", v)
	}
	cfg, err := store.LoadServices("blog-example-com")
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	svc := cfg.FindService("cms")
	if svc == nil {
		t.Fatal("cms not found")
	}
	if svc.UsesPerServicePHPFPM() {
		t.Error("UsesPerServicePHPFPM() = true, want false before SetServicePHPFPM")
	}
}

func TestSetAndClearServicePHPFPM(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.MakeDomainFiles("blog-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := store.AddService("blog-example-com", "cms", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService(cms): %v", err)
	}

	if err := store.SetServicePHPFPM("blog-example-com", "cms", "8.3", "www-data", 10); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}
	if v := store.GetServicePHPVersion("blog-example-com", "cms"); v != "8.3" {
		t.Errorf("GetServicePHPVersion() = %q, want 8.3", v)
	}
	cfg, _ := store.LoadServices("blog-example-com")
	svc := cfg.FindService("cms")
	if svc.PHPPoolGroup != "www-data" {
		t.Errorf("PHPPoolGroup = %q, want www-data", svc.PHPPoolGroup)
	}
	if svc.PHPMaxChildren != 10 {
		t.Errorf("PHPMaxChildren = %d, want 10", svc.PHPMaxChildren)
	}
	if !svc.UsesPerServicePHPFPM() {
		t.Error("UsesPerServicePHPFPM() = false, want true after SetServicePHPFPM")
	}

	if err := store.ClearServicePHPFPM("blog-example-com", "cms"); err != nil {
		t.Fatalf("ClearServicePHPFPM: %v", err)
	}
	if v := store.GetServicePHPVersion("blog-example-com", "cms"); v != "" {
		t.Errorf("GetServicePHPVersion() after Clear = %q, want \"\"", v)
	}
}

func TestSetServicePHPFPMMissingService(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.MakeDomainFiles("blog-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := store.SetServicePHPFPM("blog-example-com", "does-not-exist", "8.3", "www-data", 0); err == nil {
		t.Error("expected an error for a nonexistent service, got nil")
	}
}
