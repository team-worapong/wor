package cliapp

import "strings"

// flags is a tiny --key=value / --flag argument parser shared by every
// subcommand, replacing the shell version's repeated
// `for arg in "$@"; do case "$arg" in --x=*) ...;; esac; done` blocks.
type flags struct {
	values map[string]string
	set    map[string]bool
}

func parseFlags(args []string) flags {
	f := flags{values: map[string]string{}, set: map[string]bool{}}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		body := strings.TrimPrefix(arg, "--")
		if idx := strings.Index(body, "="); idx >= 0 {
			f.values[body[:idx]] = body[idx+1:]
		} else {
			f.set[body] = true
		}
	}
	return f
}

func (f flags) Get(key, def string) string {
	if v, ok := f.values[key]; ok {
		return v
	}
	return def
}

func (f flags) Has(key string) bool { return f.set[key] }

// Any reports whether at least one --flag or --key=value was parsed.
// Used by `wor create` to reject every -- flag: the command is
// interactive-only by design, so any flag at all (not just a specific
// disallowed set) is a usage error pointing at the automation surface
// (`wor domain/service/host add`) instead.
func (f flags) Any() bool { return len(f.values) > 0 || len(f.set) > 0 }
