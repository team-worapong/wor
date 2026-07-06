// Package phpfpm manages per-service php-fpm pools: detecting installed
// PHP-FPM versions, writing/removing one pool's config file, and
// validating+reloading php-fpm after a change. Unlike internal/systemd
// and internal/pm2 (which wrap a process wor itself starts and stops),
// php-fpm's master process is assumed already running as its own system
// service -- this package only ever adds/removes one pool's *.conf file
// underneath it and asks it to reload, the same test-before-reload shape
// hostprovider's nginx/apache providers already use.
//
// Linux (Debian/Ubuntu's /etc/php/<version>/fpm layout) and macOS
// (Homebrew) only. Windows has no official PHP-FPM build at all -- see
// hostprovider.PHPFPMEndpoint's Windows branch, which already only
// supports a user-supplied remote/manual endpoint -- so every exported
// function here fails fast on Windows rather than pretending to manage
// a local pool that can't exist. RHEL-family Linux distros use a
// different package layout than /etc/php/<version>/fpm and are not
// auto-detected yet (see DetectVersions).
package phpfpm

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wor/internal/osutil"
)

// maxPoolNameLen keeps pool/unix-user names within the traditional
// 32-byte unix login name limit (utmp's ut_user field) that useradd
// still enforces by default on most distros.
const maxPoolNameLen = 32

// PoolName returns the identifier wor uses for one service's php-fpm
// pool: its pool config file's [section] name, its dedicated unix user
// name (see EnsureUser), and the base name of both its pool .conf file
// and its socket file. Mirrors pm2.Name/systemd.Name's
// "wor_<domain>_<service>" convention, truncated and disambiguated with
// a short hash when that would exceed maxPoolNameLen -- long
// domain/service names are otherwise a common way to silently break
// useradd.
func PoolName(domain, service string) string {
	base := "wor_" + domain + "_" + service
	if len(base) <= maxPoolNameLen {
		return base
	}
	sum := sha1.Sum([]byte(base))
	suffix := fmt.Sprintf("_%x", sum[:3]) // 7 bytes: "_" + 6 hex chars
	keep := maxPoolNameLen - len(suffix)
	if keep < 0 {
		keep = 0
	}
	return base[:keep] + suffix
}

// Version identifies one installed PHP-FPM version wor can create a
// pool under.
type Version struct {
	Number     string // e.g. "8.3"
	PoolDir    string // directory wor writes wor_<domain>_<service>.conf into
	SockDir    string // directory wor writes this version's pool sockets into
	FPMBin     string // php-fpm binary path, used for `-t` config validation
	ReloadUnit string // systemd unit name (Linux) or brew formula name (macOS)
}

// linuxPHPRoot is the Debian/Ubuntu-style parent directory of every
// installed PHP version, matching the naming already assumed by
// hostprovider's unixPHPFPMSockets list (php8.4-fpm.sock etc).
const linuxPHPRoot = "/etc/php"

// homebrewPHPRoots covers both Apple Silicon and Intel Homebrew prefixes.
var homebrewPHPRoots = []string{"/opt/homebrew/etc/php", "/usr/local/etc/php"}

var versionDirRe = regexp.MustCompile(`^\d+\.\d+$`)

// scanVersionDirs finds every immediate subdirectory of root shaped
// like a PHP version number ("8.3") that also contains poolSubdir,
// returning the version numbers found. Split out from
// detectLinuxVersions/detectHomebrewVersions so version-directory
// parsing can be unit tested against a temp directory instead of
// depending on a real /etc/php or Homebrew install.
func scanVersionDirs(root, poolSubdir string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !versionDirRe.MatchString(e.Name()) {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, e.Name(), poolSubdir)); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// DetectVersions returns every PHP-FPM version wor can find installed,
// newest first. Returns nil on Windows, and on Linux distros that don't
// use the /etc/php/<version>/fpm layout.
func DetectVersions() []Version {
	var versions []Version
	switch {
	case osutil.IsLinux():
		versions = detectLinuxVersions()
	case osutil.IsMacOS():
		versions = detectHomebrewVersions()
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Number > versions[j].Number })
	return versions
}

