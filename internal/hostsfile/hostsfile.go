// Package hostsfile manages the WOR-owned block inside the OS hosts
// file (/etc/hosts on Unix, C:\Windows\System32\drivers\etc\hosts on
// Windows), porting lib/hosts.sh's update_wor_hosts_block() /
// remove_from_wor_hosts_block() / local_hosts_entry_exists(). Writing
// to the system hosts file requires elevated privileges on every OS;
// callers should check osutil.IsElevated() and warn/prompt accordingly
// before calling Add/Remove.
package hostsfile

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"wor/internal/osutil"
)

// blockStart/blockEnd are deliberately independent of
// internal/version.ProductName -- they're a structural marker written
// into the user's real system hosts file, not a display string, so
// renaming the product later must not silently orphan (or duplicate)
// blocks a previous build already wrote.
const blockStart = "# >>> WOR-HOSTS >>>"
const blockEnd = "# <<< WOR-HOSTS <<<"

// Path returns the OS hosts file location, honoring WOR_HOSTS_FILE for
// tests/overrides the same way the shell version's hosts_file_path()
// did via WOR_HOSTS_FILE.
func Path() string {
	if p := os.Getenv("WOR_HOSTS_FILE"); p != "" {
		return p
	}
	if osutil.IsWindows() {
		sysRoot := os.Getenv("SystemRoot")
		if sysRoot == "" {
			sysRoot = `C:\Windows`
		}
		return sysRoot + `\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

var blockRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(blockStart) + `\n(.*?)\n?` + regexp.QuoteMeta(blockEnd) + `\n?`)

// EntryExists reports whether host already has a 127.0.0.1 entry
// anywhere in the hosts file (not just inside the WOR block), matching
// local_hosts_entry_exists().
func EntryExists(host string) (bool, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		for _, f := range fields[1:] {
			if f == host {
				return true, nil
			}
		}
	}
	return false, nil
}

func parseBlockHosts(text string) (map[string]bool, string) {
	hosts := map[string]bool{}
	match := blockRe.FindStringSubmatch(text)
	if match == nil {
		return hosts, text
	}
	for _, line := range strings.Split(match[1], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 && parts[0] == "127.0.0.1" {
			for _, name := range parts[1:] {
				hosts[name] = true
			}
		}
	}
	rest := blockRe.ReplaceAllString(text, "")
	return hosts, rest
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Add inserts host into the WOR-managed hosts block, creating the block
// if needed. Requires write access to the hosts file (root/sudo on
// Unix, Administrator on Windows).
func Add(host string) error {
	data, _ := os.ReadFile(Path())
	text := string(data)
	hosts, rest := parseBlockHosts(text)
	hosts[host] = true

	var b strings.Builder
	b.WriteString(blockStart + "\n")
	for _, h := range sortedKeys(hosts) {
		b.WriteString("127.0.0.1 " + h + "\n")
	}
	b.WriteString(blockEnd + "\n")

	rest = strings.TrimRight(rest, "\n \t\r")
	var out string
	if rest != "" {
		out = rest + "\n\n" + b.String()
	} else {
		out = b.String()
	}
	return osutil.WriteFilePrivileged(Path(), []byte(out))
}

// Remove deletes host from the WOR-managed block (removing the block
// entirely if it becomes empty), leaving everything else in the hosts
// file untouched.
func Remove(host string) error {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := string(data)
	if !strings.Contains(text, blockStart) {
		return nil
	}
	hosts, _ := parseBlockHosts(text)
	delete(hosts, host)

	var replacement string
	if len(hosts) > 0 {
		var b strings.Builder
		b.WriteString(blockStart + "\n")
		for _, h := range sortedKeys(hosts) {
			b.WriteString("127.0.0.1 " + h + "\n")
		}
		b.WriteString(blockEnd + "\n")
		replacement = b.String()
	}

	match := blockRe.FindStringIndex(text)
	var out string
	if match == nil {
		out = text
	} else {
		out = text[:match[0]] + replacement + text[match[1]:]
	}
	// Collapse 3+ consecutive newlines down to a single blank line.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return osutil.WriteFilePrivileged(Path(), []byte(out))
}

// ListHosts returns every host currently registered in the WOR-managed
// hosts block (sorted), for orphan detection -- it makes no claim about
// whether a host is still referenced by any domain/service; callers
// (e.g. `wor clean`) cross-reference against the registry themselves.
func ListHosts() ([]string, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	hosts, _ := parseBlockHosts(string(data))
	return sortedKeys(hosts), nil
}

// RemoveAll strips the entire WOR-managed hosts block (every host in
// it, in one shot), leaving everything else in the hosts file
// untouched. Used by `wor reset`, which wipes every domain/service wor
// knows about -- removing hosts one at a time via Remove would get the
// same end result but is needlessly roundabout when the goal is
// "every WOR host entry, gone".
func RemoveAll() error {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := string(data)
	if !strings.Contains(text, blockStart) {
		return nil
	}
	out := blockRe.ReplaceAllString(text, "")
	// Collapse 3+ consecutive newlines down to a single blank line.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return osutil.WriteFilePrivileged(Path(), []byte(out))
}
