//go:build linux

package agentcore

import (
	"os"
	"syscall"
)

// diskUsage returns the total and unprivileged-available bytes of the filesystem holding
// path (the agent data dir). A failed statfs reports zeros ("not collected").
func diskUsage(path string) (totalBytes, freeBytes int64) {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := int64(st.Bsize)
	// Bavail is the space available to a non-root process — the number that actually bounds
	// what a deploy can write, so it's the honest "free" to report.
	return int64(st.Blocks) * bsize, int64(st.Bavail) * bsize
}

// memInfo returns total and available host memory in bytes from /proc/meminfo.
func memInfo() (totalBytes, availableBytes int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()
	return parseMeminfo(f)
}
