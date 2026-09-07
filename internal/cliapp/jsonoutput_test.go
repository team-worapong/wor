package cliapp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
	"wor/internal/worlock"
)

// newJSONTestApp builds an App over an initialized temporary workspace,
// so the commands under test get past Run's workspace and lock guards
// and reach their own code.
func newJSONTestApp(t *testing.T, out io.Writer) (*App, *config.Config) {
	t.Helper()
	home := t.TempDir()
	cfg := &config.Config{
		WorHome: home,
		Domains: filepath.Join(home, "domains"),
		Backups: filepath.Join(home, "backups"),
		Configs: filepath.Join(home, "configs"),
		Logs:    filepath.Join(home, "logs"),
		SSL:     filepath.Join(home, "ssl"),
	}
	for _, d := range []string{cfg.Domains, cfg.Backups, cfg.Configs, cfg.Logs, cfg.SSL} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &App{Cfg: cfg, Store: domainmodel.NewStore(cfg.Domains), Out: out, Err: io.Discard}, cfg
}

// decodeOneDocument is the check every --json test shares: stdout has to
// be exactly one JSON document and nothing else. A stray print before or
// after it is the failure mode this whole feature is built to avoid, and
// json.Decoder catches trailing content that json.Unmarshal would not.
func decodeOneDocument(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n--- stdout ---\n%s", err, stdout)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("stdout carries more than one document (trailing content):\n--- stdout ---\n%s", stdout)
	}
	if got, ok := doc["schema"].(float64); !ok || int(got) != SchemaVersion {
		t.Errorf("schema = %v, want %d -- readers check this before trusting field names", doc["schema"], SchemaVersion)
	}
	return doc
}

func TestVersionJSONIsOneDocumentWithTheDocumentedFields(t *testing.T) {
	var out bytes.Buffer
	app, _ := newJSONTestApp(t, &out)

	if code := app.Run([]string{"version", "--json"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	doc := decodeOneDocument(t, out.String())

	// These names are the published contract: renaming one is a schema
	// bump, not a refactor, so the test names them explicitly.
	for _, key := range []string{"product", "version", "release", "commit", "os", "build", "bin", "workspace_initialized"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("missing contract field %q in %v", key, doc)
		}
	}
	if doc["workspace_initialized"] != true {
		t.Errorf("workspace_initialized = %v, want true for an initialized workspace", doc["workspace_initialized"])
	}
}

func TestDoctorJSONReportsEveryCheckWithAKnownLevel(t *testing.T) {
	var out bytes.Buffer
	app, _ := newJSONTestApp(t, &out)

	app.Run([]string{"doctor", "--json"}) // exit code depends on the host; the shape does not
	doc := decodeOneDocument(t, out.String())

	if _, ok := doc["failed"].(bool); !ok {
		t.Errorf("failed = %v, want a bool restating the exit code", doc["failed"])
	}
	env, ok := doc["environment"].([]any)
	if !ok || len(env) == 0 {
		t.Fatalf("environment = %v, want a non-empty ordered list", doc["environment"])
	}
	// Ordered list, not a map: the order the fields print in is part of
	// the report, so each entry carries its own label.
	first, _ := env[0].(map[string]any)
	if first["label"] != "OS" {
		t.Errorf("first environment field = %v, want the OS line first", env[0])
	}

	checks, ok := doc["checks"].([]any)
	if !ok || len(checks) == 0 {
		t.Fatalf("checks = %v, want a non-empty list", doc["checks"])
	}
	for _, raw := range checks {
		c, _ := raw.(map[string]any)
		switch c["level"] {
		case "ok", "warn", "fail":
		default:
			t.Errorf("check level = %v, want ok/warn/fail: %v", c["level"], c)
		}
		if c["section"] == "" || c["message"] == "" {
			t.Errorf("every check needs a section and a message: %v", c)
		}
	}
}

// The whole point of the flag is that a reader never has to tell "wor
// printed nothing" apart from "wor failed", so every failure path owes
// stdout a document too.
func TestJSONFailuresStillPutOneDocumentOnStdout(t *testing.T) {
	t.Run("command has no --json form", func(t *testing.T) {
		var out, errOut bytes.Buffer
		app, _ := newJSONTestApp(t, &out)
		app.Err = &errOut

		if code := app.Run([]string{"service", "status", "--json"}); code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		doc := decodeOneDocument(t, out.String())
		if msg, _ := doc["error"].(string); !strings.Contains(msg, "--json") {
			t.Errorf("error = %q, want it to name the flag", msg)
		}
		// The advice belongs on stderr, and has to name the commands
		// that do support it -- otherwise the reader has to go looking.
		for _, want := range []string{"wor doctor --json", "wor health --json", "wor ssl status"} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("stderr should point at the supported reports; missing %q in:\n%s", want, errOut.String())
			}
		}
	})

	t.Run("workspace not initialized", func(t *testing.T) {
		var out bytes.Buffer
		home := t.TempDir() // deliberately left empty
		app := &App{
			Cfg: &config.Config{WorHome: home, Domains: filepath.Join(home, "domains")},
			Out: &out, Err: io.Discard,
		}
		if code := app.Run([]string{"health", "--json"}); code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		doc := decodeOneDocument(t, out.String())
		if msg, _ := doc["error"].(string); !strings.Contains(msg, "workspace") {
			t.Errorf("error = %q, want it to name the uninitialized workspace", msg)
		}
	})

	t.Run("another wor command holds the lock", func(t *testing.T) {
		var out bytes.Buffer
		app, cfg := newJSONTestApp(t, &out)

		held, err := worlock.Acquire(cfg.WorHome)
		if err != nil {
			t.Fatalf("could not take the lock the test needs held: %v", err)
		}
		defer held.Release()

		if code := app.Run([]string{"doctor", "--json"}); code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		decodeOneDocument(t, out.String())
	})
}

