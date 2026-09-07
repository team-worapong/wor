// Package-level note: this file holds everything shared by wor's
// machine-readable (`--json`) output. See DESIGN.md section 23.
package cliapp

import (
	"encoding/json"
	"fmt"
	"io"
)

// SchemaVersion is stamped into every document `--json` produces.
//
// It exists because wor and whatever reads its JSON are installed and
// upgraded independently -- the WOR HCP dashboard is the first such
// reader -- so a reader regularly meets a wor older or newer than the
// one it was written against and has no other way to tell. Bump it when
// an existing field changes meaning or disappears. Adding a new field
// does not need a bump: a reader that does not know a field ignores it.
//
// Once a payload is published, its field names are a contract. That is
// the reason `--json` is deliberately limited to the read-only reports
// listed in supportsJSON rather than offered on every subcommand: each
// one added is a shape that has to keep working.
const SchemaVersion = 1

// reportField is one "Label : Value" line of a report's header block.
// Recorded as an ordered list rather than a map because the order the
// fields are printed in is itself part of the report.
type reportField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// reportCheck is one ✓/⚠/✗ checklist line.
//
// Message is the whole line as it is printed, not split into a name and
// a detail. Splitting it would mean inventing a structure the text does
// not have -- the lines are free-form ("Node.js v22.1.0", "PHP-FPM not
// detected ...") -- and a reader that wants to display the check wants
// exactly what the operator would have read in the terminal.
type reportCheck struct {
	Section string   `json:"section"`
	Level   string   `json:"level"` // "ok", "warn" or "fail"
	Message string   `json:"message"`
	Notes   []string `json:"notes,omitempty"`
}

// reporter renders a checklist-shaped command (`wor doctor`,
// `wor version`) and records it at the same time, so one run can be
// printed as text or marshalled as JSON without the command being
// written twice.
//
// A command using it prints *everything* through it and never touches
// a.Out directly. That is what makes --json safe here: constructing the
// reporter with io.Discard is the only thing needed to silence the
// text, so there is no per-line "if json" test left to forget at one
// call site out of fifty -- and no stray line to corrupt the single
// JSON document stdout is supposed to carry.
type reporter struct {
	out     io.Writer
	color   bool
	section string

	fields []reportField
	checks []reportCheck
}

// newReporter builds the reporter for one command run. In JSON mode it
// writes nowhere: the recorded fields and checks are the output.
func (a *App) newReporter(jsonMode bool) *reporter {
	if jsonMode {
		return &reporter{out: io.Discard}
	}
	return &reporter{out: a.Out, color: a.colorEnabled()}
}

// Line prints a heading, rule, or other decoration. Deliberately not
// recorded: JSON readers get the structure these lines decorate, and a
// row of "=" characters is not information.
func (r *reporter) Line(format string, args ...interface{}) {
	fmt.Fprintf(r.out, format+"\n", args...)
}

// Blank prints a separating blank line.
func (r *reporter) Blank() { fmt.Fprintln(r.out) }

// Section prints a block heading and labels every check recorded from
// here until the next Section call.
func (r *reporter) Section(name string) {
	fmt.Fprintln(r.out, name)
	r.section = name
}

// Field prints and records one "Label : Value" line of a header block.
func (r *reporter) Field(label, value string) {
	fmt.Fprintf(r.out, "  %-14s: %s\n", label, value)
	r.fields = append(r.fields, reportField{Label: label, Value: value})
}

// status prints one ✓/⚠/✗ line, colored green/yellow/red on a TTY
// (plain glyph, no color, otherwise -- see colorEnabled in
// statusview.go), and records it under the current section. The glyph
// itself always prints regardless of color support, since it carries
// meaning on its own.
func (r *reporter) status(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	var glyph, code string
	switch level {
	case "ok":
		glyph, code = "✓", ansiGreen // ✓
	case "warn":
		glyph, code = "⚠", ansiYellow // ⚠
	default:
		glyph, code = "✗", ansiRed // ✗
	}
	fmt.Fprintf(r.out, "  %s %s\n", colorize(r.color, code, glyph), msg)
	r.checks = append(r.checks, reportCheck{Section: r.section, Level: level, Message: msg})
}

func (r *reporter) OK(format string, args ...interface{})   { r.status("ok", format, args...) }
func (r *reporter) Warn(format string, args ...interface{}) { r.status("warn", format, args...) }

// Fail prints a ✗ line and always returns true, so callers combine it
// into their running fail flag with `fail = r.Fail(...) || fail`.
func (r *reporter) Fail(format string, args ...interface{}) bool {
	r.status("fail", format, args...)
	return true
}

// Item prints an indented continuation line under the check just
// recorded -- one path out of a list the check counted -- and attaches
// it to that check.
func (r *reporter) Item(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(r.out, "      %s\n", msg)
	r.attach(msg)
}

// Note prints an "[INFO] ..." line explaining or repairing the check
// just recorded, and attaches it to that check -- so a reader showing
// the check can show the fix alongside it instead of dropping the half
// of the report that says what to do about the problem.
func (r *reporter) Note(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(r.out, "[INFO] %s\n", msg)
	r.attach(msg)
}

// attach appends text to the most recently recorded check. A note with
// no check before it is still printed but not recorded: there is
// nothing for it to belong to, and inventing a placeholder check would
// put a line in the JSON that never appeared in the text.
func (r *reporter) attach(text string) {
	if len(r.checks) == 0 {
		return
	}
	last := &r.checks[len(r.checks)-1]
	last.Notes = append(last.Notes, text)
}

// Fields and Checks return what the run recorded. Never nil: a reader
// iterating the JSON should find an empty list, not null, when a
// section produced nothing.
func (r *reporter) Fields() []reportField {
	if r.fields == nil {
		return []reportField{}
	}
	return r.fields
}

func (r *reporter) Checks() []reportCheck {
	if r.checks == nil {
		return []reportCheck{}
	}
	return r.checks
}

// writeJSON prints v as the single JSON document a --json command
// produces. Indented, because a person debugging the contract reads
// this in a terminal and every reader of it parses either form.
func (a *App) writeJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.Out, "%s\n", data)
	return err
}

// jsonErrorDocument is what stdout carries when a --json command fails
// instead of producing its report, so that a reader can always parse
// stdout as one JSON document and never has to tell "wor failed" apart
// from "wor printed nothing". The human-readable ERROR line still goes
// to stderr as it always has.
type jsonErrorDocument struct {
	Schema int    `json:"schema"`
	Error  string `json:"error"`
}

func (a *App) writeJSONError(message string) {
	// Deliberately ignoring the write error: this is already the
	// failure path, and there is nowhere better to report it to.
	_ = a.writeJSON(jsonErrorDocument{Schema: SchemaVersion, Error: message})
}

// supportsJSON reports whether cmd/rest has a --json form.
//
// An allowlist, not a default: every payload wor publishes is a shape
// it has to keep working (see SchemaVersion), and the commands left out
// are left out for reasons that do not expire. Mutating commands
// (`service restart`, `ssl issue`, `deploy`) give a reader an exit code
// and streamed progress, which is what driving them actually needs --
// a document at the end describes nothing useful about a job that was
// watched. Interactive commands (`create`, `setup`) have no output
// shape at all, only a conversation; automating those needs the
// non-interactive commands they wrap, not a flag here.
func supportsJSON(cmd string, rest []string) bool {
	switch cmd {
	case "version", "--version", "-v", "doctor", "health":
		return true
	case "ssl":
		return len(rest) > 0 && rest[0] == "status"
	}
	return false
}
