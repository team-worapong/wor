package cliapp

import (
	"fmt"
	"os"

	"wor/internal/domainmodel"
)

// cmdPath implements `wor path [<domain>[/<service>]]`: resolve a
// domain or service to its absolute directory under WOR_HOME/domains
// and print it -- nothing else -- to stdout.
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
//
// Anything beyond the path itself (an [OK] prefix, a trailing note)
// would break command substitution, so errors go to stderr via the
// normal ERROR path and stdout stays clean.
//
// With no argument it prints the domains root, so `wor goto` alone
// jumps to WOR_HOME/domains.
//
// Validation is deliberately just "the directory exists": resolving
// through ParseTarget/RequireSlug already blocks path traversal
// (`wor path ../../etc` is rejected as an invalid slug), and checking
// os.Stat instead of services.config.json means you can still jump
// into a folder that exists on disk but was never registered as a
// service -- which is exactly when you're most likely to want a look
// around.
func (a *App) cmdPath(args []string) error {
	if len(args) > 1 {
		return a.errf("usage: wor path [<domain>[/<service>]]")
	}
	dir := a.Store.DomainsDir
	if len(args) == 1 && args[0] != "" {
		domain, service, err := domainmodel.ParseTarget(args[0])
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
