//go:build linux

package cliapp

// /proc readers behind cliapp's resource reporting (see resources.go).
// All best-effort and unprivileged: /proc/stat, /proc/meminfo, and
// other users' /proc/<pid>/stat + the VmRSS line of status are world-
// readable on a stock Linux, so none of this ever needs (or asks for)
// root -- the same non-interactive posture as diagnose/health overall.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// linuxClockTicks is USER_HZ, the unit of utime/stime in
// /proc/<pid>/stat. Fixed at 100 on every mainstream Linux (the
// kernel's ABI value exposed to userspace is a compile-time constant;
// getconf CLK_TCK returns 100 on all Debian/Ubuntu/RHEL kernels wor
// targets), so it's not worth shelling out to getconf for.
const linuxClockTicks = 100.0

// hostCPUSampleData carries one /proc/stat reading; math happens in
// hostCPUFromSamples so the sampling and the arithmetic stay separately
// testable.
type hostCPUSampleData struct {
	busy, total uint64
	cores       int
}

// hostCPUSample parses /proc/stat: the aggregate "cpu " line's fields
// (user nice system idle iowait irq softirq steal ...) plus a count of
// the per-core "cpuN" lines.
func hostCPUSample() (hostCPUSampleData, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return hostCPUSampleData{}, false
	}
	var s hostCPUSampleData
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			s.cores++
			continue
		}
		for i, f := range fields[1:] {
			n, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				break
			}
			s.total += n
			// fields 4 and 5 (0-indexed 3, 4 here) are idle and iowait;
			// everything else counts as busy.
			if i != 3 && i != 4 {
				s.busy += n
			}
		}
	}
	return s, s.total > 0
}

// hostCPUFromSamples turns two /proc/stat readings into a CPU% of
// total capacity (all cores -- top's summary-line convention).
func hostCPUFromSamples(first, second hostCPUSampleData) (known bool, pct float64, cores int) {
	if second.total <= first.total {
		return false, 0, second.cores
	}
	dTotal := second.total - first.total
	dBusy := second.busy - first.busy
	return true, float64(dBusy) / float64(dTotal) * 100, second.cores
}

// hostMemInfo reads MemTotal/MemAvailable (kB) from /proc/meminfo,
// returned in bytes. MemAvailable (kernel 3.14+) is the kernel's own
// "how much can be claimed without swapping" estimate -- the right
// "used = total - available" basis, unlike free-minus-buffers math.
func hostMemInfo() (totalBytes, availBytes int64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	get := func(key string) (int64, bool) {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, key+":") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, false
			}
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, false
			}
			return kb * 1024, true
		}
		return 0, false
	}
	total, ok1 := get("MemTotal")
	avail, ok2 := get("MemAvailable")
	return total, avail, ok1 && ok2
}

// pidsCPUTicks sums utime+stime (fields 14 and 15 of
// /proc/<pid>/stat) across pids. Vanished pids are skipped -- a pool
// worker recycling mid-sample costs a slightly-low reading, not an
// error.
func pidsCPUTicks(pids []int) uint64 {
	var sum uint64
	for _, pid := range pids {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			continue
		}
		// comm (field 2) can contain spaces and is parenthesized;
		// everything after the LAST ')' is space-separated, making
		// utime/stime fields 12 and 13 of that remainder (they are
		// fields 14/15 of the full line, and fields 3..13 precede them
		// after the paren).
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(s[i+1:])
		if len(fields) < 13 {
			continue
		}
		utime, err1 := strconv.ParseUint(fields[11], 10, 64)
		stime, err2 := strconv.ParseUint(fields[12], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		sum += utime + stime
	}
	return sum
}

// pidsRSSBytes sums VmRSS (from /proc/<pid>/status, kB) across pids.
func pidsRSSBytes(pids []int) int64 {
	var sum int64
	for _, pid := range pids {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "VmRSS:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					sum += kb * 1024
				}
			}
			break
		}
	}
	return sum
}

// ticksToCPUPercent converts a utime+stime tick delta over a measured
// wall-clock window into a CPU% (100% = one core busy throughout, same
// convention as pm2's monit.cpu and systemd's cpuPercentFromDelta).
func ticksToCPUPercent(deltaTicks uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(deltaTicks) / linuxClockTicks / elapsed.Seconds() * 100
}