// `wor ssl status --json example/web` must read example/web as the
// target. Taking args[1] positionally made it read "--json" instead, so
// the flag could only ever be passed last.
func TestSSLStatusReadsTheTargetPastALeadingFlag(t *testing.T) {
	var out bytes.Buffer
	app, _ := newJSONTestApp(t, &out)

	if code := app.Run([]string{"ssl", "status", "--json", "nosuchhost"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for an unknown host", code)
	}
	doc := decodeOneDocument(t, out.String())
	msg, _ := doc["error"].(string)
	if !strings.Contains(msg, "nosuchhost") {
		t.Errorf("error = %q, want the target after the flag to be the one looked up", msg)
	}
	if strings.Contains(msg, "--json") {
		t.Errorf("error = %q -- the flag was taken for the host name", msg)
	}
}

func TestReporterRendersTheCheckListTheWayItAlwaysHas(t *testing.T) {
	var out bytes.Buffer
	r := &reporter{out: &out}

	r.Section("Environment")
	r.Field("OS", "Linux")
	r.Field("Host Provider", "nginx")
	r.OK("Workspace initialized")
	r.Warn("PM2 not installed")
	if !r.Fail("Go not installed") {
		t.Error("Fail must return true so callers can fold it into their fail flag")
	}

	want := "Environment\n" +
		"  OS            : Linux\n" +
		"  Host Provider : nginx\n" +
		"  ✓ Workspace initialized\n" +
		"  ⚠ PM2 not installed\n" +
		"  ✗ Go not installed\n"
	if out.String() != want {
		t.Errorf("text output changed.\n got:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestReporterAttachesNotesToThePrecedingCheck(t *testing.T) {
	r := &reporter{out: io.Discard}

	// A note before any check has nothing to belong to and must not
	// invent a check to hang itself on.
	r.Note("orphan advice")
	if len(r.Checks()) != 0 {
		t.Fatalf("a note with no preceding check must not be recorded, got %v", r.Checks())
	}

	r.Section("Security")
	r.Warn(".env file(s) readable by any account on this machine -- 1 found:")
	r.Item("/srv/wor/domains/example/web/.env")
	r.Note("Fix: chmod 640")
	r.OK("No world-readable .env files found under WOR_HOME")

	checks := r.Checks()
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2: %v", len(checks), checks)
	}
	if got := checks[0].Notes; len(got) != 2 || got[0] != "/srv/wor/domains/example/web/.env" || got[1] != "Fix: chmod 640" {
		t.Errorf("notes = %v, want the path and the fix attached to the warning they follow", got)
	}
	if checks[1].Notes != nil {
		t.Errorf("the later check must not inherit the earlier one's notes, got %v", checks[1].Notes)
	}
	if checks[0].Section != "Security" {
		t.Errorf("section = %q, want Security", checks[0].Section)
	}
}

// In JSON mode the reporter writes nowhere: anything it printed would
// land on stdout beside the document and corrupt it.
func TestReporterInJSONModePrintsNothing(t *testing.T) {
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard}

	r := app.newReporter(true)
	r.Line("WOR Doctor")
	r.Section("Runtimes")
	r.Field("OS", "Linux")
	r.OK("Node.js v22.0.0")
	r.Note("Fix: install it")

	if out.Len() != 0 {
		t.Errorf("wrote %q to stdout in JSON mode; the document must be the only thing there", out.String())
	}
	if len(r.Checks()) != 1 || len(r.Fields()) != 1 {
		t.Errorf("JSON mode still has to record: checks=%v fields=%v", r.Checks(), r.Fields())
	}
}

// Fields and Checks are what the report marshals, and a reader
// iterating them should meet [] rather than null when nothing was
// recorded.
func TestReporterAccessorsAreNeverNil(t *testing.T) {
	r := &reporter{out: io.Discard}
	if r.Fields() == nil || r.Checks() == nil {
		t.Error("empty reporter must return empty slices, not nil")
	}
	data, err := json.Marshal(doctorReport{Schema: SchemaVersion, Environment: r.Fields(), Checks: r.Checks()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("empty report marshalled to %s, want empty arrays", data)
	}
}
