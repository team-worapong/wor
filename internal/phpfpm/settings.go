// This file implements the per-service PHP settings file: an optional
// `.wor/php.ini` inside a service's own directory whose contents wor
// renders into that service's pool config as php_value /
// php_admin_value directives.
//
// Why a translation step instead of pointing php-fpm at the file: a
// php-fpm pool has no way to include a php.ini of its own. The only
// per-pool ini mechanism the SAPI offers is the php_value /
// php_admin_value family inside the pool's own *.conf, and the obvious
// alternative -- env[PHP_INI_SCAN_DIR] in the pool -- cannot work
// either, because the master parses php.ini once at startup and every
// worker is a fork of it, long before any pool's env is applied. So wor
// reads the file, validates it, and emits directives. The upside is
// that WritePool already runs `php-fpm -t` and rolls the file back on
// failure, so a bad setting can never take the shared master's other
// pools down with it.
//
// Why an allowlist rather than passing keys through: `.wor/php.ini`
// lives in the service tree, which the deploy account writes, while the
// pool file it feeds is written by root. Two things follow. First, the
// renderer must never treat the file as text to paste -- a value
// carrying a newline would otherwise inject arbitrary pool directives
// (`user = root`), so parsing is strictly line/key/value and every
// value is checked for control characters. Second, a handful of ini
// keys reach beyond the pool's own workers -- error_log is opened by
// the root master, extension/sendmail_path name programs to run -- and
// those stay out of the allowlist entirely (see rejectedSettings, which
// exists to say *why* rather than let them fall into the generic
// unknown-key error). Everything left in allowedSettings only affects
// the requesting service's own workers, which is exactly the blast
// radius the file's author already has through their own PHP code.
package phpfpm

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// SettingsFileName is the file, inside a service's own `.wor`
// directory, that holds its PHP ini overrides. Callers build the full
// path (the `.wor` directory's location is a WOR_HOME layout detail
// this package deliberately doesn't know) and hand it to LoadSettings.
const SettingsFileName = "php.ini"

// maxSettingValueLen bounds one value's length. No legitimate ini value
// wor accepts is anywhere near this long; the cap exists so a runaway
// or hostile file cannot bloat the pool config.
const maxSettingValueLen = 256

// Setting is one validated ini override, in the order it appeared in
// the file. admin is unexported on purpose: whether a key needs
// php_admin_value rather than php_value is a property of the key
// itself, decided by allowedSettings, and must never be something a
// caller (or a settings file) can choose.
type Setting struct {
	Key   string
	Value string
	admin bool
}

// allowedSettings maps each accepted ini key to whether it must be
// emitted as php_admin_value instead of php_value.
//
// php_value is the default because it reproduces php.ini semantics: the
// application can still ini_set() past it at runtime, which plenty of
// PHP code legitimately does (raising memory_limit for one import job,
// say). There is no security argument for php_admin_value here anyway --
// whoever writes `.wor/php.ini` also writes the PHP that runs in the
// pool, so admin-locking a value protects nothing.
//
// The exceptions are keys PHP classifies as PHP_INI_SYSTEM: php_value
// cannot set those at all, and php-fpm applies no such setting rather
// than reporting an error, so using php_value for them would look like
// it worked and silently do nothing.
var allowedSettings = map[string]bool{
	// php_value (PHP_INI_ALL / PHP_INI_PERDIR)
	"memory_limit":            false,
	"max_execution_time":      false,
	"max_input_time":          false,
	"max_input_vars":          false,
	"upload_max_filesize":     false,
	"post_max_size":           false,
	"default_socket_timeout":  false,
	"date.timezone":           false,
	"error_reporting":         false,
	"display_errors":          false,
	"log_errors":              false,
	"output_buffering":        false,
	"zlib.output_compression": false,
	"session.gc_maxlifetime":  false,
	"session.cookie_lifetime": false,

	// php_admin_value (PHP_INI_SYSTEM -- php_value would be ignored)
	"max_file_uploads": true,
	"expose_php":       true,
}

