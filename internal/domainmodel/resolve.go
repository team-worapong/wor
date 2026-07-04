package domainmodel

import (
	"fmt"
	"regexp"
	"strings"
)

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// SlugOK mirrors lib/validation.sh slug_ok().
func SlugOK(s string) bool { return slugRe.MatchString(s) }

// RequireSlug validates a domain-id/service-name slug.
func RequireSlug(s string) error {
	if !SlugOK(s) {
		return fmt.Errorf("invalid slug '%s'. Use lowercase a-z, 0-9 and hyphen only", s)
	}
	return nil
}

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
var digitsOnlyRe = regexp.MustCompile(`^[0-9]+$`)
var ipv4Re = regexp.MustCompile(`^[0-9]+(\.[0-9]+){3}$`)

// ValidateDomainName ports lib/validation.sh validate_domain_name() +
// reject_localhost_and_ip().
func ValidateDomainName(host string) error {
	if host == "" {
		return fmt.Errorf("domain name is required")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" {
		return fmt.Errorf("unsupported domain name: %s. Use a local domain such as app.test or a public domain such as app.example.com", host)
	}
	if ipv4Re.MatchString(lower) {
		return fmt.Errorf("IP addresses are not supported as WOR host names: %s", host)
	}
	if strings.Contains(lower, ":") {
		return fmt.Errorf("IP addresses are not supported as WOR host names: %s", host)
	}
	if len(lower) > 253 {
		return fmt.Errorf("domain name is too long: %s", host)
	}
	if !strings.Contains(lower, ".") {
		return fmt.Errorf("invalid domain name: %s. Use a local domain such as app.test or a public domain such as app.example.com", host)
	}
	if strings.HasPrefix(lower, ".") || strings.HasSuffix(lower, ".") {
		return fmt.Errorf("invalid domain name: %s", host)
	}
	labels := strings.Split(lower, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("invalid domain name: %s", host)
		}
		if !labelRe.MatchString(label) {
			return fmt.Errorf("invalid domain name: %s", host)
		}
	}
	if digitsOnlyRe.MatchString(labels[len(labels)-1]) {
		return fmt.Errorf("invalid domain name: %s", host)
	}
	return nil
}

// ParseTarget splits a "<domain>[/<service>]" target string, mirroring
// lib/validation.sh parse_target().
func ParseTarget(target string) (domain, service string, err error) {
	if target == "" {
		return "", "", fmt.Errorf("target is required")
	}
	if idx := strings.Index(target, "/"); idx >= 0 {
		domain = target[:idx]
		service = target[idx+1:]
	} else {
		domain = target
	}
	if err := RequireSlug(domain); err != nil {
		return "", "", err
	}
	if service != "" {
		if err := RequireSlug(service); err != nil {
			return "", "", err
		}
	}
	return domain, service, nil
}

// coTLDSuffixes are second-level ccTLD suffixes that need three labels
// to form a registrable base (matches host_base_parts()/domain_id_from_base()
// in lib/hosts.sh).
var coTLDSuffixes = map[string]bool{
	"co.th": true, "ac.th": true, "go.th": true, "or.th": true,
	"co.uk": true, "com.au": true, "co.jp": true,
}

// HostBaseParts splits a FQDN into its "service" label (subdomain
// prefix, or "www" if none) and its registrable base domain. Port of
// lib/hosts.sh host_base_parts().
func HostBaseParts(host string) (service, base string, err error) {
	parts := strings.Split(host, ".")
	n := len(parts)
	if n < 2 {
		return "", "", fmt.Errorf("cannot resolve domain from host: %s", host)
	}
	baseLabels := 2
	if n >= 3 {
		last2 := parts[n-2] + "." + parts[n-1]
		if coTLDSuffixes[last2] {
			baseLabels = 3
		}
	}
	service = "www"
	if n > baseLabels {
		service = parts[0]
	}
	start := n - baseLabels
	base = strings.Join(parts[start:], ".")
	return service, base, nil
}

// DomainIDFromBase derives the WOR domain id from a registrable base
// domain, e.g. "example.com" -> "com-example",
// "example.co.th" -> "th-co-example". Port of
// lib/hosts.sh domain_id_from_base().
func DomainIDFromBase(base string) (string, error) {
	parts := strings.Split(base, ".")
	n := len(parts)
	if n >= 3 {
		last2 := parts[n-2] + "." + parts[n-1]
		if coTLDSuffixes[last2] {
			return parts[n-1] + "-" + parts[n-2] + "-" + parts[n-3], nil
		}
	}
	if n < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("cannot resolve base domain from: %s", base)
	}
	return parts[1] + "-" + parts[0], nil
}

// HostToDomainService resolves a FQDN into (domain, service), honoring
// an explicit domain override. Port of lib/hosts.sh host_to_domain_service().
func HostToDomainService(host, override string) (domain, service string, err error) {
	service, base, err := HostBaseParts(host)
	if err != nil {
		return "", "", err
	}
	if override != "" {
		return override, service, nil
	}
	domain, err = DomainIDFromBase(base)
	if err != nil {
		return "", "", err
	}
	return domain, service, nil
}

// RootDomainAliasHost returns the "www." alias for an apex host, e.g.
// example.com -> www.example.com.
func RootDomainAliasHost(host string) string { return "www." + host }

// SuggestDomainTypeForHost mirrors lib/hosts.sh suggest_domain_type_for_host().
func SuggestDomainTypeForHost(host string) string {
	lower := strings.ToLower(host)
	if strings.HasSuffix(lower, ".test") || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") {
		return "local"
	}
	return "public"
}

// NormalizeDomainType mirrors lib/hosts.sh normalize_domain_type().
func NormalizeDomainType(v string) (string, error) {
	switch v {
	case "local", "Local", "LOCAL", "development", "dev":
		return "local", nil
	case "public", "Public", "PUBLIC":
		return "public", nil
	case "":
		return "", nil
	default:
		return "", fmt.Errorf("invalid domain type: %s. Use local or public", v)
	}
}
