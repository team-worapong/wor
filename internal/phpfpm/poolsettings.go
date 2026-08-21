// This file implements the second per-service settings file:
// `.wor/php-fpm.ini`, which tunes the pool's process manager.
//
// Why a second file rather than a section in php.ini: these are not the
// same kind of setting, and php-fpm does not treat them as such.
// `memory_limit` is a PHP ini setting, carried into the pool as
// `php_value[memory_limit]` and overridable by the application at
// runtime. `pm.max_children` is a php-fpm pool directive: it is written
// bare, the application cannot see or change it, and it governs the
// worker processes rather than anything inside them. Folding both into
// one file would mean one line silently meaning php_value and the next
// meaning a raw directive, decided by a lookup table the reader cannot
// see. Two files, each with one meaning, is the honest shape -- and it
// keeps php.ini's "no [sections]" rule, which would otherwise have to
// be reversed.
//
// What this file must never allow is anything that defines the pool's
// identity: user, group, listen and the socket ownership are wor's, and
// a service able to set them could take over another service's socket
// or run its workers as another account. Those keys are named in
// rejectedPoolSettings with the reason, rather than left to the generic
// unknown-key error.
package phpfpm

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// PoolSettingsFileName is the file, inside a service's own `.wor`
// directory, that tunes its pool's process manager.
const PoolSettingsFileName = "php-fpm.ini"

// PoolSetting is one validated pool directive, in the order it appeared
// in the file.
type PoolSetting struct {
	Key   string
	Value string
}

// DefaultMaxChildren is the pm.max_children a pool gets when its
// php-fpm.ini does not say otherwise.
const DefaultMaxChildren = 5

// defaultPoolSettings is the process-manager block wor generates on its
// own. Kept as data rather than a format string because php-fpm.ini
// overrides it by key: a value the service sets replaces the default in
// place instead of being appended after it, so the pool file never
// carries the same directive twice with different values.
var defaultPoolSettings = map[string]string{
	"pm":                      "dynamic",
	"pm.max_children":         strconv.Itoa(DefaultMaxChildren),
	"pm.start_servers":        "2",
	"pm.min_spare_servers":    "1",
	"pm.max_spare_servers":    "3",
	"pm.process_idle_timeout": "10s",
}

// poolSettingOrder is the order directives are emitted in, per process
// manager mode. Only the directives a mode actually uses are listed:
// php-fpm tolerates `pm.start_servers` under `pm = static` (it ignores
// them), but a pool file that lists settings the running mode does not
// use misleads whoever reads it next.
var poolSettingOrder = map[string][]string{
	"dynamic":  {"pm", "pm.max_children", "pm.start_servers", "pm.min_spare_servers", "pm.max_spare_servers"},
	"static":   {"pm", "pm.max_children"},
	"ondemand": {"pm", "pm.max_children", "pm.process_idle_timeout"},
}

// allowedPoolSettings is every pool directive a service may set for
// itself. All of them govern only this pool's own worker processes,
// which is the same blast radius the service's own code already has.
var allowedPoolSettings = map[string]bool{
	"pm":                        true,
	"pm.max_children":           true,
	"pm.start_servers":          true,
	"pm.min_spare_servers":      true,
	"pm.max_spare_servers":      true,
	"pm.max_requests":           true,
	"pm.process_idle_timeout":   true,
	"request_terminate_timeout": true,
	"rlimit_files":              true,
}

// rejectedPoolSettings names directives that are deliberately NOT
// accepted, with the reason. The first group is the pool's identity,
// which wor derives and a service must not be able to redefine; the
// rest are directives that make the root master open a path or run a
// program on the service's behalf.
var rejectedPoolSettings = map[string]string{
	"user":         "the unix user a pool's workers run as is wor's to decide -- a service that could set it would be able to run as another service's account",
	"group":        "the pool's group is what grants it access to its own document root, and is set when the pool is created",
	"listen":       "the socket path is wor's to decide -- a service that could set it would be able to take over another service's socket",
	"listen.owner": "socket ownership is what lets the web server connect to this pool and nothing else connect to it",
	"listen.group": "socket ownership is what lets the web server connect to this pool and nothing else connect to it",
	"listen.mode":  "socket permissions are what lets the web server connect to this pool and nothing else connect to it",
	"chroot":       "this changes the filesystem the pool sees, which wor cannot then reason about",
	"chdir":        "wor sets the pool's working directory from the service's own document root",
	"error_log":    "the php-fpm master opens a pool's error_log as root, so a service-owned file could point a privileged write anywhere on the host",
	"access.log":   "the php-fpm master opens a pool's log files as root, so a service-owned file could point a privileged write anywhere on the host",
	"slowlog":      "the php-fpm master opens a pool's log files as root, so a service-owned file could point a privileged write anywhere on the host",
}

