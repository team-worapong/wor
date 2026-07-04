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
}

func HostDir(sslRoot, host string) string       { return filepath.Join(sslRoot, "hosts", host) }
func stateFile(sslRoot, host string) string     { return filepath.Join(HostDir(sslRoot, host), "ssl.json") }

func WriteState(sslRoot, host, provider, cert, key, autoRenew string) error {
	dir := HostDir(sslRoot, host)
	if err := osutil.EnsureDir(dir); err != nil {
		return err
	}
	st := State{Enabled: true, Provider: provider, CertFile: cert, KeyFile: key, AutoRenew: autoRenew}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile(sslRoot, host), data, 0o600)
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