// rejectedSettings names keys that are deliberately NOT accepted, with
// the reason shown to whoever tried to use one. Separate from the
// generic unknown-key error because "wor will not do this, and here is
// why" is a different message from "wor does not know this key yet" --
// the first is a decision, the second is a gap that can be filled by
// adding to allowedSettings.
var rejectedSettings = map[string]string{
	"error_log":         "the php-fpm master opens a pool's error_log as root, so a service-owned file could point a privileged write anywhere on the host",
	"extension":         "loading a shared object is a way to run arbitrary native code; install extensions through the system's PHP packages instead",
	"zend_extension":    "loading a shared object is a way to run arbitrary native code; install extensions through the system's PHP packages instead",
	"sendmail_path":     "this names a program php runs on the service's behalf; configure mail transport on the host instead",
	"open_basedir":      "this is a host-level containment setting, and a service must not be able to widen the one it runs under",
	"disable_functions": "this is a host-level containment setting, and a service must not be able to shorten the list it runs under",
	"upload_tmp_dir":    "this points php's uploads at a directory outside the service tree, which wor cannot reason about",
}

// rejectedSettingPrefixes covers whole families the same way
// rejectedSettings covers single keys.
var rejectedSettingPrefixes = map[string]string{
	"opcache.": "opcache's shared memory is allocated once by the php-fpm master, so a per-pool opcache setting is ignored rather than applied -- change it in the version's own php.ini",
}

// settingKeyRe is what an ini key may look like: lowercase letters,
// digits, underscores and dots (`session.gc_maxlifetime`), starting
// with a letter, digit or underscore. Anything else -- brackets,
// spaces, quotes -- is rejected before the allowlist is even consulted.
var settingKeyRe = regexp.MustCompile(`^[a-z0-9_][a-z0-9_.]*$`)

// LoadSettings reads and validates the settings file at path. A missing
// file is not an error and yields no settings -- the file is optional,
// and every service that never creates one keeps exactly the pool
// config wor generated for it.
func LoadSettings(path string) ([]Setting, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settings, err := ParseSettings(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return settings, nil
}

// ParseSettings validates one settings file's contents, returning its
// settings in file order. Split from LoadSettings so the rules can be
// tested directly against the bytes, with no file involved.
//
// Every rejection is an error rather than a skipped line: a setting
// that was asked for and silently not applied is the failure mode this
// whole feature exists to avoid, so wor refuses the file as a whole and
// names the line at fault.
func ParseSettings(data []byte) ([]Setting, error) {
	pairs, err := parseKeyValueFile(data, func(key string, lineNo int) error {
		_, err := classifySettingKey(key, lineNo)
		return err
	})
	if err != nil {
		return nil, err
	}
	settings := make([]Setting, 0, len(pairs))
	for _, p := range pairs {
		// The error was already surfaced by the callback above, so this
		// second call only fetches the directive the key needs.
		admin, _ := classifySettingKey(p.Key, p.LineNo)
		settings = append(settings, Setting{Key: p.Key, Value: p.Value, admin: admin})
	}
	return settings, nil
}

// keyValue is one accepted line of a settings file.
type keyValue struct {
	Key    string
	Value  string
	LineNo int
}

// parseKeyValueFile is the scanner both settings files share: the same
// comment rules, the same "no [sections]", the same key-shape and
// duplicate checks, and the same value cleaning. Only which keys are
// allowed differs, which is what allow reports -- so php.ini and
// php-fpm.ini can never drift apart on anything except their
// allowlists, and in particular can never differ on the control
// character check that keeps a value from injecting a pool directive.
func parseKeyValueFile(data []byte, allow func(key string, lineNo int) error) ([]keyValue, error) {
	var pairs []keyValue
	seen := map[string]int{}

	for i, raw := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("line %d: section headers are not supported -- this file configures one service's pool, so every line is a plain `key = value`", lineNo)
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected `key = value`, got %q", lineNo, line)
		}

		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		value := strings.TrimSpace(line[eq+1:])
		if !settingKeyRe.MatchString(key) {
			return nil, fmt.Errorf("line %d: %q is not a valid setting name", lineNo, key)
		}
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: %s is already set on line %d -- set it once", lineNo, key, prev)
		}
		if err := allow(key, lineNo); err != nil {
			return nil, err
		}
		value, err := cleanSettingValue(key, value, lineNo)
		if err != nil {
			return nil, err
		}

		seen[key] = lineNo
		pairs = append(pairs, keyValue{Key: key, Value: value, LineNo: lineNo})
	}
	return pairs, nil
}

