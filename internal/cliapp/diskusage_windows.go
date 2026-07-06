//go:build windows

package cliapp

// diskUsagePercent is not implemented on Windows (syscall.Statfs does
// not exist there and the GetDiskFreeSpaceEx dance isn't worth the
// win for an advisory check) -- `wor diagnose` prints its disk row as
// "not checked" instead, the same graceful-skip convention its other
// platform-gated checks use.
func diskUsagePercent(path string) (int, bool) { return 0, false }

func diskUsageBytes(path string) (used, total int64, ok bool) { return 0, 0, false }
