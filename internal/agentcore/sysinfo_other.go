//go:build !linux

package agentcore

// The agent prepares and deploys on Linux servers; on other platforms (a developer's macOS
// box running the agent against a dev control plane) host disk/memory aren't collected, and
// the control plane treats the zero values as "not reported".
func diskUsage(string) (totalBytes, freeBytes int64) { return 0, 0 }

func memInfo() (totalBytes, availableBytes int64) { return 0, 0 }
