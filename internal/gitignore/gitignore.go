// Package gitignore implements a small, dependency-free matcher for
// gitignore-format pattern files, used by `wor source backup`
// (internal/cliapp/source.go) to optionally skip files a project's own
// .gitignore already excludes from version control.
//
// Scope: this package only ever reads ONE .gitignore file, at the root
// of the directory tree being matched (the domain or service dir being
// zipped) -- it does not walk into subdirectories looking for nested
// .gitignore files the way `git` itself does, and it does not consult
// .git/info/exclude or any global excludesfile. That's a deliberate
// simplification (see project history 2026-07-05): full git semantics
// would need per-directory pattern layering and priority rules that
// aren't worth the complexity for a backup-exclusion feature. Within
// that root-only scope, pattern syntax matches git as closely as
// practical: comments, blank lines, "!" negation, a leading "/" (or any
// non-trailing "/") anchoring a pattern to the root instead of matching
// at any depth, a trailing "/" restricting a pattern to directories,
// and "*"/"?"/"[...]" /"**" wildcards.
package gitignore

import (
	"os"
	"path/filepath"
	"strings"
)

// rule is one parsed line of a .gitignore file.
type rule struct {
	negate  bool     // line started with "!"
	dirOnly bool     // line ended with "/" (only matches directories)
	segs    []string // pattern split on "/"; a leading "**" segment means "unanchored" (may start matching at any depth)
}

// Matcher answers "is this path ignored?" for one .gitignore file's
// worth of rules.
type Matcher struct {
	rules []rule
}

// Load reads path (typically "<dir>/.gitignore") and returns a Matcher.
// A missing file is not an error -- it returns (nil, nil), meaning "no
// filtering to apply", since not every backed-up directory has a
// .gitignore at all.
func Load(path string) (*Matcher, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return Parse(string(data)), nil
}

// Parse builds a Matcher from gitignore-format text (the contents of a
// .gitignore file).
func Parse(data string) *Matcher {
	m := &Matcher{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}

		negate := false
		switch {
		case strings.HasPrefix(line, "#"):
			continue // comment
		case strings.HasPrefix(line, "\\#"), strings.HasPrefix(line, "\\!"):
			line = line[1:] // escaped literal '#'/'!' as the first character
		case strings.HasPrefix(line, "!"):
			negate = true
			line = line[1:]
		}
		if line == "" {
			continue
		}

		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}

		// A pattern is anchored to the gitignore's directory if it
		// contains a "/" anywhere before the end (leading or interior);
		// only a pattern with no interior slash at all may match
		// starting at any depth below the root.
		anchored := strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")

		segs := strings.Split(line, "/")
		if !anchored {
			segs = append([]string{"**"}, segs...)
		}
		m.rules = append(m.rules, rule{negate: negate, dirOnly: dirOnly, segs: segs})
	}
	return m
}

// Match reports whether relPath (slash-separated, relative to the
// Matcher's .gitignore directory) is ignored. isDir must reflect
// whether relPath itself names a directory, since "foo/" style patterns
// only ever match directories.
//
// Later rules override earlier ones on a match, mirroring git's own
// "last matching pattern wins" behavior -- this is what lets a
// .gitignore un-ignore a specific path with "!" after a broader
// preceding pattern excluded it.
func (m *Matcher) Match(relPath string, isDir bool) bool {
	if m == nil || len(m.rules) == 0 {
		return false
	}
	relPath = filepath.ToSlash(strings.Trim(relPath, "/"))
	if relPath == "" {
		return false
	}
	pathSegs := strings.Split(relPath, "/")

	ignored := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		if matchSegments(r.segs, pathSegs) {
			ignored = !r.negate
		}
	}
	return ignored
}

// matchSegments matches a gitignore pattern (already split on "/", with
// an unanchored pattern's implicit leading "**" already applied) against
// a candidate path's segments. "**" consumes zero or more path segments;
// every other segment is matched with filepath.Match, which already
// supports "*"/"?"/"[...]" without crossing a "/" boundary.
func matchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(path); i++ {
			if matchSegments(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	ok, err := filepath.Match(pattern[0], path[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], path[1:])
}
