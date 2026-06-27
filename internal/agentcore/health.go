package agentcore

import (
	"context"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// dockerProbeTimeout bounds the per-heartbeat Docker probe so a hung or slow daemon can
// never stall the heartbeat (which beats every ~30s). The probe context derives from the
// loop context, so a shutdown cancels it immediately.
const dockerProbeTimeout = 3 * time.Second

// caddyProbeTimeout bounds the per-heartbeat Caddy probes (a `caddy version` exec and an
// admin-API request), for the same reason as the Docker probe.
const caddyProbeTimeout = 3 * time.Second

// dockerProber is the read-only Docker capability the heartbeat needs: a cheap
// liveness+version probe. *dockerClient satisfies it via serverVersion. It is kept
// separate from deploymentRuntime so the privileged deploy surface is not widened for
// health reporting.
type dockerProber interface {
	serverVersion(ctx context.Context) (version string, err error)
}

// healthFacts are the compatibility signals the agent reports on each heartbeat. They are
// non-sensitive: whether Docker and the Caddy reverse proxy are reachable and their versions,
// the host OS/arch, and coarse host resources (disk/memory/CPU). The control plane derives a
// readiness signal from them — see docs/architecture/agent.md.
type healthFacts struct {
	DockerAvailable bool
	DockerVersion   string
	OS              string
	Arch            string
	CaddyAvailable  bool
	CaddyRunning    bool
	CaddyVersion    string
	hostResources
	CPUCount int32
}

// collectHealth gathers the heartbeat's compatibility facts. OS/Arch come from the agent
// binary's own runtime.GOOS/GOARCH — the HOST the agent runs on, not the Docker daemon's
// platform (which can differ, e.g. Docker Desktop's Linux VM on macOS); keep it that way.
// Docker and Caddy are probed with short, cancelable timeouts; any probe error means
// unavailable. A nil prober (the Docker client failed to construct at startup) reports Docker
// unavailable. CPUCount is always set (runtime.NumCPU, never zero), which is how the control
// plane tells an agent that reports the extended facts apart from one that predates them.
func collectHealth(ctx context.Context, p dockerProber, opts Options) healthFacts {
	f := healthFacts{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		CPUCount:      int32(runtime.NumCPU()),
		hostResources: collectHostResources(opts.DataDir),
	}
	if p != nil {
		cctx, cancel := context.WithTimeout(ctx, dockerProbeTimeout)
		if v, err := p.serverVersion(cctx); err == nil {
			f.DockerAvailable = true
			f.DockerVersion = v
		}
		cancel()
	}
	f.CaddyAvailable, f.CaddyVersion, f.CaddyRunning = probeCaddy(ctx, opts)
	return f
}

// probeCaddy reports whether the Caddy binary is installed (and its version), and whether its
// admin API is reachable (it is serving). Both probes fail closed: any error means "not
// available / not running", never a stalled heartbeat.
func probeCaddy(ctx context.Context, opts Options) (available bool, version string, running bool) {
	bin := strings.TrimSpace(opts.CaddyBin)
	if bin == "" {
		bin = defaultCaddyBin
	}
	vctx, cancel := context.WithTimeout(ctx, caddyProbeTimeout)
	defer cancel()
	if out, err := exec.CommandContext(vctx, bin, "version").Output(); err == nil {
		available = true
		version = parseCaddyVersion(string(out))
	}

	admin := strings.TrimSpace(opts.CaddyAdmin)
	if admin == "" {
		admin = defaultCaddyAdmin
	}
	running = caddyAdminReachable(ctx, admin)
	return available, version, running
}

// caddyAdminReachable reports whether Caddy's admin API answers — any HTTP response means the
// process is up and serving; a refused connection or timeout means it is not.
func caddyAdminReachable(ctx context.Context, admin string) bool {
	cctx, cancel := context.WithTimeout(ctx, caddyProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://"+admin+"/config/", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// parseCaddyVersion extracts the version from `caddy version` output (e.g. "v2.7.6 h1:…"),
// trimming the leading "v".
func parseCaddyVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[0], "v")
}
