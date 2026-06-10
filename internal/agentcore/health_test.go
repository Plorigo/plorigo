package agentcore

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

// stubProber is a fake dockerProber: it returns a fixed version or error, so health
// collection and the heartbeat loop can be tested without a real Docker daemon.
type stubProber struct {
	version string
	err     error
}

func (s stubProber) serverVersion(_ context.Context) (string, error) { return s.version, s.err }

func TestCollectHealth(t *testing.T) {
	t.Run("docker available reports version", func(t *testing.T) {
		f := collectHealth(context.Background(), stubProber{version: "27.1.1"})
		if !f.DockerAvailable || f.DockerVersion != "27.1.1" {
			t.Errorf("docker = (%v, %q), want (true, 27.1.1)", f.DockerAvailable, f.DockerVersion)
		}
		assertHostFacts(t, f)
	})
	t.Run("probe error means unavailable", func(t *testing.T) {
		f := collectHealth(context.Background(), stubProber{err: errors.New("daemon down")})
		if f.DockerAvailable || f.DockerVersion != "" {
			t.Errorf("docker = (%v, %q), want (false, empty)", f.DockerAvailable, f.DockerVersion)
		}
		assertHostFacts(t, f)
	})
	t.Run("nil prober means unavailable but still reports host facts", func(t *testing.T) {
		f := collectHealth(context.Background(), nil)
		if f.DockerAvailable {
			t.Errorf("DockerAvailable = true, want false for a nil prober")
		}
		assertHostFacts(t, f)
	})
}

// assertHostFacts checks OS/Arch are always the agent's own host values, regardless of
// Docker — that is what makes "os is set" a reliable health-reporting-agent marker.
func assertHostFacts(t *testing.T, f healthFacts) {
	t.Helper()
	if f.OS != runtime.GOOS || f.Arch != runtime.GOARCH {
		t.Errorf("os/arch = %q/%q, want %q/%q", f.OS, f.Arch, runtime.GOOS, runtime.GOARCH)
	}
}