func detectLinuxVersions() []Version {
	var out []Version
	for _, num := range scanVersionDirs(linuxPHPRoot, "fpm") {
		fpmDir := filepath.Join(linuxPHPRoot, num, "fpm")
		bin := osutil.FindBinary("php-fpm"+num, "/usr/sbin/php-fpm"+num)
		out = append(out, Version{
			Number:     num,
			PoolDir:    filepath.Join(fpmDir, "pool.d"),
			SockDir:    "/run/php",
			FPMBin:     bin,
			ReloadUnit: "php" + num + "-fpm",
		})
	}
	return out
}

func detectHomebrewVersions() []Version {
	var out []Version
	for _, root := range homebrewPHPRoots {
		prefix := filepath.Dir(filepath.Dir(root)) // ".../etc/php" -> ".../etc" -> "..."
		for _, num := range scanVersionDirs(root, "php-fpm.d") {
			bin, reloadUnit := resolveHomebrewPHPBinary(prefix, num)
			out = append(out, Version{
				Number:     num,
				PoolDir:    filepath.Join(root, num, "php-fpm.d"),
				SockDir:    filepath.Join(prefix, "var", "run"),
				FPMBin:     bin,
				ReloadUnit: reloadUnit,
			})
		}
	}
	return out
}

// resolveHomebrewPHPBinary finds the actual php-fpm binary for a
// detected version directory, and the Homebrew service name that
// controls it. Homebrew's php config always lives under a
// version-numbered directory (.../etc/php/<num>/...) regardless of
// whether the installed formula is a specifically-tapped "php@<num>"
// keg or simply the plain "php" formula (whenever <num> happens to be
// whatever's currently the latest/default version, with no separate
// versioned keg installed at all) -- assuming it's always "php@<num>"
// (as this function's predecessor did) breaks on that second, very
// common case: wor would then guess a Homebrew service name and binary
// path that don't exist, so it looks for a running process under a
// name Homebrew never used and later (`wor run`) tries to
// (re-)bootstrap that already-loaded LaunchAgent under the wrong name,
// which surfaces as a launchctl "Bootstrap failed: 5" error.
//
// This tries the versioned keg path first, then falls back to the
// plain "php" keg only if its own reported version actually matches
// num -- never blindly assuming "php" == this version, which would
// silently point a service at the wrong PHP binary on a host with
// several versions installed side by side.
//
// A further wrinkle found via a live bug report: Homebrew can make
// *both* "opt/php@<num>" and "opt/php" resolve (as symlinks) to the
// exact same Cellar keg when only the plain "php" formula is
// installed -- the "php@<num>" opt path existing is not proof a
// formula literally named "php@<num>" is what's actually registered
// with `brew services`/launchd. Preferring the versioned path
// unconditionally in that case previously produced a reloadUnit
// (e.g. "php@8.5") that doesn't match brew's real service name
// ("php"), which made IsRunning/Start/reload act on a name brew has
// never heard of. When both candidates resolve, this now asks `brew
// services list` (the actual ground truth for the registered name)
// which one is real, rather than guessing from path existence alone.
func resolveHomebrewPHPBinary(prefix, num string) (bin, reloadUnit string) {
	versionedBin := osutil.FindBinary("php-fpm"+num, filepath.Join(prefix, "opt", "php@"+num, "sbin", "php-fpm"))

	plain := filepath.Join(prefix, "opt", "php", "sbin", "php-fpm")
	plainBin := ""
	if osutil.IsExecutableFile(plain) && phpFPMVersionMatches(plain, num) {
		plainBin = plain
	}

	switch {
	case versionedBin != "" && plainBin != "":
		if brewServiceExists("php@" + num) {
			return versionedBin, "php@" + num
		}
		if brewServiceExists("php") {
			return plainBin, "php"
		}
		// Neither name has ever been registered with brew services
		// (e.g. this master has never been started that way) -- fall
		// back to the versioned name, matching this function's
		// previous unconditional preference.
		return versionedBin, "php@" + num
	case versionedBin != "":
		return versionedBin, "php@" + num
	case plainBin != "":
		return plainBin, "php"
	default:
		// Nothing resolvable on disk -- keep the old guessed name/path so
		// error messages still point somewhere plausible, even though
		// nothing will actually be found running under it.
		return "", "php@" + num
	}
}