// classifySettingKey decides whether key is allowed and, if so, which
// directive carries it.
func classifySettingKey(key string, lineNo int) (admin bool, err error) {
	if admin, ok := allowedSettings[key]; ok {
		return admin, nil
	}
	if reason, ok := rejectedSettings[key]; ok {
		return false, fmt.Errorf("line %d: %s cannot be set per service: %s", lineNo, key, reason)
	}
	for prefix, reason := range rejectedSettingPrefixes {
		if strings.HasPrefix(key, prefix) {
			return false, fmt.Errorf("line %d: %s cannot be set per service: %s", lineNo, key, reason)
		}
	}
	return false, fmt.Errorf("line %d: %s is not one of the settings wor applies per service. Supported: %s", lineNo, key, strings.Join(AllowedSettingKeys(), ", "))
}

// cleanSettingValue strips one optional layer of surrounding double
// quotes and rejects anything that has no business in a pool config
// file. The control-character check is the load-bearing one: the pool
// file is line-oriented, so a value carrying a newline would end the
// php_value directive and let the rest of the line become a pool
// directive of the author's choosing.
func cleanSettingValue(key, value string, lineNo int) (string, error) {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}
	if value == "" {
		return "", fmt.Errorf("line %d: %s has no value -- remove the line instead of leaving it empty", lineNo, key)
	}
	if len(value) > maxSettingValueLen {
		return "", fmt.Errorf("line %d: %s's value is longer than %d characters", lineNo, key, maxSettingValueLen)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("line %d: %s's value contains a control character", lineNo, key)
		}
	}
	return value, nil
}

// AllowedSettingKeys lists every accepted key, sorted, for error
// messages and for the reference file wor writes next to php.ini.
func AllowedSettingKeys() []string {
	keys := make([]string, 0, len(allowedSettings))
	for key := range allowedSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SettingLines renders settings as the pool directives they become,
// one per line and in file order. Exported because it is also the
// yardstick for comparison: `wor diagnose` reads the directives a live
// pool file actually carries (PoolSettingLines) and checks them against
// what the service's current php.ini would produce, which only works if
// both sides are the same strings produced by the same function.
func SettingLines(settings []Setting) []string {
	lines := make([]string, 0, len(settings))
	for _, s := range settings {
		directive := "php_value"
		if s.admin {
			directive = "php_admin_value"
		}
		lines = append(lines, fmt.Sprintf("%s[%s] = %s", directive, s.Key, s.Value))
	}
	return lines
}

// renderSettings turns validated settings into the block that goes at
// the end of a pool file. Returns "" for no settings, so a service
// without a php.ini gets byte-for-byte the pool file it got before this
// feature existed.
func renderSettings(settings []Setting) string {
	if len(settings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n; from " + SettingsFileName + " in this service's .wor directory\n")
	for _, line := range SettingLines(settings) {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// PoolUpToDate reports whether the pool config file on disk already
// contains exactly what p renders to.
//
// Two callers, one comparison. `wor deploy` uses it to skip a pool
// write that would change nothing -- writing anyway means a privileged
// write plus `php-fpm -t` plus a reload of the shared master, which
// cycles the workers of every OTHER service under it, on every deploy
// of any pooled php service. And `wor info`/`wor diagnose`/`wor health`
// use it to report drift: a pool that no longer matches the service's
// own `.wor` files because nobody applied them.
//
// Comparing the whole rendered file, rather than just the settings
// lines, is what makes it a real answer -- it covers php.ini, the pool
// tuning in php-fpm.ini, and a pool file edited by hand, all at once.
//
// readable is false when the file cannot be read, which is a real
// possibility rather than an error: the read-only inspections must
// never prompt for elevation, and callers must not report an unreadable
// pool as drifted. A missing file also reports readable=false; that is
// the "no pool at all" case, which every caller handles separately.
func PoolUpToDate(p Pool) (upToDate, readable bool) {
	data, err := os.ReadFile(PoolFilePath(p.Version, p.Domain, p.Service))
	if err != nil {
		return false, false
	}
	return string(data) == PoolFileContent(p), true
}
