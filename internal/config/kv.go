package config

import (
	"bufio"
	"os"
	"strings"
)

// ParseKV reads a simple `key = value` file (comments start with `#`,
// blank lines ignored, values may be wrapped in single or double
// quotes) used by both ~/.wor/config and $WOR_HOME/configs/host.env.
// Returns an empty map (not an error) if the file does not exist.
func ParseKV(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := stripSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = unquote(val)
		if key == "" {
			continue
		}
		out[key] = val
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func stripSpace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// lookup returns the first non-empty value found for any of the given
// aliases (case-sensitive, matching the shell version's explicit
// `key|KEY` alternatives).
func lookup(m map[string]string, aliases ...string) (string, bool) {
	for _, a := range aliases {
		if v, ok := m[a]; ok && v != "" {
			return v, true
		}
	}
	return "", false
}