// phpFPMVersionMatches reports whether bin's own "-v" output claims to
// be PHP num (e.g. "8.5"), so resolveHomebrewPHPBinary never mistakes
// an unrelated PHP version's binary for a match just because it's also
// named "php".
func phpFPMVersionMatches(bin, num string) bool {
	out, err := exec.Command(bin, "-v").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "PHP "+num)
}

// ResolveVersion looks up one specific PHP-FPM version among
// DetectVersions()'s live results, for callers (pool removal, host
// config rendering) that only have a version number string on record
// (domainmodel.Service.PHPVersion) and need the full Version (its pool
// directory, socket directory, reload unit) to act on it.
func ResolveVersion(number string) (Version, bool) {
	for _, v := range DetectVersions() {
		if v.Number == number {
			return v, true
		}
	}
	return Version{}, false
}

// DefaultMaxChildren is used when Pool.MaxChildren is left at zero.
const DefaultMaxChildren = 5

// Pool describes one per-service php-fpm pool wor manages.
type Pool struct {
	Domain  string
	Service string
	Version Version
	User    string // dedicated unix user this pool runs as, see EnsureUser
	Group   string // group granted read access to the service's document root, see GrantGroupAccess
	// ListenOwner/ListenGroup are the socket file's owner/group --
	// crucially NOT the same thing as User/Group: the socket is mode
	// 0660, and the process that must connect() to it is the WEB SERVER
	// (nginx/apache), not the pool's own workers. Leaving these as the
	// pool user produced a real 502 on a real Debian host (2026-07-06):
	// every wor check passed (wor itself could connect), but www-data
	// couldn't. Callers set these to the web server's run user on
	// Linux; empty falls back to User/Group (the pre-fix behavior,
	// which is also correct on macOS where everything runs as the
	// login user anyway).
	ListenOwner string
	ListenGroup string
	MaxChildren int // pm.max_children; DefaultMaxChildren if <= 0
}

// SocketPath returns the unix socket path wor listens this pool on.
func SocketPath(v Version, domain, service string) string {
	return filepath.Join(v.SockDir, PoolName(domain, service)+".sock")
}

// PoolFilePath returns the absolute path of domain/service's pool
// config file under v's pool directory.
func PoolFilePath(v Version, domain, service string) string {
	return filepath.Join(v.PoolDir, PoolName(domain, service)+".conf")
}

func poolFileContent(p Pool) string {
	maxChildren := p.MaxChildren
	if maxChildren <= 0 {
		maxChildren = DefaultMaxChildren
	}
	name := PoolName(p.Domain, p.Service)
	sock := SocketPath(p.Version, p.Domain, p.Service)
	listenOwner, listenGroup := p.ListenOwner, p.ListenGroup
	if listenOwner == "" {
		listenOwner = p.User
	}
	if listenGroup == "" {
		listenGroup = p.Group
	}
	return fmt.Sprintf(`[%s]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660
pm = dynamic
pm.max_children = %d
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
`, name, p.User, p.Group, sock, listenOwner, listenGroup, maxChildren)
}

// WritePool writes p's pool config file, validates the resulting
// php-fpm config with `-t` before touching anything live, and reloads
// php-fpm only if validation passed. On validation failure the
// just-written pool file is removed again, so a bad pool config is
// never left half-applied (and never risks taking down every other
// pool sharing the same php-fpm master on the next real reload). The
// same rollback applies if reload() itself fails: a config the running
// master never actually picked up has no business lingering on disk
// looking like a live pool.
func WritePool(p Pool) error {
	if osutil.IsWindows() {
		return fmt.Errorf("per-service php-fpm pools are not supported on Windows")
	}
	path := PoolFilePath(p.Version, p.Domain, p.Service)
	if err := osutil.WriteFilePrivileged(path, []byte(poolFileContent(p))); err != nil {
		return err
	}
	if err := testConfig(p.Version); err != nil {
		osutil.RemoveFilePrivileged(path)
		return fmt.Errorf("php-fpm config test failed after writing pool %s, rolled back: %w", PoolName(p.Domain, p.Service), err)
	}
	if err := reload(p.Version); err != nil {
		osutil.RemoveFilePrivileged(path)
		return fmt.Errorf("php-fpm reload failed after writing pool %s, rolled back: %w", PoolName(p.Domain, p.Service), err)
	}
	return nil
}

