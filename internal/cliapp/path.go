package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wor/internal/domainmodel"
)

// cmdPath implements `wor path [--pick] [<domain>[/<service>]]`:
// resolve a domain or service to its absolute directory under
// WOR_HOME/domains and print it -- nothing else -- to stdout.
//
// The bare, prefix-free output is the whole point: a wor process can
// never change its parent shell's working directory (an OS-level
// restriction every CLI hits -- zoxide, nvm, conda all work around it
// the same way), so "take me to that service's folder" has to be a
// two-piece design:
//
//	wor path <target>   -> resolves + validates, prints the directory
//	cd "$(wor path x)"  -> works immediately, no setup
//	wor goto <target>   -> shell function from `wor shell-init` that
//	                       does the cd for you (see shellinit.go)
//	wor goto            -> no target: the shell function calls
//	                       `wor path --pick`, a numbered interactive
//	                       menu of every domain and domain/service
//	                       (see pathPick below)
//	wor goto .          -> WOR_HOME itself (the one target that isn't
//	                       under domains/)
//	wor goto ./<path>   -> WOR_HOME/<path> -- any subtree of WOR_HOME
//	                       (logs/, backups/, ...), not just domains
//
// Anything beyond the path itself (an [OK] prefix, a trailing note)
// would break command substitution, so errors go to stderr via the
// normal ERROR path and stdout stays clean.
//
// With no argument it opens the picker, exactly like a bare
// `wor goto` -- scripts that need a directory non-interactively must
// name one (`wor path .`, `wor path <domain>[/<service>]`).
//
// Validation is deliberately just "the directory exists": resolving
// through ParseTarget/RequireSlug already blocks path traversal
// (`wor path ../../etc` is rejected as an invalid slug), and checking
// os.Stat instead of services.config.json means you can still jump
// into a folder that exists on disk but was never registered as a
// service -- which is exactly when you're most likely to want a look
// around.
func (a *App) cmdPath(args []string) error {
	pick := false
	target := ""
	for _, arg := range args {
		switch {
		case arg == "--pick":
			pick = true
		case strings.HasPrefix(arg, "-"):
			return a.errf("unknown flag: %s (usage: wor path [--pick] [<domain>[/<service>]])", arg)
		case target == "":
			target = arg
		default:
			return a.errf("usage: wor path [--pick] [<domain>[/<service>]]")
		}
	}
	if pick && target != "" {
		return a.errf("--pick and an explicit <domain>[/<service>] target are mutually exclusive")
	}
	if pick || target == "" {
		// A bare `wor path` opens the same interactive picker a bare
		// `wor goto` does (owner's call: the two should feel
		// identical). --pick is kept as an explicit synonym. The
		// non-interactive way to get a directory is to name it:
		// `wor path .` (WOR_HOME) or `wor path <domain>[/<service>]`
		// -- a headless bare `wor path` now just exits 1 ("cancelled")
		// when stdin has no terminal to answer the menu with.
		return a.pathPick()
	}

	var dir string
	if target == "." {
		// `wor path .` / `wor goto .` -> WOR_HOME itself. Checked
		// before ParseTarget, whose slug rule ([a-z0-9-]) would
		// otherwise reject the dot.
		dir = a.Cfg.WorHome
	} else if strings.HasPrefix(target, "./") {
		// `wor path ./<path>` / `wor goto ./<path>` -> WOR_HOME/<path>
		// (e.g. `wor goto ./logs/myapp`, `wor goto ./backups`). This
		// form deliberately allows arbitrary multi-level paths --
		// WOR_HOME's own subtrees (logs/, backups/, ssl/, ...) aren't
		// slug-named -- so it can't go through ParseTarget; instead
		// the escape hatch ParseTarget's slug rule closes is closed
		// here by hand: after Clean, anything that still starts with
		// ".." (or turned absolute, e.g. `.//etc`) points outside
		// WOR_HOME and is refused.
		rel := filepath.Clean(strings.TrimPrefix(target, "./"))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return a.errf("path escapes WOR_HOME: %s", target)
		}
		dir = filepath.Join(a.Cfg.WorHome, rel)
	} else {
		domain, service, err := domainmodel.ParseTarget(target)
		if err != nil {
			return err
		}
		if service == "" {
			dir = a.Store.DomainDir(domain)
		} else {
			dir = a.Store.ServiceDir(domain, service)
		}
	}
	st, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return a.errf("no such directory: %s (check `wor domain add` / `wor service add`, or the spelling)", dir)
		}
		return err
	}
	if !st.IsDir() {
		return a.errf("not a directory: %s", dir)
	}
	fmt.Fprintln(a.Out, dir)
	return nil
}

// pathPick implements `wor path --pick`: a numbered menu of every
// domain and domain/service, sorted by domain (os.ReadDir returns
// entries name-sorted already, so domains and each domain's services
// come out ordered for free).
//
// The split between output streams is what makes this usable from the
// `wor goto` shell function, which runs it inside command
// substitution: the menu and prompt go to STDERR (which passes
// through to the terminal), the selection is read from stdin (still
// the terminal -- $(...) redirects only stdout), and the single
// chosen path is the only thing printed to STDOUT, so it's all the
// function captures and cd's to.
//
// Listing is directory-based (subdirectories of each domain dir),
// matching cmdPath's exists-on-disk validation philosophy: an
// unregistered-but-present folder is still offered. Domain dirs only
// ever hold service dirs plus wor's own config files
// (services.config.json etc. -- backups/logs trees live elsewhere,
// see cmdDomain add), so plain "is a directory" is a sufficient
// filter; dot-dirs (.git and friends) are skipped.
//
// Cancelling (empty Enter, or EOF on a non-terminal stdin) returns a
// "cancelled" error: Run() must exit nonzero here, because a zero
// exit with empty stdout would make the shell function `cd ""`.
func (a *App) pathPick() error {
	entries, err := os.ReadDir(a.Store.DomainsDir)
	if err != nil {
		return err
	}
	type pathOption struct {
		label string
		dir   string
	}
	// WOR_HOME is always entry 1 (owner request): it's the one useful
	// jump target that isn't under domains/, and it also guarantees
	// the menu is never empty on a fresh workspace with no domains
	// yet. The absolute path is shown in the label because -- unlike
	// the domain entries -- the name alone doesn't tell you where it
	// points.
	opts := []pathOption{{"WOR_HOME (" + a.Cfg.WorHome + ")", a.Cfg.WorHome}}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		domain := e.Name()
		domainDir := a.Store.DomainDir(domain)
		opts = append(opts, pathOption{domain, domainDir})
		subs, err := os.ReadDir(domainDir)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			opts = append(opts, pathOption{domain + "/" + s.Name(), filepath.Join(domainDir, s.Name())})
		}
	}
	width := len(strconv.Itoa(len(opts)))
	fmt.Fprintln(a.Err)
	for i, o := range opts {
		fmt.Fprintf(a.Err, "  %*d) %s\n", width, i+1, o.label)
	}
	for {
		fmt.Fprintf(a.Err, "Select 1-%d (Enter to cancel): ", len(opts))
		line, readErr := a.In.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return a.errf("cancelled")
		}
		n, convErr := strconv.Atoi(line)
		if convErr == nil && n >= 1 && n <= len(opts) {
			fmt.Fprintln(a.Out, opts[n-1].dir)
			return nil
		}
		fmt.Fprintf(a.Err, "Pick a number between 1 and %d.\n", len(opts))
		if readErr != nil {
			// stdin has ended (EOF/error) -- re-prompting would spin
			// forever on the same result.
			return a.errf("cancelled")
		}
	}
}
