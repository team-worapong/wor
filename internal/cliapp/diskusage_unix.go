//go:build !windows

package cliapp

import "syscall"

// diskUsagePercent reports how full the filesystem holding path is
// (0-100), for `wor diagnose`'s disk check. Uses syscall.Statfs
// directly (present on both Linux and macOS with the fields this
// needs) rather than pulling in a third-party disk library --
// Dependency Policy. The uint64 conversions cover the small
// signed/unsigned type differences between the two platforms'
// Statfs_t definitions.
func diskUsagePercent(path string) (int, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, false
	}
	used := float64(uint64(st.Blocks)-uint64(st.Bavail)) / float64(st.Blocks) * 100
	return int(used + 0.5), true
}

// diskUsageBytes is diskUsagePercent with absolute numbers, for `wor
// health`'s "Disk Usage : 60.2G / 98.0G (61%)" header line. Same
// Statfs call, same signed/unsigned-conversion caveats.
func diskUsageBytes(path string) (used, total int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, 0, false
	}
	bsize := int64(st.Bsize)
	total = int64(st.Blocks) * bsize
	used = (int64(st.Blocks) - int64(st.Bavail)) * bsize
	return used, total, true
}
