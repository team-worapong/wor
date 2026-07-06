package cliapp

import "fmt"

// shellInitScript is what `wor shell-init` prints for the user's rc
// file. It wraps the real binary in a same-named shell function so
// that `wor goto <domain>[/<service>]` can cd -- something the wor
// process itself can never do to its parent shell (see cmdPath's doc
// comment in path.go). Every other subcommand falls through to
// `command wor "$@"` untouched.
//
// Kept to strict POSIX-ish constructs that bash and zsh share (`local`
// is not POSIX but works in both); fish users would need a separate
// variant, which we don't ship until someone asks.
//
// Notes on the details:
//   - `shift` inside the function only shifts the *function's* args,
//     so a bare `wor goto` becomes a bare `wor path` -- which opens
//     the interactive picker. The menu works *inside* command
//     substitution because the menu + prompt go to stderr and the
//     selection is read from stdin -- $(...) captures only stdout,
//     which carries nothing but the chosen path (see pathPick in
//     path.go).
//   - The resolved path is captured before cd so a resolution failure
//     (unknown domain, uninitialized workspace, cancelled picker, ...)
//     prints wor's normal ERROR on stderr and returns nonzero without
//     moving the shell anywhere.
//   - `command wor` bypasses the function itself, avoiding infinite
//     recursion.
const shellInitScript = `# wor shell integration.
# Install by adding this line to ~/.bashrc or ~/.zshrc:
#   eval "$(wor shell-init)"
# Then:
#   wor goto <domain>            -> cd WOR_HOME/domains/<domain>
#   wor goto <domain>/<service>  -> cd WOR_HOME/domains/<domain>/<service>
#   wor goto .                   -> cd WOR_HOME
#   wor goto ./<path>            -> cd WOR_HOME/<path>  (e.g. ./logs)
#   wor goto                     -> numbered menu (WOR_HOME first, then
#                                   every domain and domain/service);
#                                   pick one to cd there
wor() {
    if [ "$1" = "goto" ]; then
        shift
        local _wor_dir
        _wor_dir="$(command wor path "$@")" || return $?
        cd "$_wor_dir" || return $?
    else
        command wor "$@"
    fi
}
`

// cmdShellInit implements `wor shell-init`: print shellInitScript and
// nothing else, so `eval "$(wor shell-init)"` in an rc file stays
// safe. That eval runs on every new shell, which is why this command
// is exempt from both the workspace-initialized gate and the WOR_HOME
// lock in app.go -- it must never print an ERROR banner (eval'ing one
// would spew into every new terminal) and never block on another wor
// command holding the lock.
func (a *App) cmdShellInit(args []string) error {
	if len(args) > 0 {
		return a.errf("usage: wor shell-init (takes no arguments; eval its output in your shell rc file)")
	}
	fmt.Fprint(a.Out, shellInitScript)
	return nil
}
