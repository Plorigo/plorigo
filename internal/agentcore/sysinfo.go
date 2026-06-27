package agentcore

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// hostResources are coarse host facts the agent reports for the readiness signal. Zero means
// "not collected" (a non-Linux build, or a read that failed) — the control plane treats it as
// not reported. Disk/memory collection is platform-specific (see sysinfo_linux.go); the agent
// deploys on Linux, so other platforms report zeros.
type hostResources struct {
	diskTotalBytes    int64
	diskFreeBytes     int64
	memTotalBytes     int64
	memAvailableBytes int64
}

// collectHostResources gathers disk usage for the data-dir filesystem and host memory.
func collectHostResources(dataDir string) hostResources {
	dt, df := diskUsage(dataDir)
	mt, ma := memInfo()
	return hostResources{diskTotalBytes: dt, diskFreeBytes: df, memTotalBytes: mt, memAvailableBytes: ma}
}

// parseMeminfo extracts MemTotal and MemAvailable (in bytes) from /proc/meminfo content. It is
// platform-neutral so it can be unit-tested anywhere; only the file read is Linux-specific.
func parseMeminfo(r io.Reader) (totalBytes, availableBytes int64) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			totalBytes = meminfoKB(val)
		case "MemAvailable":
			availableBytes = meminfoKB(val)
		}
	}
	return totalBytes, availableBytes
}

// meminfoKB parses a "  16384256 kB" /proc/meminfo value into bytes.
func meminfoKB(v string) int64 {
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}
