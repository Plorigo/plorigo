package serversetup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const ubuntu2204 = "PRETTY_NAME=\"Ubuntu 22.04.3 LTS\"\nID=ubuntu\nVERSION_ID=\"22.04\"\n"

// execRule scripts one command response, matched by substring (first match wins).
type execRule struct {
	match string
	res   ExecResult
	err   error
}

type scriptedExec struct {
	rules  []execRule
	runs   []string
	closed bool
}

func (e *scriptedExec) Run(_ context.Context, cmd string) (ExecResult, error) {
	e.runs = append(e.runs, cmd)
	for _, r := range e.rules {
		if strings.Contains(cmd, r.match) {
			return r.res, r.err
		}
	}
	return ExecResult{ExitCode: 0}, nil // unmatched commands succeed
}

func (e *scriptedExec) Close() error { e.closed = true; return nil }

// execWith builds an executor that bootstraps a healthy Ubuntu 22.04 box, with the given
// overrides taking precedence (prepended, so they match before the healthy defaults).
func execWith(overrides ...execRule) *scriptedExec {
	base := []execRule{
		{match: "/etc/os-release", res: ExecResult{Stdout: ubuntu2204}},
		{match: "id -u", res: ExecResult{Stdout: "0\n"}}, // root
		{match: "lock-frontend", res: ExecResult{Stdout: "FREE\n"}},
		{match: "ss -ltn", res: ExecResult{Stdout: ""}}, // ports free
		{match: "docker version", res: ExecResult{Stdout: "24.0.7\n"}},
		{match: "ufw status", res: ExecResult{Stdout: "Status: inactive\n"}},
	}
	return &scriptedExec{rules: append(append([]execRule{}, overrides...), base...)}
}

type fakeAgents struct {
	token       string
	tokenErr    error
	online      bool
	onlineErr   error
	regCalls    int
	onlineCalls int
}

func (f *fakeAgents) RegistrationToken(_ context.Context, _ string) (string, error) {
	f.regCalls++
	return f.token, f.tokenErr
}

func (f *fakeAgents) AgentOnline(_ context.Context, _, _ string) (bool, error) {
	f.onlineCalls++
	return f.online, f.onlineErr
}

type capturedEvent struct{ step, kind, status, message string }

type capture struct {
	events  []capturedEvent
	audited []string
}

func (c *capture) emit(step, kind, status, message string) {
	c.events = append(c.events, capturedEvent{step, kind, status, message})
}
func (c *capture) audit(_ context.Context, action string) { c.audited = append(c.audited, action) }

func (c *capture) failed() (step, message string) {
	for _, e := range c.events {
		if e.status == "failed" {
			return e.step, e.message
		}
	}
	return "", ""
}
func (c *capture) okStep(step string) bool {
	for _, e := range c.events {
		if e.step == step && e.status == "ok" {
			return true
		}
	}
	return false
}
func (c *capture) logContains(step, substr string) bool {
	for _, e := range c.events {
		if e.step == step && e.kind == "log" && strings.Contains(e.message, substr) {
			return true
		}
	}
	return false
}

// newRunner wires a Runner against fakes. provisionKey returns a fixed public key by default.
func newRunner(exec SSHExecutor, ag AgentProvisioner, c *capture, provision func(context.Context) (string, error)) *Runner {
	if provision == nil {
		provision = func(context.Context) (string, error) { return "ssh-ed25519 AAAAPUBLIC plorigo", nil }
	}
	return &Runner{
		exec:              exec,
		emit:              c.emit,
		audit:             c.audit,
		agents:            ag,
		provisionKey:      provision,
		workspaceID:       "ws-1",
		serverID:          "srv-1",
		controlPlaneURL:   "https://cp.example.com",
		heartbeatAttempts: 3,
		heartbeatDelay:    time.Millisecond,
		sleep:             func(time.Duration) {},
	}
}

func TestRun_SuccessProvisionsAndRedactsToken(t *testing.T) {
	exec := execWith()
	ag := &fakeAgents{token: "plrt_SUPERSECRETTOKEN", online: true}
	c := &capture{}
	r := newRunner(exec, ag, c, nil)

	reason := r.Run(context.Background())
	if reason != "" {
		t.Fatalf("expected success, got failure: %q", reason)
	}
	for _, step := range []string{"detect_os", "check_privilege", "preflight", "install_prereqs", "provision_user", "await_agent"} {
		if !c.okStep(step) {
			t.Errorf("step %q did not complete ok", step)
		}
	}
	// The installer ran with the registration token...
	ranInstaller := false
	for _, cmd := range exec.runs {
		if strings.Contains(cmd, "install-agent.sh") && strings.Contains(cmd, "plrt_SUPERSECRETTOKEN") {
			ranInstaller = true
		}
	}
	if !ranInstaller {
		t.Error("the installer command should carry the registration token")
	}
	// ...but the token must NEVER appear in any emitted event (redaction).
	for _, e := range c.events {
		if strings.Contains(e.message, "SUPERSECRETTOKEN") {
			t.Errorf("registration token leaked into an event: %q", e.message)
		}
	}
	// A prerequisite change is audited.
	if !containsStr(c.audited, "server_setup.prereq_change") {
		t.Errorf("expected a prereq_change audit, got %v", c.audited)
	}
}

