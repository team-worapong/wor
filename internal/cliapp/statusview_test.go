package cliapp

import (
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-5 * time.Minute, ""},
		{45 * time.Minute, "45m"},
		{2*time.Hour + 10*time.Minute, "2h 10m"},
		{3*24*time.Hour + 4*time.Hour, "3d 4h"},
	}
	for _, c := range cases {
		if got := formatUptime(c.d); got != c.want {
			t.Errorf("formatUptime(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestColorize(t *testing.T) {
	if got := colorize(false, ansiGreen, "ok"); got != "ok" {
		t.Errorf("colorize(false, ...) = %q, want plain %q", got, "ok")
	}
	want := ansiGreen + "ok" + ansiReset
	if got := colorize(true, ansiGreen, "ok"); got != want {
		t.Errorf("colorize(true, ...) = %q, want %q", got, want)
	}
}

func TestTag(t *testing.T) {
	if got := tag(false, ansiGreen, "●", "[ok]"); got != "[ok]" {
		t.Errorf("tag(false, ...) = %q, want %q", got, "[ok]")
	}
	want := ansiGreen + "●" + ansiReset
	if got := tag(true, ansiGreen, "●", "[ok]"); got != want {
		t.Errorf("tag(true, ...) = %q, want %q", got, want)
	}
}
