package domainmodel

import "testing"

func TestListAllServices(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.MakeDomainFiles("shop.example.com"); err != nil {
		t.Fatalf("MakeDomainFiles(shop): %v", err)
	}
	if err := store.AddService("shop.example.com", "webapp", "", 3000, "node", ""); err != nil {
		t.Fatalf("AddService(webapp): %v", err)
	}
	if err := store.AddService("shop.example.com", "api-gateway", "", 8080, "go", ""); err != nil {
		t.Fatalf("AddService(api-gateway): %v", err)
	}

	if err := store.MakeDomainFiles("blog.example.com"); err != nil {
		t.Fatalf("MakeDomainFiles(blog): %v", err)
	}
	if err := store.AddService("blog.example.com", "cms", "", 0, "php", ""); err != nil {
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

	if _, ok := byTarget["shop.example.com/webapp"]; !ok {
		t.Error("missing shop.example.com/webapp")
	}
	if _, ok := byTarget["shop.example.com/api-gateway"]; !ok {
		t.Error("missing shop.example.com/api-gateway")
	}
	cms, ok := byTarget["blog.example.com/cms"]
	if !ok {
		t.Fatal("missing blog.example.com/cms")
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