func TestRun_UnsupportedOS(t *testing.T) {
	exec := execWith(execRule{match: "/etc/os-release", res: ExecResult{Stdout: "PRETTY_NAME=\"Debian GNU/Linux 12\"\nID=debian\nVERSION_ID=\"12\"\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "unsupported operating system") {
		t.Fatalf("reason = %q, want unsupported OS", reason)
	}
	if step, _ := c.failed(); step != "detect_os" {
		t.Errorf("failed at step %q, want detect_os", step)
	}
}

func TestRun_MissingRootOrSudo(t *testing.T) {
	exec := execWith(
		execRule{match: "id -u", res: ExecResult{Stdout: "1000\n"}}, // not root
		execRule{match: "sudo -n true", res: ExecResult{ExitCode: 1}},
	)
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "passwordless sudo") {
		t.Fatalf("reason = %q, want a sudo/root hint", reason)
	}
	if step, _ := c.failed(); step != "check_privilege" {
		t.Errorf("failed at step %q, want check_privilege", step)
	}
}

func TestRun_AptLockHeld(t *testing.T) {
	exec := execWith(execRule{match: "lock-frontend", res: ExecResult{Stdout: "LOCKED\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "apt/dpkg lock") {
		t.Fatalf("reason = %q, want apt lock hint", reason)
	}
	if step, _ := c.failed(); step != "preflight" {
		t.Errorf("failed at step %q, want preflight", step)
	}
}

func TestRun_DockerAlreadyInstalled(t *testing.T) {
	exec := execWith(execRule{match: "docker version", res: ExecResult{Stdout: "24.0.7\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	if reason := r.Run(context.Background()); reason != "" {
		t.Fatalf("a present Docker must not fail setup, got %q", reason)
	}
	if !c.logContains("preflight", "already installed") {
		t.Error("expected a log noting Docker is already installed")
	}
}

func TestRun_OldDockerIsUpgradedNotFatal(t *testing.T) {
	exec := execWith(execRule{match: "docker version", res: ExecResult{Stdout: "19.03.1\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	if reason := r.Run(context.Background()); reason != "" {
		t.Fatalf("an old Docker must not fail setup (installer upgrades), got %q", reason)
	}
	if !c.logContains("preflight", "is old") {
		t.Error("expected a log noting Docker is old")
	}
}

func TestRun_OccupiedPorts(t *testing.T) {
	exec := execWith(execRule{match: "ss -ltn", res: ExecResult{Stdout: "0.0.0.0:80\n[::]:443\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "ports 80 and/or 443") {
		t.Fatalf("reason = %q, want occupied-ports hint", reason)
	}
	if step, _ := c.failed(); step != "preflight" {
		t.Errorf("failed at step %q, want preflight", step)
	}
}

func TestRun_UfwActiveIsAllowedNotFatal(t *testing.T) {
	exec := execWith(execRule{match: "ufw status", res: ExecResult{Stdout: "Status: active\n"}})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	if reason := r.Run(context.Background()); reason != "" {
		t.Fatalf("an active UFW must not fail setup (ports are allowed), got %q", reason)
	}
	if !c.logContains("preflight", "UFW firewall is active") {
		t.Error("expected a log noting UFW is active")
	}
	allowed := false
	for _, cmd := range exec.runs {
		if strings.Contains(cmd, "ufw allow") {
			allowed = true
		}
	}
	if !allowed {
		t.Error("expected UFW allow commands to open SSH/80/443")
	}
}

func TestRun_AgentHeartbeatTimeout(t *testing.T) {
	exec := execWith()
	ag := &fakeAgents{online: false} // agent never checks in
	c := &capture{}
	r := newRunner(exec, ag, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "did not connect") {
		t.Fatalf("reason = %q, want a heartbeat-timeout hint", reason)
	}
	if step, _ := c.failed(); step != "await_agent" {
		t.Errorf("failed at step %q, want await_agent", step)
	}
	if ag.onlineCalls != r.heartbeatAttempts {
		t.Errorf("polled %d times, want %d attempts", ag.onlineCalls, r.heartbeatAttempts)
	}
}

func TestRun_TransportErrorStops(t *testing.T) {
	exec := execWith(execRule{match: "/etc/os-release", err: errors.New("connection reset")})
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, nil)

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "lost the SSH connection") {
		t.Fatalf("reason = %q, want a transport-failure message", reason)
	}
}

func TestRun_ProvisionKeyFailure(t *testing.T) {
	exec := execWith()
	c := &capture{}
	r := newRunner(exec, &fakeAgents{online: true}, c, func(context.Context) (string, error) {
		return "", errors.New("seal failed")
	})

	reason := r.Run(context.Background())
	if !strings.Contains(reason, "could not provision the management credential") {
		t.Fatalf("reason = %q, want a provisioning-failure message", reason)
	}
	if step, _ := c.failed(); step != "provision_user" {
		t.Errorf("failed at step %q, want provision_user", step)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
