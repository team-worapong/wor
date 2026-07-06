//go:build !linux

package cliapp

// Non-Linux stubs for resources_linux.go's /proc readers: host and
// php-pool usage read "unknown" (rendered as "-"), while pm2- and
// systemd-sourced numbers in resources.go still work wherever those
// providers do. Linux is the production target; macOS is a development
// convenience (same split as permcheck_windows.go).

import "time"

type hostCPUSampleData struct{}

func hostCPUSample() (hostCPUSampleData, bool) { return hostCPUSampleData{}, false }

func hostCPUFromSamples(first, second hostCPUSampleData) (known bool, pct float64, cores int) {
	return false, 0, 0
}

func hostMemInfo() (totalBytes, availBytes int64, ok bool) { return 0, 0, false }

func pidsCPUTicks(pids []int) uint64 { return 0 }

func pidsRSSBytes(pids []int) int64 { return 0 }

func ticksToCPUPercent(deltaTicks uint64, elapsed time.Duration) float64 { return 0 }
