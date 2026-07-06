//go:build !windows

package cliapp

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"wor/internal/domainmodel"
)

// webServerRunUser guesses which unix user nginx/apache's worker
// processes run as, so checkWorHomeReachability can test whether that
// user can actually traverse into WOR_HOME. Debian/Ubuntu's nginx and
// apache2 packages both default to "www-data" -- for nginx this is
// checked first against its own "user" directive (a simple best-effort
// line scan of /etc/nginx/nginx.conf, not a full config parser,
// consistent with this project's other deliberately-simplified
// parsers -- see internal/gitignore, phpfpm.DetectListenAddrs) in case
// an operator changed it, falling back to the Debian default
// otherwise.
func webServerRunUser(provider string) string {
	if provider == "nginx" {
		if data, err := os.ReadFile("/etc/nginx/nginx.conf"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "user ") {
					continue
				}
				fields := strings.Fields(strings.TrimSuffix(line, ";"))
				if len(fields) >= 2 {
					return fields[1]
				}
			}
		}
	}
	return "www-data"
}

// webUserExists reports whether name resolves to a real unix account.
// Used to avoid checkWorHomeReachability silently reporting "reachable"
// when the guessed/detected web server user doesn't actually exist on
// this system (pathBlocksTraversal's own best-effort design treats an
// unresolvable user as "not blocked" for every directory, which would
// otherwise look identical to a genuinely clean result).
func webUserExists(name string) bool {
	_, err := user.Lookup(name)
	return err == nil
}

// pathBlocksTraversal reports whether username cannot execute
// (traverse) into dir, based on dir's own owner/group/other
// permission bits -- the same three-way check the kernel itself makes
// when resolving a path. Best-effort: any lookup failure (unknown
// user, unreadable stat, non-unix filesystem) is treated as "not
// blocked" rather than raising a false alarm, since this is an
// advisory `wor doctor` check, not a hard gate.
func pathBlocksTraversal(dir, username string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	u, err := user.Lookup(username)
	if err != nil {
		return false
	}
	mode := info.Mode().Perm()
	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	if u.Uid == uid {
		return mode&0o100 == 0
	}
	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	if u.Gid == gid {
		return mode&0o010 == 0
	}
	if groupIDs, err := u.GroupIds(); err == nil {
		for _, g := range groupIDs {
			if g == gid {
				return mode&0o010 == 0
			}
		}
	}
	return mode&0o001 == 0
}

// blockedPaths reports which of dirs (checked in order, duplicates
// skipped) block webUser's traversal, via pathBlocksTraversal. Shared
// by checkWorHomeReachability (every registered service, used by `wor
// doctor`) and checkServiceReachability (one specific service, used by
// `wor info`) so the two never drift on what "blocked" means.
func blockedPaths(dirs []string, webUser string) []string {
	seen := map[string]bool{}
	var blocked []string
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if pathBlocksTraversal(dir, webUser) {
			blocked = append(blocked, dir)
		}
	}
	return blocked
}

// worHomeAncestorChain returns WOR_HOME and every ancestor directory up
// to "/" -- the traversal chain every request needs regardless of
// which service is being served (this is the class of bug the check
// exists for: WOR_HOME living under a restrictive path like a user's
// home directory, which nginx/php-fpm can never see past no matter how
// open the directories *below* WOR_HOME are).
func worHomeAncestorChain(worHome string) []string {
	var chain []string
	dir := worHome
	for {
		chain = append(chain, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return chain
}

// checkWorHomeReachability checks whether webUser can traverse every
// directory needed to actually serve a static/php site off disk:
// worHomeAncestorChain, plus domains/<domain>/<service>/public for
// every currently registered static or php service specifically -- the
// only templates where the web server reads files directly off disk;
// node/go/python are reverse-proxied to a port and never need
// filesystem access to the service directory at all. Returns every
// blocking directory found, deduplicated, or nil if nothing blocks
// traversal.
func checkWorHomeReachability(a *App, webUser string) []string {
	dirs := worHomeAncestorChain(a.Cfg.WorHome)
	if refs, err := a.Store.ListAllServices(); err == nil {
		for _, ref := range refs {
			if domainmodel.ProcessProviderFor(ref.Service.Type) != "" {
				continue // node/go/python: reverse-proxied, no fs read needed
			}
			dirs = append(dirs,
				filepath.Join(a.Cfg.Domains, ref.Domain),
				filepath.Join(a.Cfg.Domains, ref.Domain, ref.Service.Name),
				filepath.Join(a.Cfg.Domains, ref.Domain, ref.Service.Name, "public"),
			)
		}
	}
	return blockedPaths(dirs, webUser)
}

// checkServiceReachability is checkWorHomeReachability's single-service
// scope, used by `wor info` to answer "can the web server reach THIS
// service" without needing a full `wor doctor` pass. Always includes
// worHomeAncestorChain (a blocked ancestor affects every service
// equally, not just this one), plus this one service's own docroot
// chain -- skipped for node/go/python, same reverse-proxied exception
// checkWorHomeReachability makes.
func checkServiceReachability(a *App, webUser, domain, service, svcType string) []string {
	dirs := worHomeAncestorChain(a.Cfg.WorHome)
	if domainmodel.ProcessProviderFor(svcType) == "" {
		dirs = append(dirs,
			filepath.Join(a.Cfg.Domains, domain),
			filepath.Join(a.Cfg.Domains, domain, service),
			filepath.Join(a.Cfg.Domains, domain, service, "public"),
		)
	}
	return blockedPaths(dirs, webUser)
}

// worHomeReachabilityFixCommand renders the setfacl one-liner that
// grants webUser execute-only (traverse, no read/listing) access to
// every blocked directory at once -- setfacl accepts multiple target
// paths in a single invocation, so this is one command to copy-paste
// regardless of how many directories are involved.
func worHomeReachabilityFixCommand(webUser string, blocked []string) string {
	return fmt.Sprintf("setfacl -m u:%s:--x %s", webUser, strings.Join(blocked, " "))
}
