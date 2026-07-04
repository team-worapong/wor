package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/team-worapong/wor/internal/config"
)

func TestDomainIDRules(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"example.com":   "com-example",
		"example.co.th": "th-co-example",
		"mooda.co.uk":   "uk-co-mooda",
	}
	for input, want := range tests {
		got, err := ID(input)
		if err != nil {
			t.Fatalf("ID(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ID(%q) = %q", input, got)
		}
	}
}

func TestAddCreatesDomainDirectoryAndMetadata(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manager := NewManager(config.Config{WORHome: home})

	metadata, err := manager.Add(AddRequest{Domain: "example.co.th"})
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}

	wantPath := filepath.Join(home, "domains", "th-co-example")
	if metadata.DomainID != "th-co-example" {
		t.Fatalf("DomainID = %q", metadata.DomainID)
	}
	if metadata.DomainName != "example.co.th" {
		t.Fatalf("DomainName = %q", metadata.DomainName)
	}
	if metadata.DomainPath != wantPath {
		t.Fatalf("DomainPath = %q", metadata.DomainPath)
	}
	if _, err := os.Stat(filepath.Join(wantPath, MetadataFileName)); err != nil {
		t.Fatalf("domain metadata not written: %v", err)
	}

	var stored Metadata
	data, err := os.ReadFile(filepath.Join(wantPath, MetadataFileName))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if stored.DomainID != metadata.DomainID || stored.DomainName != metadata.DomainName {
		t.Fatalf("stored metadata = %#v", stored)
	}
}

func TestAddExistingDomainIsIdempotent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manager := NewManager(config.Config{WORHome: home})

	first, err := manager.Add(AddRequest{Domain: "example.com"})
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	second, err := manager.Add(AddRequest{Domain: "example.com"})
	if err != nil {
		t.Fatalf("add existing domain: %v", err)
	}

	if !second.Existing {
		t.Fatal("Existing = false")
	}
	if second.DomainID != first.DomainID {
		t.Fatalf("DomainID = %q", second.DomainID)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("CreatedAt changed from %q to %q", first.CreatedAt, second.CreatedAt)
	}
}

func TestCatalogFindsLongestMatchingDomainFromMetadata(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manager := NewManager(config.Config{WORHome: home})
	if _, err := manager.Add(AddRequest{Domain: "example.com"}); err != nil {
		t.Fatalf("add example.com: %v", err)
	}
	if _, err := manager.Add(AddRequest{Domain: "app.example.com"}); err != nil {
		t.Fatalf("add app.example.com: %v", err)
	}

	metadata, ok, err := NewCatalog(config.Config{WORHome: home}).FindLongestMatch("api.app.example.com")
	if err != nil {
		t.Fatalf("find match: %v", err)
	}
	if !ok {
		t.Fatal("expected domain match")
	}
	if metadata.DomainName != "app.example.com" {
		t.Fatalf("DomainName = %q", metadata.DomainName)
	}
}
