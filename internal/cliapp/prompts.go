package cliapp

import (
	"fmt"
	"strings"
)

// promptAbort is the panic value prompt uses to end a command that
// asked a question nobody can answer: stdin reached EOF before a line
// was typed, or --non-interactive is set. App.Run recovers it and
// reports it as the command's error.
//
// A panic rather than an error return, because a question can be asked
// from any depth (the ~50 prompt call sites all return a plain answer)
// and the only correct continuation is "stop here" -- the same thing
// Ctrl-C does at that moment. That is safe because wor persists state
// last (see the ssl issue flow and applyHostParams): a command stopped
// at a question has not yet recorded the change it was asking about.
// The exception is an optional follow-up asked after the work is
// already done, where stopping would report a finished command as
// failed; those use offer instead, which never aborts.
type promptAbort struct{ reason string }

// readAnswer prints message to a.Err (matching the shell version's
// habit of sending prompts to stderr so stdout stays script-friendly)
// and reads one line from a.In. ok is false when there is nobody to
// answer: --non-interactive is set, or stdin ended before any input.
// A final line without a trailing newline still counts as an answer.
func (a *App) readAnswer(message string) (answer string, ok bool) {
	fmt.Fprint(a.Err, message)
	if a.NonInteractive {
		fmt.Fprintln(a.Err)
		return "", false
	}
	line, err := a.In.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(a.Err)
		return "", false
	}
	return strings.TrimSpace(line), true
}

// prompt reads one answer, and ends the command (see promptAbort) when
// nobody can give one. It used to return "" on EOF, which every
// caller then read as "pressed Enter" -- so a wor run with no stdin
// (cron, a control panel) silently accepted every default, including
// the elevation gate and `wor domain remove`'s Web Data question.
func (a *App) prompt(message string) string {
	answer, ok := a.readAnswer(message)
	if !ok {
		panic(promptAbort{a.noAnswerReason(message)})
	}
	return answer
}

// noAnswerReason is the error a question nobody answered ends the
// command with. It names the question, because "cancelled" alone
// leaves a script's author guessing which one needed a flag.
func (a *App) noAnswerReason(message string) string {
	question := strings.TrimRight(strings.TrimSpace(message), ":")
	if a.NonInteractive {
		return fmt.Sprintf("%q needs an answer, but wor is running non-interactively -- cancelled. Pass the flag that answers it, or run the command in a terminal.", question)
	}
	return fmt.Sprintf("no answer to %q (stdin closed) -- cancelled. Run the command in a terminal, or pass the flag that answers it.", question)
}

// promptDefault prompts with "<message> [<def>]: " and returns def if
// the user enters nothing.
func (a *App) promptDefault(message, def string) string {
	answer := a.prompt(fmt.Sprintf("%s [%s]: ", message, def))
	if answer == "" {
		return def
	}
	return answer
}

// confirmYesDefaultNo mirrors `read -r -p "... [y/N]: "` + regex check.
func (a *App) confirmYesDefaultNo(message string) bool {
	answer := strings.ToLower(a.prompt(message + " [y/N]: "))
	return answer == "y" || answer == "yes"
}

// confirmYesDefaultYes mirrors `read -r -p "... [Y/n]: "`.
func (a *App) confirmYesDefaultYes(message string) bool {
	answer := strings.ToLower(a.prompt(message + " [Y/n]: "))
	return answer == "" || answer == "y" || answer == "yes"
}

// offer asks about an optional follow-up to work that has already
// finished ("Deploy now?", "Restart php-fpm now?"). Unlike the confirm
// helpers it never ends the command: with nobody to answer, the
// follow-up is simply not taken, and the caller prints how to do it
// later. Aborting there instead would report a command that succeeded
// as failed -- or, where the question sits between a write and its
// bookkeeping, stop in the middle.
func (a *App) offer(message string, defaultYes bool) bool {
	suffix := " [y/N]: "
	if defaultYes {
		suffix = " [Y/n]: "
	}
	answer, ok := a.readAnswer(message + suffix)
	if !ok {
		fmt.Fprintln(a.Err, "(no answer -- skipped)")
		return false
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true
	case "":
		return defaultYes
	}
	return false
}

// confirmYN is a stricter "[Y/n]" prompt (default yes on empty input):
// unlike confirmYesDefaultYes, which silently treats any unrecognized
// input as "no", this only accepts an empty answer, "Y"/"y", or "N"/"n"
// -- anything else prints an error and re-prompts, so a stray keystroke
// can't be misread as a real answer. Used by `wor domain remove`'s
// per-item Logs/Web Data/Backups prompts.
func (a *App) confirmYN(message string) bool {
	for {
		switch a.prompt(message + " [Y/n]: ") {
		case "", "Y", "y":
			return true
		case "N", "n":
			return false
		default:
			fmt.Fprintln(a.Err, "Please answer Y, y, N, or n.")
		}
	}
}

// requireTyped requires the user to type an exact confirmation word
// (e.g. "YES", "RESET"), matching the shell version's high-stakes
// confirmations for destructive operations.
func (a *App) requireTyped(message, word string) bool {
	return a.prompt(message) == word
}
