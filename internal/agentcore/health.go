package agentcore

import (
	"context"
	"runtime"
	"time"
)

// dockerProbeTimeout bounds the per-heartbeat Docker probe so a hung or slow daemon can
// never stall the heartbeat (which beats every ~30s). The probe context derives from the
// loop context, so a shutdown cancels it immediately.
const dockerProbeTimeout = 3 * time.Second

// dockerProber is the read-only Docker capability the heartbeat needs: a cheap
// liveness+version probe. *dockerClient satisfies it via serverVersion. It is kept
// separate from deploymentRuntime so the privileged deploy surface is not widened for
// health reporting.
type dockerProber interface {
	serverVersion(ctx context.Context) (version string, err error)
}

// healthFacts are the compatibility signals the agent reports on each heartbeat. They are
// deliberately minimal and non-sensitive: whether Docker is reachable, its version, and
// the host OS/arch. The richer readiness model (disk/memory/CPU, Caddy, ports, outbound
// connectivity) is a later slice — see docs/architecture/agent.md.
type healthFacts struct {
	DockerAvailable bool
	DockerVersion   string
	OS              string
	Arch            string
}

// collectHealth gathers the heartbeat's compatibility facts. OS/Arch come from the agent
// binary's own runtime.GOOS/GOARCH — the HOST the agent runs on, not the Docker daemon's
// platform (which can differ, e.g. Docker Desktop's Linux VM on macOS); keep it that way.
// Docker is probed with a short, cancelable timeout; any probe error means unavailable. A
// nil prober (the Docker client failed to construct at startup) reports unavailable. OS is
// always set, which is how the control plane tells a health-reporting agent apart from one
// that predates this field (see proto/agent/v1/agent.proto).
func collectHealth(ctx context.Context, p dockerProber) healthFacts {
	f := healthFacts{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if p == nil {
		return f
	}
	cctx, cancel := context.WithTimeout(ctx, dockerProbeTimeout)
	defer cancel()
	if v, err := p.serverVersion(cctx); err == nil {
		f.DockerAvailable = true
		f.DockerVersion = v
	}
	return f
}