// rejectedPoolSettingPrefixes covers whole families the same way.
var rejectedPoolSettingPrefixes = map[string]string{
	"php_value":       "PHP ini settings go in " + SettingsFileName + ", which validates them and picks the right directive for each key",
	"php_admin_value": "PHP ini settings go in " + SettingsFileName + ", which validates them and picks the right directive for each key",
	"php_flag":        "PHP ini settings go in " + SettingsFileName + ", which validates them and picks the right directive for each key",
	"php_admin_flag":  "PHP ini settings go in " + SettingsFileName + ", which validates them and picks the right directive for each key",
	"env[":            "wor does not manage a pool's environment; use the service's own configuration",
}

// LoadPoolSettings reads and validates the pool settings file at path.
// A missing file is not an error and yields no settings.
func LoadPoolSettings(path string) ([]PoolSetting, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settings, err := ParsePoolSettings(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return settings, nil
}

// ParsePoolSettings validates one pool settings file's contents,
// returning its directives in file order. Same scanner and same
// all-or-nothing rule as ParseSettings -- see parseKeyValueFile.
func ParsePoolSettings(data []byte) ([]PoolSetting, error) {
	pairs, err := parseKeyValueFile(data, classifyPoolSettingKey)
	if err != nil {
		return nil, err
	}
	settings := make([]PoolSetting, 0, len(pairs))
	for _, p := range pairs {
		settings = append(settings, PoolSetting{Key: p.Key, Value: p.Value})
	}
	if err := checkProcessManagerMode(settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// checkProcessManagerMode rejects a `pm` value wor does not know how to
// render a block for. php-fpm would reject it too -- WritePool's `-t`
// pass catches it and rolls back -- but failing here names the line and
// the three valid values instead of surfacing php-fpm's own error after
// a write.
func checkProcessManagerMode(settings []PoolSetting) error {
	for _, s := range settings {
		if s.Key != "pm" {
			continue
		}
		if _, ok := poolSettingOrder[s.Value]; !ok {
			return fmt.Errorf("pm = %s is not a process manager php-fpm has; use static, dynamic or ondemand", s.Value)
		}
	}
	return nil
}

func classifyPoolSettingKey(key string, lineNo int) error {
	if allowedPoolSettings[key] {
		return nil
	}
	if reason, ok := rejectedPoolSettings[key]; ok {
		return fmt.Errorf("line %d: %s cannot be set per service: %s", lineNo, key, reason)
	}
	for prefix, reason := range rejectedPoolSettingPrefixes {
		if strings.HasPrefix(key, prefix) {
			return fmt.Errorf("line %d: %s cannot be set here: %s", lineNo, key, reason)
		}
	}
	return fmt.Errorf("line %d: %s is not one of the pool settings wor applies per service. Supported: %s", lineNo, key, strings.Join(AllowedPoolSettingKeys(), ", "))
}

// AllowedPoolSettingKeys lists every accepted pool directive, sorted,
// for error messages and for the reference file wor writes.
func AllowedPoolSettingKeys() []string {
	keys := make([]string, 0, len(allowedPoolSettings))
	for key := range allowedPoolSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// PoolSettingLines renders the pool's process-manager block: wor's
// defaults for the process manager in force, with the service's own
// php-fpm.ini applied over them, plus any extra directive the service
// set that is not part of that block.
//
// Overriding by key rather than appending is the point. php-fpm takes
// the last assignment of a directive, so appending would work -- and
// would leave a pool file saying `pm.max_children = 5` on one line and
// `pm.max_children = 40` four lines later, where the reader has to know
// php-fpm's precedence rules to tell which one is live.
func PoolSettingLines(settings []PoolSetting) []string {
	values := make(map[string]string, len(defaultPoolSettings))
	for key, value := range defaultPoolSettings {
		values[key] = value
	}
	mode := values["pm"]
	for _, s := range settings {
		values[s.Key] = s.Value
		if s.Key == "pm" {
			mode = s.Value
		}
	}

	order, ok := poolSettingOrder[mode]
	if !ok {
		// Unreachable through ParsePoolSettings (checkProcessManagerMode
		// rejects it first); falling back to the dynamic block keeps a
		// hand-built Pool from rendering an empty process manager.
		order = poolSettingOrder["dynamic"]
	}
	emitted := map[string]bool{}
	lines := make([]string, 0, len(order)+len(settings))
	for _, key := range order {
		lines = append(lines, key+" = "+values[key])
		emitted[key] = true
	}
	// Directives outside the process-manager block (pm.max_requests,
	// request_terminate_timeout, rlimit_files) follow in file order, so
	// the pool file reads the way the service wrote them.
	for _, s := range settings {
		if !emitted[s.Key] {
			lines = append(lines, s.Key+" = "+s.Value)
			emitted[s.Key] = true
		}
	}
	return lines
}
