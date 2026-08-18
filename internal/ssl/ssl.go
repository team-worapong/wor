// Package ssl implements `wor ssl`, porting commands/ssl.sh and
// lib/providers/ssl/{letsencrypt,self_signed,custom}.sh.
package ssl

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/osutil"
)

// State is wor's record of a host's installed certificate, stored as
// JSON at $WOR_HOME/ssl/hosts/<host>/ssl.json (the shell version used
// ssl.env; JSON keeps this package free of ad-hoc env-file parsing).
type State struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider"`
	CertFile  string `json:"certFile"`
	KeyFile   string `json:"keyFile"`
	AutoRenew string `json:"autoRenew"` // "enabled" | "disabled" | "unsupported"
	// ForceHTTPS records whether plain-HTTP requests to this host are
	// redirected to HTTPS. It is the operator's decision, resolved once
	// when the certificate is issued and stored -- never recomputed
	// from the provider or the hostname on later reads, or the
	// behaviour they chose could change by itself.
	//
	// A pointer so that "never recorded" is distinguishable from
	// "recorded as off". Files written before this field existed have
	// no value, and the two web servers did opposite things then:
	// apache redirected whenever a certificate existed, nginx never
	// did. Reading absent as a flat false would silently switch every
	// upgraded apache site back to serving plaintext on :80 -- an
	// upgrade quietly *removing* a redirect is the direction that
	// weakens a site, so absent means "whatever this provider did
	// before" instead. See ForceHTTPSOr.
	ForceHTTPS *bool `json:"forceHttps,omitempty"`
}

// ForceHTTPSOr resolves the redirect setting, falling back to
// legacyDefault when the state predates the field. Callers pass the
// behaviour their web server had before this setting existed, so an
// upgrade changes nothing until the operator says otherwise.
func (s State) ForceHTTPSOr(legacyDefault bool) bool {
	if s.ForceHTTPS == nil {
		return legacyDefault
	}
	return *s.ForceHTTPS
}

// Recorded reports whether the redirect setting was ever set
// explicitly, so callers can tell the operator that a value they are
// seeing is inherited rather than chosen.
func (s State) Recorded() bool { return s.ForceHTTPS != nil }

// SetForceHTTPS records an explicit decision.
func (s *State) SetForceHTTPS(v bool) { s.ForceHTTPS = &v }

func HostDir(sslRoot, host string) string   { return filepath.Join(sslRoot, "hosts", host) }
func stateFile(sslRoot, host string) string { return filepath.Join(HostDir(sslRoot, host), "ssl.json") }

// WriteState records st as host's certificate state. It takes the whole
// struct rather than a positional argument per field: there are enough
// of them now that a call site reads as a row of bare strings, and
// adding one silently shifts every caller's meaning.
func WriteState(sslRoot, host string, st State) error {
	dir := HostDir(sslRoot, host)
	if err := osutil.EnsureDir(dir); err != nil {
		return err
	}
	st.Enabled = true
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return osutil.WriteFileAtomic(stateFile(sslRoot, host), data, 0o600)
}

// LoadState returns (state, true, nil) if a certificate is on record
// for host, or (zero, false, nil) if none exists yet.
func LoadState(sslRoot, host string) (State, bool, error) {
	path := stateFile(sslRoot, host)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, false, nil
		}
		return State{}, false, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, false, err
	}
	return st, true, nil
}

func RemoveState(sslRoot, host string) error {
	return os.Remove(stateFile(sslRoot, host))
}

func RemoveHostDir(sslRoot, host string) error {
	return os.RemoveAll(HostDir(sslRoot, host))
}

// NormalizeProvider mirrors commands/ssl.sh ssl_provider_normalize().
func NormalizeProvider(v string) (string, error) {
	switch v {
	case "letsencrypt", "lets-encrypt":
		return "letsencrypt", nil
	case "self-signed", "selfsigned":
		return "self-signed", nil
	case "custom":
		return "custom", nil
	case "none", "":
		return "none", nil
	default:
		return "", fmt.Errorf("unsupported SSL provider: %s", v)
	}
}

func ProviderLabel(v string) string {
	switch v {
	case "letsencrypt":
		return "Let's Encrypt"
	case "self-signed":
		return "Self-signed"
	case "custom":
		return "Custom"
	case "none":
		return "None"
	default:
		return v
	}
}

// StatusInfo is the human-readable status shown by `wor ssl status`.
type StatusInfo struct {
	Enabled    bool
	Provider   string
	CertFile   string
	KeyFile    string
	AutoRenew  string
	Expiration string
}

func Status(sslRoot, host string) StatusInfo {
	st, ok, _ := LoadState(sslRoot, host)
	if !ok {
		return StatusInfo{Enabled: false, Provider: "none", Expiration: "unknown"}
	}
	info := StatusInfo{
		Enabled: true, Provider: st.Provider, CertFile: st.CertFile, KeyFile: st.KeyFile,
		AutoRenew: st.AutoRenew, Expiration: "unknown",
	}
	if st.CertFile != "" && osutil.Exists("openssl") {
		if info2, err := certExpiration(st.CertFile); err == nil {
			info.Expiration = info2
		}
	}
	return info
}

// certExpiration shells out to `openssl x509 -noout -enddate`, matching
// commands/ssl.sh ssl_status()'s expiration lookup.
func certExpiration(certFile string) (string, error) {
	out, err := exec.Command("openssl", "x509", "-in", certFile, "-noout", "-enddate").Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	return strings.TrimPrefix(line, "notAfter="), nil
}
