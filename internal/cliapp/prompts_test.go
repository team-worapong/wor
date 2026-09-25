package cliapp

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promptApp is an App whose questions read from input.
func promptApp(input string) (*App, *bytes.Buffer) {
	var errOut bytes.Buffer
	return &App{Out: &bytes.Buffer{}, Err: &errOut, In: bufio.NewReader(strings.NewReader(input))}, &errOut
}

// aborted runs fn and reports whether it ended with promptAbort, the
// way App.Run would see it.
func aborted(t *testing.T, fn func()) (reason string, ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			abort, isAbort := r.(promptAbort)
			if !isAbort {
				panic(r)
			}
			reason, ok = abort.reason, true
		}
	}()
	fn()
	return "", false
}

// The bug this whole change exists for: with no stdin, prompt used to
// return "" and every default-yes question read that as Enter.
func TestPromptWithNoInputEndsTheCommandInsteadOfTakingTheDefault(t *testing.T) {
	for name, ask := range map[string]func(*App){
		"confirmYesDefaultYes": func(a *App) { a.confirmYesDefaultYes("Proceed?") },
		"confirmYN":            func(a *App) { a.confirmYN("Remove web data?") },
		"promptDefault":        func(a *App) { a.promptDefault("Choose", "1") },
		"requireTyped":         func(a *App) { a.requireTyped("Type YES: ", "YES") },
	} {
		t.Run(name, func(t *testing.T) {
			app, _ := promptApp("")
			reason, ok := aborted(t, func() { ask(app) })
			if !ok {
				t.Fatal("a question nobody answered was given an answer")
			}
			if !strings.Contains(reason, "stdin closed") {
				t.Errorf("reason = %q, want it to say stdin closed", reason)
			}
		})
	}
}

// Enter is still an answer: only the absence of any input aborts.
func TestPromptEnterStillMeansTheDefault(t *testing.T) {
	app, _ := promptApp("\n")
	if !app.confirmYesDefaultYes("Proceed?") {
		t.Error("an empty line must still take the [Y/n] default")
	}
}

// A last line with no trailing newline -- `printf y | wor ...` -- is
// input, not EOF.
func TestPromptFinalLineWithoutNewlineIsAnAnswer(t *testing.T) {
	app, _ := promptApp("n")
	if _, ok := aborted(t, func() {
		if app.confirmYesDefaultYes("Proceed?") {
			t.Error(`"n" without a newline was read as yes`)
		}
	}); ok {
		t.Fatal("a final line without a newline was treated as no input")
	}
}

// Piped answers keep working until they run out, and only then abort.
func TestPromptAbortsWhenPipedAnswersRunOut(t *testing.T) {
	app, _ := promptApp("y\n")
	reason, ok := aborted(t, func() {
		app.confirmYN("Remove backups?")
		app.confirmYN("Remove logs?")
	})
	if !ok || !strings.Contains(reason, "Remove logs?") {
		t.Fatalf("aborted = %v, reason = %q; want an abort naming the unanswered question", ok, reason)
	}
}

// --non-interactive must fail at the first question without consuming
// input, even when some is available: a program passing the flag has
// said it will not answer, so whatever is on stdin is not an answer.
func TestNonInteractiveAbortsWithoutReadingInput(t *testing.T) {
	app, _ := promptApp("y\n")
	app.NonInteractive = true
	reason, ok := aborted(t, func() { app.confirmYesDefaultYes("Proceed?") })
	if !ok {
		t.Fatal("--non-interactive answered a question")
	}
	if !strings.Contains(reason, "non-interactively") {
		t.Errorf("reason = %q, want it to name non-interactive mode", reason)
	}
	if rest, _ := app.In.ReadString('\n'); rest != "y\n" {
		t.Errorf("input was consumed: %q left", rest)
	}
}

