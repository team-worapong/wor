package cliapp

import (
	"encoding/json"
	"strings"

	"wor/internal/domainmodel"
)

type pm2ProcessInfo struct {
	Name string `json:"name"`
}

func parsePM2List(jlistOutput string) []string {
	var procs []pm2ProcessInfo
	if err := json.Unmarshal([]byte(jlistOutput), &procs); err != nil {
		return nil
	}
	var names []string
	for _, p := range procs {
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}
	return names
}

// worPM2Names returns every PM2 process name starting with "wor_",
// matching cmd_reset()'s cleanup filter.
func worPM2Names(jlistOutput string) []string {
	var out []string
	for _, name := range parsePM2List(jlistOutput) {
		if strings.HasPrefix(name, "wor_") {
			out = append(out, name)
		}
	}
	return out
}

// orphanPM2Names returns wor_-prefixed PM2 process names whose backing
// domain/service no longer exists in the registry, matching
// cmd_clean()'s orphan-process cleanup.
func orphanPM2Names(jlistOutput string, store *domainmodel.Store) []string {
	var out []string
	for _, name := range parsePM2List(jlistOutput) {
		if !strings.HasPrefix(name, "wor_") {
			continue
		}
		rest := strings.TrimPrefix(name, "wor_")
		idx := strings.LastIndex(rest, "_")
		if idx < 1 {
			continue
		}
		domain, service := rest[:idx], rest[idx+1:]
		if !store.ServiceExists(domain, service) {
			out = append(out, name)
		}
	}
	return out
}

// parseWorUnitName splits a "wor_<domain>_<service>.service" file name
// back into (domain, service, ok), the systemd equivalent of the
// PM2 name parsing above.
func parseWorUnitName(unitFile string) (domain, service string, ok bool) {
	rest := strings.TrimSuffix(strings.TrimPrefix(unitFile, "wor_"), ".service")
	idx := strings.LastIndex(rest, "_")
	if idx < 1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// orphanSystemdUnits returns wor_-prefixed unit file names whose
// backing domain/service no longer exists in the registry -- the
// systemd equivalent of orphanPM2Names.
func orphanSystemdUnits(unitFiles []string, store *domainmodel.Store) []string {
	var out []string
	for _, f := range unitFiles {
		domain, service, ok := parseWorUnitName(f)
		if !ok {
			continue
		}
		if !store.ServiceExists(domain, service) {
			out = append(out, f)
		}
	}
	return out
}
