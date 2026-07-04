package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/team-worapong/wor/internal/config"
)

const MetadataFileName = "domain.json"

type AddRequest struct {
	Domain string
}

type Metadata struct {
	DomainID   string `json:"domain_id"`
	DomainName string `json:"domain_name"`
	DomainPath string `json:"domain_path"`
	CreatedAt  string `json:"created_at"`
}

type Manager struct {
	layout config.WORHomeLayout
	now    func() time.Time
}

func NewManager(cfg config.Config) Manager {
	return Manager{
		layout: config.Layout(cfg),
		now:    time.Now,
	}
}

func (m Manager) Add(request AddRequest) (Metadata, error) {
	domainName, labels, err := Normalize(request.Domain)
	if err != nil {
		return Metadata{}, err
	}

	domainID := IDFromLabels(labels)
	domainPath := filepath.Join(m.layout.Domains, domainID)
	metadata := Metadata{
		DomainID:   domainID,
		DomainName: domainName,
		DomainPath: domainPath,
		CreatedAt:  m.now().UTC().Format(time.RFC3339),
	}

	if err := os.MkdirAll(domainPath, 0o755); err != nil {
		return Metadata{}, fmt.Errorf("create domain directory: %w", err)
	}
	if err := writeMetadata(filepath.Join(domainPath, MetadataFileName), metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func Normalize(value string) (string, []string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return "", nil, errors.New("domain is required")
	}

	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", nil, fmt.Errorf("domain %q must contain at least two labels", value)
	}
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return "", nil, fmt.Errorf("domain %q: %w", value, err)
		}
	}
	return value, labels, nil
}

func ID(domainName string) (string, error) {
	_, labels, err := Normalize(domainName)
	if err != nil {
		return "", err
	}
	return IDFromLabels(labels), nil
}

func IDFromLabels(labels []string) string {
	reversed := make([]string, 0, len(labels))
	for index := len(labels) - 1; index >= 0; index-- {
		reversed = append(reversed, labels[index])
	}
	return strings.Join(reversed, "-")
}

func validateLabel(label string) error {
	if label == "" {
		return errors.New("labels must not be empty")
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return fmt.Errorf("label %q must not start or end with '-'", label)
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return fmt.Errorf("label %q contains unsupported character %q", label, r)
	}
	return nil
}

func writeMetadata(path string, metadata Metadata) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("domain %q already exists", metadata.DomainName)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect domain metadata: %w", err)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode domain metadata: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write domain metadata: %w", err)
	}
	return nil
}

type Catalog struct {
	domainsDir string
}

func NewCatalog(cfg config.Config) Catalog {
	return Catalog{
		domainsDir: config.Layout(cfg).Domains,
	}
}

func (c Catalog) List() ([]Metadata, error) {
	entries, err := os.ReadDir(c.domainsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read domain catalog: %w", err)
	}

	domains := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, err := ReadMetadata(filepath.Join(c.domainsDir, entry.Name(), MetadataFileName))
		if err != nil {
			return nil, err
		}
		domains = append(domains, metadata)
	}

	sort.Slice(domains, func(i, j int) bool {
		leftLabels := labelCount(domains[i].DomainName)
		rightLabels := labelCount(domains[j].DomainName)
		if leftLabels == rightLabels {
			return domains[i].DomainName < domains[j].DomainName
		}
		return leftLabels > rightLabels
	})
	return domains, nil
}

func (c Catalog) FindLongestMatch(fqdn string) (Metadata, bool, error) {
	fqdn, _, err := Normalize(fqdn)
	if err != nil {
		return Metadata{}, false, err
	}

	domains, err := c.List()
	if err != nil {
		return Metadata{}, false, err
	}
	for _, metadata := range domains {
		if fqdn == metadata.DomainName || strings.HasSuffix(fqdn, "."+metadata.DomainName) {
			return metadata, true, nil
		}
	}
	return Metadata{}, false, nil
}

func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("read domain metadata %s: %w", path, err)
	}

	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse domain metadata %s: %w", path, err)
	}
	if strings.TrimSpace(metadata.DomainID) == "" || strings.TrimSpace(metadata.DomainName) == "" {
		return Metadata{}, fmt.Errorf("domain metadata %s is incomplete", path)
	}
	return metadata, nil
}

func labelCount(value string) int {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	return len(strings.Split(value, "."))
}
