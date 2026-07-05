package cliapp

import (
	"fmt"
	"os"
	"time"

	"wor/internal/osutil"
)

// ANSI color codes used by `wor service status` and `wor host list`'s
// grouped output. Kept to a small, semantic set (group header, ok,
// fail, muted) rather than a general-purpose palette.
const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiGray   = "\x1b[90m"
	ansiPink   = "\x1b[35m"

	// ansiDim is the SGR "faint" attribute (2), applied on top of the
	// terminal's own default foreground rather than a fixed "bright
	// black" color code (ansiGray). ansiGray renders low-contrast to the
	// point of being hard to read on some terminal themes -- bright
	// black is defined independently per-theme and isn't guaranteed to
	// sit at a readable distance from the background. Dimming the
	// default foreground instead lets the terminal's own theme decide
	// what "muted" looks like, which stays legible across both light and
	// dark schemes. Used for secondary/sub-line text (e.g. the
	// proc-name/cpu/mem line under a pm2/systemd row) that should read
	// as de-emphasized, not as a distinct gray hue.
	ansiDim = "\x1b[2m"
)

// colorEnabled reports whether status-style output should use ANSI
// color: only when writing to an interactive terminal, and only if the
// user hasn't opted out via NO_COLOR (https://no-color.org), the de
// facto standard also respected by git, npm, and most modern CLIs.
// Piping output (`wor service status | cat`, writing to a log file,
// etc.) always gets the plain fallback.
func (a *App) colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := a.Out.(*os.File)
	if !ok {
		return false
	}
	return osutil.IsTerminal(f)
}

// colorize wraps s in code/reset when useColor is true, otherwise
// returns s unchanged.
func colorize(useColor bool, code, s string) string {
	if !useColor {
		return s
	}
	return code + s + ansiReset
}

// tag renders a small status indicator: a colored glyph on a TTY, or a
// plain bracketed word when color is unavailable (piped output,
// NO_COLOR, non-TTY) -- mirroring how git/npm/docker fall back to
// plain text markers instead of silently dropping the information ANSI
// color would otherwise carry.
func tag(useColor bool, code, glyph, plain string) string {
	if useColor {
		return colorize(true, code, glyph)
	}
	return plain
}

// formatPercent renders a CPU percentage the way `top`/pm2's own status
// display do: one decimal place, e.g. "0.4%".
func formatPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}

// formatBytes renders a byte count as the coarsest binary (1024-based)
// unit that keeps it readable -- "512b", "48mb", "1.2gb" -- matching
// the units pm2/docker-style tools use for RSS/memory figures.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fgb", float64(n)/float64(int64(1)<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%dmb", n/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dkb", n/(1<<10))
	default:
		return fmt.Sprintf("%db", n)
	}
}

// formatUptime renders a duration the way `pm2 status` does: the
// coarsest one or two units that make it readable ("3d 4h", "2h 10m",
// "45m"), not a precise stopwatch reading.
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	totalMinutes := int(d.Minutes())
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes / 60) % 24
	minutes := totalMinutes % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