// RemovePool removes domain/service's pool file (if present) and
// reloads php-fpm. A missing file is not an error, matching
// systemd.RemoveUnit's tolerance for cleaning up a partially-created
// service.
func RemovePool(v Version, domain, service string) error {
	if osutil.IsWindows() {
		return nil
	}
	path := PoolFilePath(v, domain, service)
	if err := osutil.RemoveFilePrivileged(path); err != nil {
		return err
	}
	if err := reload(v); err != nil {
		return err
	}
	// php-fpm drops the pool from its runtime on reload but does NOT
	// unlink the dead pool's socket file, so /run/php/<pool>.sock would
	// linger forever (observed on a real Debian host, 2026-07-06, after
	// `wor service remove`). Best-effort: a leftover socket with no
	// listener behind it is cosmetic, so a removal failure must not turn
	// a successfully removed pool into an error.
	osutil.RemoveFilePrivileged(SocketPath(v, domain, service))
	return nil
}

// testConfig runs `<fpmBin> -t`, the same config-test invocation
// hostprovider.DetectListenAddrs uses -- validates and exits without
// binding anything, so it's safe even while the real php-fpm master is
// already running.
//
// On Linux this needs elevation: `-t` doesn't just parse syntax, it also
// opens the master's global error_log (e.g. /var/log/php8.4-fpm.log,
// root-owned with restrictive perms on Debian/Ubuntu) to confirm it's
// writable. Running it as wor's own unprivileged invoking user fails
// with "failed to open error_log ... Permission denied" (exit status
// 78) even when the pool file just written is perfectly valid -- which
// then made WritePool roll back a good config. Matches
// hostprovider/nginx.go's nginxProvider.Test(), which already elevates
// its own `-t` on Linux for the same reason. macOS (Homebrew) typically
// runs php-fpm and owns its logs as the invoking user, so no
// elevation is needed there.
func testConfig(v Version) error {
	if v.FPMBin == "" {
		return fmt.Errorf("php-fpm binary not found for PHP %s", v.Number)
	}
	if osutil.IsMacOS() {
		out, err := exec.Command(v.FPMBin, "-t").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s -t: %w: %s", v.FPMBin, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd, err := osutil.SudoCommand(v.FPMBin, "-t")
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("%s -t (%s): %w: %s", v.FPMBin, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsRunning reports whether v's php-fpm master service is currently
// active. Used by `wor run` to tell an already-running master from one
// that needs starting before a pool underneath it can be relied on --
// unlike reload(), which assumes the master is already up.
//
// Checks the service manager first (Homebrew/systemctl, keyed on
// v.ReloadUnit), but falls back to a naming-agnostic process check
// (fpmProcessRunning) if that says "not running" -- v.ReloadUnit is
// wor's own guessed convention ("php@<version>" on Homebrew), which can
// mismatch the formula actually installed (e.g. a host where "php"
// itself, unversioned, happens to be that version, rather than a
// separately tapped "php@X.Y" keg). Trusting a false "not running" here
// would make `wor run` try to (re-)start an already-loaded master,
// which on macOS surfaces as a launchctl "Bootstrap failed: 5" error
// (bootstrapping something already loaded in the gui session).
func IsRunning(v Version) bool {
	if osutil.IsWindows() {
		return false
	}
	if osutil.IsMacOS() {
		if brewServiceStarted(v.ReloadUnit) {
			return true
		}
		return fpmProcessRunning(v)
	}
	out, err := exec.Command("systemctl", "is-active", v.ReloadUnit).Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		return true
	}
	return fpmProcessRunning(v)
}

// fpmProcessRunning is IsRunning's naming-agnostic fallback: rather than
// trust the service manager's exact formula/unit name, it checks whether
// a process running this version's own php-fpm binary already exists
// (`pgrep -f <FPMBin>`). Best-effort -- if pgrep isn't available, this
// just returns false, same as any other undetectable state.
//
// Known weak spot (found via a live false-negative report, not just
// theoretical): php-fpm rewrites its own process title via setproctitle
// once it starts (`ps`/`pgrep -f` then sees "php-fpm: master process
// (/path/to/php-fpm.conf)", not the original exec path) -- so this
// match can fail against a perfectly healthy master on any OS, not just
// as a Homebrew-naming quirk. IsRunning's callers should keep that in
// mind: a false "not running" here is possible even when both
// IsRunning's checks are implemented correctly. PoolAlive (below) is
// the safer signal for "is this specific pool usable" precisely because
// it sidesteps process/service-name matching entirely.
func fpmProcessRunning(v Version) bool {
	if v.FPMBin == "" {
		return false
	}
	out, err := exec.Command("pgrep", "-f", v.FPMBin).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// poolDialTimeout bounds how long PoolAlive waits to connect to a
// pool's socket -- long enough to tolerate a master briefly busy under
// load, short enough that `wor service status` doesn't visibly hang per
// php row.
const poolDialTimeout = 300 * time.Millisecond

// PoolAlive reports whether domain/service's own pool socket is
// currently accepting connections. This is a direct, name-independent
// signal of whether THIS pool specifically is up -- unlike IsRunning,
// which checks the shared php-fpm MASTER's process-manager state (the
// right question for `wor run`'s "should I start the master" decision,
// but the wrong one for a per-pool status row): IsRunning's
// service-name guess can be wrong (see resolveHomebrewPHPBinary's
// php-vs-php@X.Y ambiguity), and its pgrep fallback can't reliably
// match a live master at all once setproctitle has overwritten its
// command line (see fpmProcessRunning). Dialing the pool's own socket
// avoids both problems entirely. Always false on Windows, matching
// every other function in this package.
func PoolAlive(v Version, domain, service string) bool {
	if osutil.IsWindows() {
		return false
	}
	conn, err := net.DialTimeout("unix", SocketPath(v, domain, service), poolDialTimeout)
	if err != nil {
		// Permission denied is NOT "pool down". Since the
		// socket-ownership fix, pool sockets are owned by the WEB
		// SERVER's user at mode 0660 -- which is exactly right for
		// nginx/apache and exactly wrong for an unprivileged wor
		// process trying to dial them (a correctly-secured pool showed
		// up as "not accepting connections" across diagnose/health/
		// status on a real host, 2026-07-07, while serving 200s the
		// whole time). Denied-but-workers-exist means the pool is up;
		// the worker check reads /proc directly, so no sudo prompt
		// (diagnose's non-interactive rule).
		if errors.Is(err, os.ErrPermission) {
			return poolWorkersRunning(domain, service)
		}
		return false
	}
	conn.Close()
	return true
}

// Start starts v's php-fpm master service if it isn't already running.
// Per the per-service php-fpm design, wor now manages this master's
// lifecycle end-to-end for versions it creates pools under (superseding
// the older "php-fpm is assumed already running" invariant for that
// case) -- see docs/services.md.
// poolWorkersRunning reports whether any php-fpm worker process for
// this pool exists, by scanning /proc/<pid>/cmdline for the exact
// process title php-fpm gives its workers ("php-fpm: pool <name>").
// Unlike fpmProcessRunning's pgrep-on-binary-path heuristic (see its
// doc comment for why that can't find setproctitle'd processes), the
// worker title IS the setproctitle output, so exact-matching it is
// reliable. Linux only -- macOS has no /proc, and macOS pools run as
// the login user so PoolAlive's dial never gets EACCES there.
func poolWorkersRunning(domain, service string) bool {
	return len(PoolWorkerPIDs(domain, service)) > 0
}

// PoolWorkerPIDs returns the PIDs of every php-fpm worker process
// belonging to this pool, found by exact-matching each
// /proc/<pid>/cmdline against the worker process title. Shared by
// poolWorkersRunning (liveness) and the resource-usage reporting in
// `wor health`/`wor info` (per-pool cpu/mem is the sum over these
// workers). Linux only; nil elsewhere.
func PoolWorkerPIDs(domain, service string) []int {
	if !osutil.IsLinux() {
		return nil
	}
	needle := "php-fpm: pool " + PoolName(domain, service)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if readErr != nil {
			continue
		}
		title := strings.TrimRight(strings.ReplaceAll(string(data), "\x00", " "), " ")
		if title == needle {
			pids = append(pids, pid)
		}
	}
	return pids
}

// Restart fully restarts this version's php-fpm master -- needed (vs
// reload) exactly when a pool's socket OWNERSHIP must change: php-fpm
// only chown()s a socket when it binds it, and a graceful reload keeps
// already-bound sockets, so a listen.owner change on an existing
// socket silently does nothing until a real restart re-creates it
// (verified on a real Debian host, 2026-07-07). Briefly interrupts
// every pool under this master, so callers should ask before using it.
func Restart(v Version) error {
	if osutil.IsWindows() {
		return fmt.Errorf("php-fpm is not supported on Windows")
	}
	if osutil.IsMacOS() {
		// brew services restart is already a full restart -- same as
		// reload() on macOS.
		return reload(v)
	}
	cmd, err := osutil.SudoCommand("systemctl", "restart", v.ReloadUnit)
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("systemctl restart %s (%s): %w: %s", v.ReloadUnit, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func Start(v Version) error {
	if osutil.IsWindows() {
		return fmt.Errorf("php-fpm is not supported on Windows")
	}
	if osutil.IsMacOS() {
		if !osutil.Exists("brew") {
			return fmt.Errorf("brew not found; cannot start %s", v.ReloadUnit)
		}
		out, err := exec.Command("brew", "services", "start", v.ReloadUnit).CombinedOutput()
		if err != nil {
			return fmt.Errorf("brew services start %s: %w: %s", v.ReloadUnit, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd, err := osutil.SudoCommand("systemctl", "start", v.ReloadUnit)
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("systemctl start %s (%s): %w: %s", v.ReloadUnit, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// brewServiceRow returns `brew services list`'s fields for name ("Name
// Status User File"), or nil if name has no row at all -- shared lookup
// behind brewServiceStarted (is it running) and brewServiceExists (is
// this name registered with brew at all, regardless of status).
func brewServiceRow(name string) []string {
	out, err := exec.Command("brew", "services", "list").Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[0] == name {
			return fields
		}
	}
	return nil
}

// brewServiceStarted reports whether Homebrew considers name's service
// started (duplicated from hostprovider's identical helper -- this
// project's leaf packages deliberately don't import each other for
// small shared utilities like this, see e.g. reload()'s own brew-restart
// logic already being separately implemented per package).
func brewServiceStarted(name string) bool {
	row := brewServiceRow(name)
	return len(row) >= 2 && row[1] == "started"
}

// brewServiceExists reports whether name appears as a row in `brew
// services list` at all, regardless of its started/stopped status.
// Used by resolveHomebrewPHPBinary to tell which of "php"/"php@X.Y" is
// actually the registered brew service name when both happen to resolve
// as valid on-disk paths -- opt/ symlink existence alone can't
// disambiguate that, but brew's own service list is ground truth.
func brewServiceExists(name string) bool {
	return brewServiceRow(name) != nil
}

// reload asks the running php-fpm master for v to pick up the pool.d
// change: `systemctl reload phpX.Y-fpm` on Linux, `brew services
// restart php@X.Y` on macOS (Homebrew's LaunchAgent wrapper has no
// reload verb, only start/stop/restart -- the same tradeoff
// hostprovider/nginx.go's macOS reload() already makes for nginx).
func reload(v Version) error {
	if osutil.IsMacOS() {
		if !osutil.Exists("brew") {
			return fmt.Errorf("brew not found; cannot reload %s", v.ReloadUnit)
		}
		out, err := exec.Command("brew", "services", "restart", v.ReloadUnit).CombinedOutput()
		if err != nil {
			return fmt.Errorf("brew services restart %s: %w: %s", v.ReloadUnit, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd, err := osutil.SudoCommand("systemctl", "reload", v.ReloadUnit)
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("systemctl reload %s (%s): %w: %s", v.ReloadUnit, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}