// offer is for follow-ups to finished work: no answer skips, it never
// aborts, whatever its default.
func TestOfferWithNoInputSkipsWithoutAborting(t *testing.T) {
	for _, defaultYes := range []bool{true, false} {
		app, errOut := promptApp("")
		if _, ok := aborted(t, func() {
			if app.offer("Deploy now?", defaultYes) {
				t.Errorf("offer(defaultYes=%v) with no input took the follow-up", defaultYes)
			}
		}); ok {
			t.Fatalf("offer(defaultYes=%v) aborted the command", defaultYes)
		}
		if !strings.Contains(errOut.String(), "skipped") {
			t.Errorf("offer should say it skipped, got %q", errOut.String())
		}
	}
}

func TestOfferAnswers(t *testing.T) {
	for input, want := range map[string]bool{"\n": true, "y\n": true, "n\n": false, "maybe\n": false} {
		app, _ := promptApp(input)
		if got := app.offer("Restart php-fpm now?", true); got != want {
			t.Errorf("offer(%q) = %v, want %v", input, got, want)
		}
	}
}

// The elevation question declines rather than aborts, so the privileged
// operation's own error path -- which restores what it changed -- runs.
func TestElevationWithNoInputIsDeclinedNotAborted(t *testing.T) {
	for _, nonInteractive := range []bool{false, true} {
		app, _ := promptApp("")
		app.NonInteractive = nonInteractive
		if _, ok := aborted(t, func() {
			if app.answerElevation("run 'systemctl' with elevated (sudo) privileges") {
				t.Errorf("elevation granted with nobody to answer (non-interactive=%v)", nonInteractive)
			}
		}); ok {
			t.Fatalf("elevation question aborted the command (non-interactive=%v)", nonInteractive)
		}
	}
	app, _ := promptApp("\n")
	if !app.answerElevation("run 'systemctl' with elevated (sudo) privileges") {
		t.Error("Enter must still grant elevation")
	}
}

func TestTakeNonInteractive(t *testing.T) {
	t.Setenv("WOR_NONINTERACTIVE", "")
	args, on := takeNonInteractive([]string{"service", "restart", "--non-interactive", "shop/api"})
	if !on || strings.Join(args, " ") != "service restart shop/api" {
		t.Errorf("got %v, %v; want the flag found and removed", args, on)
	}
	// Before the command too, since it is global.
	if args, on := takeNonInteractive([]string{"--non-interactive", "create"}); !on || len(args) != 1 || args[0] != "create" {
		t.Errorf("got %v, %v; want [create], true", args, on)
	}
	if _, on := takeNonInteractive([]string{"health"}); on {
		t.Error("non-interactive without the flag or the variable")
	}
	t.Setenv("WOR_NONINTERACTIVE", "1")
	if _, on := takeNonInteractive([]string{"health"}); !on {
		t.Error("WOR_NONINTERACTIVE=1 was ignored")
	}
}

// End to end through Run, on the command where the old behaviour was
// destructive: `wor domain remove` with no stdin used to answer yes to
// Backups, Logs and Web Data, and delete all three.
func TestRunDomainRemoveWithNoInputDeletesNothing(t *testing.T) {
	for name, args := range map[string][]string{
		"stdin closed":    {"domain", "remove", "shop-example"},
		"non-interactive": {"domain", "remove", "shop-example", "--non-interactive"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WOR_NONINTERACTIVE", "")
			var errOut bytes.Buffer
			app, _ := newJSONTestApp(t, &bytes.Buffer{})
			app.Err = &errOut
			// Answers are available, and must not be used under
			// --non-interactive; with stdin closed there are none.
			input := "y\ny\ny\n"
			if name == "stdin closed" {
				input = ""
			}
			app.In = bufio.NewReader(strings.NewReader(input))
			logsDir, backupsDir := setupDomainWithLogsAndBackups(t, app, "shop-example")

			if code := app.Run(args); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(errOut.String(), "ERROR: ") || !strings.Contains(errOut.String(), "Remove backups") {
				t.Errorf("stderr should carry an ERROR naming the first unanswered question, got:\n%s", errOut.String())
			}
			for _, dir := range []string{backupsDir, logsDir, app.Store.DomainDir("shop-example")} {
				if _, err := os.Stat(dir); err != nil {
					t.Errorf("%s was removed: %v", filepath.Base(dir), err)
				}
			}
		})
	}
}
