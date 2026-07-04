package domainmodel

import (
	_ "embed"
	"strings"
)

//go:embed assets/public_suffix_list.dat
var embeddedPSL string

// pslRule is one parsed line of the public suffix list.
type pslRule struct {
	labels    []string // lowercased, in the file's left-to-right order
	exception bool
	wildcard  bool
}

func parsePSL(data string) []pslRule {
	var rules []pslRule
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		exception := strings.HasPrefix(line, "!")
		body := line
		if exception {
			body = line[1:]
		}
		labels := strings.Split(strings.ToLower(body), ".")
		rules = append(rules, pslRule{
			labels:    labels,
			exception: exception,
			wildcard:  len(labels) > 0 && labels[0] == "*",
		})
	}
	return rules
}

// IsApexDomain reports whether host is a registrable "apex"/root domain
// (e.g. example.com, example.co.th) rather than a subdomain (e.g.
// app.example.com). Port of lib/validation.sh is_apex_domain(), which
// shelled out to Node; here the same public-suffix algorithm runs
// natively against the embedded list.
func IsApexDomain(host string) bool {
	if err := ValidateDomainName(host); err != nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	labels := strings.Split(lower, ".")
	rules := parsePSL(embeddedPSL)

	var best []string
	bestException := false
	found := false

	for _, r := range rules {
		matched := false
		if r.wildcard {
			if len(labels) >= len(r.labels) {
				suffix := strings.Join(labels[len(labels)-len(r.labels)+1:], ".")
				matched = suffix == strings.Join(r.labels[1:], ".")
			}
		} else {
			if len(labels) >= len(r.labels) {
				matched = strings.Join(labels[len(labels)-len(r.labels):], ".") == strings.Join(r.labels, ".")
			}
		}
		if matched && (!found || len(r.labels) > len(best)) {
			best = r.labels
			bestException = r.exception
			found = true
		}
	}

	suffixLength := 1
	if found {
		if bestException {
			suffixLength = len(best) - 1
		} else {
			suffixLength = len(best)
		}
	}
	registrableLength := suffixLength + 1
	return len(labels) == registrableLength
}
