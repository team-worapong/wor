package cliapp

import (
	"fmt"
	"strings"
)

// prompt prints message to a.Err (matching the shell version's habit of
// sending prompts to stderr so stdout stays script-friendly) and reads
// one line of input from a.In.
func (a *App) prompt(message string) string {
	fmt.Fprint(a.Err, message)
	line, _ := a.In.ReadString('\n')
	return strings.TrimSpace(line)
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
