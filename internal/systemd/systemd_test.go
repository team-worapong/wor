package systemd

import (
	"testing"
	"time"
)

func TestParseSampleActive(t *testing.T) {
	out := "ActiveState=active\nMainPID=883\nCPUUsageNSec=1500000000\nMemoryCurrent=52428800\n"
	s := parseSample(out)
	if s.state != "active" {
		t.Errorf("state = %q, want active", s.state)
	}
	if s.pid != 883 {
		t.Errorf("pid = %d, want 883", s.pid)
	}
	if !s.cpuKnown || s.cpuNSec != 1500000000 {
		t.Errorf("cpu = (%v, known=%v), want (1500000000, true)", s.cpuNSec, s.cpuKnown)
	}
	if !s.memKnown || s.memBytes != 52428800 {
		t.Errorf("mem = (%v, known=%v), want (52428800, true)", s.memBytes, s.memKnown)
	}
}

func TestParseSampleAccountingDisabled(t *testing.T) {
	// A unit installed before CPUAccounting/MemoryAccounting were added
	// to unitFileContent() reports "[not set]" for both properties.
	out := "ActiveState=active\nMainPID=42\nCPUUsageNSec=[not set]\nMemoryCurrent=[not set]\n"
	s := parseSample(out)
	if s.cpuKnown {
		t.Error("expected cpuKnown = false for '[not set]'")
	}
	if s.memKnown {
		t.Error("expected memKnown = false for '[not set]'")
	}
}

func TestParseSampleSentinelMax(t *testing.T) {
	// systemd's UINT64_MAX sentinel for "unbounded"/"unknown".
	out := "ActiveState=inactive\nMainPID=0\nCPUUsageNSec=18446744073709551615\nMemoryCurrent=18446744073709551615\n"
	s := parseSample(out)
	if s.cpuKnown || s.memKnown {
		t.Error("expected both cpuKnown and memKnown = false for the UINT64_MAX sentinel")
	}
}

func TestParseSampleEmpty(t *testing.T) {
	s := parseSample("")
	if s.state != "" || s.pid != 0 || s.cpuKnown || s.memKnown {
		t.Errorf("expected zero-value sample for empty input, got %+v", s)
	}
}

func TestParseSystemdUint(t *testing.T) {
	cases := []struct {
		in      string
		want    uint64
		wantOK  bool
	}{
		{"1500000000", 1500000000, true},
		{"0", 0, true},
		{"[not set]", 0, false},
		{"", 0, false},
		{"18446744073709551615", 0, false}, // math.MaxUint64 sentinel
		{"not-a-number", 0, false},
	}
	for _, c := range cases {
		got, ok := parseSystemdUint(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("parseSystemdUint(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestCPUPercentFromDelta(t *testing.T) {
	interval := 200 * time.Millisecond

	// Half a core's worth of CPU time consumed over the interval -> 50%.
	first := sampleShowProperties{cpuKnown: true, cpuNSec: 1_000_000_000}
	second := sampleShowProperties{cpuKnown: true, cpuNSec: 1_000_000_000 + uint64(interval.Nanoseconds())/2}
	pct, ok := cpuPercentFromDelta(first, second, interval)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if pct < 49 || pct > 51 {
		t.Errorf("pct = %v, want ~50", pct)
	}
}

func TestCPUPercentFromDeltaUnknown(t *testing.T) {
	interval := 200 * time.Millisecond
	known := sampleShowProperties{cpuKnown: true, cpuNSec: 100}
	unknown := sampleShowProperties{cpuKnown: false}
	if _, ok := cpuPercentFromDelta(unknown, known, interval); ok {
		t.Error("expected ok = false when the first sample lacks CPU accounting")
	}
	if _, ok := cpuPercentFromDelta(known, unknown, interval); ok {
		t.Error("expected ok = false when the second sample lacks CPU accounting")
	}
}

func TestCPUPercentFromDeltaWentBackwards(t *testing.T) {
	interval := 200 * time.Millisecond
	first := sampleShowProperties{cpuKnown: true, cpuNSec: 1_000_000}
	second := sampleShowProperties{cpuKnown: true, cpuNSec: 500_000} // e.g. the unit restarted between samples
	if _, ok := cpuPercentFromDelta(first, second, interval); ok {
		t.Error("expected ok = false when the second sample's CPU usage is behind the first")
	}
}
