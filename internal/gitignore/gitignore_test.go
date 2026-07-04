package gitignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchBasics(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		path    string
		isDir   bool
		want    bool
	}{
		{"plain name matches at root", "node_modules", "node_modules", true, true},
		{"plain name matches at any depth", "node_modules", "src/api/node_modules", true, true},
		{"plain name does not match a different name", "node_modules", "other_modules", true, false},
		{"wildcard extension anywhere", "*.log", "logs/debug.log", false, true},
		{"wildcard extension no match", "*.log", "logs/debug.txt", false, false},
		{"leading slash anchors to root", "/build", "build", true, true},
		{"leading slash does not match nested", "/build", "src/build", true, false},
		{"middle slash anchors to root", "src/build", "src/build", true, true},
		{"middle slash does not match nested elsewhere", "src/build", "other/src/build", true, false},
		{"trailing slash matches directory", "dist/", "dist", true, true},
		{"trailing slash does not match a file of the same name", "dist/", "dist", false, false},
		{"double-star matches across levels", "foo/**/bar", "foo/x/y/bar", false, true},
		{"double-star matches zero levels", "foo/**/bar", "foo/bar", false, true},
		{"character class", "*.[ot]xt", "notes.txt", false, true},
		{"comment line ignored", "# not a pattern", "# not a pattern", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Parse(c.pattern)
			if got := m.Match(c.path, c.isDir); got != c.want {
				t.Errorf("pattern %q vs path %q (isDir=%v) = %v, want %v", c.pattern, c.path, c.isDir, got, c.want)
			}
		})
	}
}

func TestMatchNegationOverridesEarlierRule(t *testing.T) {
	m := Parse("*.log\n!important.log\n")
	if !m.Match("debug.log", false) {
		t.Error("debug.log should be ignored by *.log")
	}
	if m.Match("important.log", false) {
		t.Error("important.log should be un-ignored by the later !important.log rule")
	}
}

func TestMatchLaterPatternWins(t *testing.T) {
	// Git's rule: the LAST matching pattern in the file decides the
	// outcome, not the first.
	m := Parse("!keep.txt\n*.txt\n")
	if !m.Match("keep.txt", false) {
		t.Error("a later *.txt should re-ignore keep.txt even though an earlier rule un-ignored it")
	}
}

func TestMatchBlankAndCommentLinesIgnored(t *testing.T) {
	m := Parse("\n# comment\n\n*.tmp\n")
	if !m.Match("scratch.tmp", false) {
		t.Error("*.tmp should still match after blank lines/comments")
	}
}

func TestMatchNilMatcher(t *testing.T) {
	var m *Matcher
	if m.Match("anything", false) {
		t.Error("a nil Matcher must never report a match")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "does-not-exist", ".gitignore"))
	if err != nil {
		t.Fatalf("Load on a missing file: unexpected error: %v", err)
	}
	if m != nil {
		t.Error("Load on a missing file should return a nil Matcher")
	}
}

func TestLoadReadsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.Match("debug.log", false) {
		t.Error("expected the loaded .gitignore's *.log rule to match")
	}
}
