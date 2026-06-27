package serversetup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dial error sentinels, so the service can map a connection failure to a precise, redacted,
// plain-English reason (and audit a failed authentication). The SSHDialer implementation wraps
// these.
var (
	ErrAuth            = errors.New("ssh authentication failed")
	ErrHostKeyMismatch = errors.New("ssh host key mismatch")
)

// installerURL is the shared one-line installer the managed path drives over SSH. It prepares
// the OS (Docker, Caddy), installs the agent + systemd unit, and starts it — the same script
// the self-serve path runs. The dashboard path adds the non-root `plorigo` management user.
const installerURL = "https://raw.githubusercontent.com/Plorigo/plorigo/main/scripts/install-agent.sh"

// The scoped sudoers policy for the plorigo management user: an explicit, wildcard-free
// allowlist of exactly the repair commands the management channel needs (restart/recover the
// agent and Docker). It is deliberately NOT broad apt/ALL access — re-running full setup uses
// a fresh bootstrap credential, not this. The exact allowlist is a security-review item; see
// docs/architecture/server-management.md.
const plorigoSudoers = "plorigo ALL=(root) NOPASSWD: " +
	"/usr/bin/systemctl restart plorigo-agent.service, " +
	"/usr/bin/systemctl start plorigo-agent.service, " +
	"/usr/bin/systemctl stop plorigo-agent.service, " +
	"/usr/bin/systemctl status plorigo-agent.service, " +
	"/usr/bin/systemctl restart docker.service, " +
	"/usr/bin/systemctl status docker.service\n"

// ExecResult is the outcome of one remote command. A non-zero ExitCode is a command failure
// the step interprets; a transport error (connection dropped) is returned separately.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SSHExecutor runs commands over an established SSH session. The bootstrap runner depends only
// on this port, so it is fully unit-testable with a fake executor.
type SSHExecutor interface {
	Run(ctx context.Context, cmd string) (ExecResult, error)
	Close() error
}

// DialTarget is everything needed to open the one-time bootstrap SSH session. The credential
// fields are used only for the attempt and never stored.
type DialTarget struct {
	Host                 string
	Port                 int
	Username             string
	Password             string
	PrivateKey           []byte
	PrivateKeyPassphrase string
	// PinnedHostKeyFingerprint enforces TOFU: empty on the first connection (the dialer
	// returns the captured fingerprint to pin); non-empty makes the dialer reject a server
	// presenting a different host key.
	PinnedHostKeyFingerprint string
}

// SSHDialer opens the bootstrap SSH session and reports the server's host-key fingerprint
// (for TOFU pinning). Implemented by internal/platform/sshexec; the service uses it, the
// runner uses the returned executor.
type SSHDialer interface {
	Dial(ctx context.Context, target DialTarget) (exec SSHExecutor, hostKeyFingerprint string, err error)
}

// AgentProvisioner is what the runner needs from the agents module (wired via an adapter in
// internal/app, so serversetup never imports agents): mint a one-time registration token for
// the installer, and report whether the server's agent is online (the heartbeat wait).
type AgentProvisioner interface {
	RegistrationToken(ctx context.Context, serverID string) (token string, err error)
	AgentOnline(ctx context.Context, workspaceID, serverID string) (online bool, err error)
}

// Runner executes the ordered bootstrap steps over an SSHExecutor, emitting redacted status
// and log events as it goes. It returns a plain-English failure reason ("" on success); it
// never returns key material or a raw credential, and never emits a command that carries the
// registration token.
type Runner struct {
	exec   SSHExecutor
	emit   func(step, kind, status, message string)
	audit  func(ctx context.Context, action string)
	agents AgentProvisioner
	// provisionKey generates+seals+stores the management credential for this server and
	// returns its public authorized_keys line (it audits the credential install itself).
	provisionKey func(ctx context.Context) (publicKeyLine string, err error)

	workspaceID     string
	serverID        string
	controlPlaneURL string

	heartbeatAttempts int
	heartbeatDelay    time.Duration
	sleep             func(d time.Duration) // injectable so tests don't actually sleep
}

// Run executes every step in order, stopping at the first failure. The returned reason is ""
// on success or a plain-English failure message otherwise.
func (r *Runner) Run(ctx context.Context) (failureReason string) {
	steps := []struct {
		name string
		fn   func(context.Context) string
	}{
		{"detect_os", r.detectOS},
		{"check_privilege", r.checkPrivilege},
		{"preflight", r.preflight},
		{"install_prereqs", r.installPrereqs},
		{"provision_user", r.provisionUser},
		{"await_agent", r.awaitAgent},
	}
	for _, s := range steps {
		if ctx.Err() != nil {
			r.emit(s.name, "status", "failed", "setup was canceled")
			return "setup was canceled"
		}
		r.emit(s.name, "status", "started", "")
		if reason := s.fn(ctx); reason != "" {
			r.emit(s.name, "status", "failed", reason)
			return reason
		}
		r.emit(s.name, "status", "ok", "")
	}
	return ""
}

func (r *Runner) detectOS(ctx context.Context) string {
	res, reason := r.cmd(ctx, "cat /etc/os-release")
	if reason != "" {
		return reason
	}
	if res.ExitCode != 0 {
		return "could not read /etc/os-release on the server; is this a Linux host?"
	}
	id := osReleaseField(res.Stdout, "ID")
	ver := osReleaseField(res.Stdout, "VERSION_ID")
	if id != "ubuntu" || (ver != "22.04" && ver != "24.04") {
		pretty := osReleaseField(res.Stdout, "PRETTY_NAME")
		if pretty == "" {
			pretty = strings.TrimSpace(id + " " + ver)
		}
		return fmt.Sprintf("unsupported operating system: %s. Plorigo supports Ubuntu 22.04 and 24.04 LTS.", pretty)
	}
	r.emit("detect_os", "log", "", "Detected Ubuntu "+ver+" LTS.")
	return ""
}

func (r *Runner) checkPrivilege(ctx context.Context) string {
	res, reason := r.cmd(ctx, "id -u")
	if reason != "" {
		return reason
	}
	if strings.TrimSpace(res.Stdout) == "0" {
		r.emit("check_privilege", "log", "", "Connected as root.")
		return ""
	}
	res2, reason := r.cmd(ctx, "sudo -n true")
	if reason != "" {
		return reason
	}
	if res2.ExitCode != 0 {
		return "the bootstrap user must be root or have passwordless sudo. Reconnect as root, or grant the user NOPASSWD sudo, and retry."
	}
	r.emit("check_privilege", "log", "", "Passwordless sudo confirmed.")
	return ""
}

func (r *Runner) preflight(ctx context.Context) string {
	// apt/dpkg lock — a held lock makes the installer's package steps fail confusingly.
	if res, reason := r.cmd(ctx, "if sudo fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock >/dev/null 2>&1; then echo LOCKED; else echo FREE; fi"); reason != "" {
		return reason
	} else if strings.Contains(res.Stdout, "LOCKED") {
		return "another process holds the apt/dpkg lock (often unattended-upgrades). Wait for it to finish and retry."
	}

	// Ports 80/443 must be free for the reverse proxy.
	if res, reason := r.cmd(ctx, "ss -ltnH 2>/dev/null | awk '{print $4}' | grep -E '(:80|:443)$' || true"); reason != "" {
		return reason
	} else if strings.TrimSpace(res.Stdout) != "" {
		return "ports 80 and/or 443 are already in use. Plorigo's reverse proxy needs them free — stop the process using them and retry."
	}

	// Docker presence/version is informational: the installer installs or upgrades as needed.
	res, _ := r.cmd(ctx, "docker version --format '{{.Server.Version}}' 2>/dev/null || true")
	switch v := strings.TrimSpace(res.Stdout); {
	case v == "":
		r.emit("preflight", "log", "", "Docker is not installed; the installer will add it.")
	case dockerMajor(v) > 0 && dockerMajor(v) < 20:
		r.emit("preflight", "log", "", "Docker "+v+" is old; the installer will upgrade it.")
	default:
		r.emit("preflight", "log", "", "Docker "+v+" is already installed.")
	}

	// UFW, if active, must allow SSH and the proxy ports or the server becomes unreachable.
	if res, _ := r.cmd(ctx, "sudo ufw status 2>/dev/null | head -1"); strings.Contains(res.Stdout, "Status: active") {
		r.emit("preflight", "log", "", "UFW firewall is active; allowing SSH, 80, and 443.")
		// Idempotent allows; ignore the result (best-effort hardening, not a gate).
		_, _ = r.cmd(ctx, "sudo ufw allow OpenSSH >/dev/null 2>&1; sudo ufw allow 80/tcp >/dev/null 2>&1; sudo ufw allow 443/tcp >/dev/null 2>&1; true")
	}
	return ""
}

func (r *Runner) installPrereqs(ctx context.Context) string {
	token, err := r.agents.RegistrationToken(ctx, r.serverID)
	if err != nil {
		return "could not mint an agent registration token."
	}
	r.emit("install_prereqs", "log", "", "Installing prerequisites (Docker, Caddy) and the Plorigo agent…")
	// The command carries the one-time token, so it is NEVER emitted to events. The installer
	// redacts the token from its own output, and the token is in the args, not stdout.
	cmd := fmt.Sprintf("curl -fsSL %s | sudo sh -s -- --control-plane %s --token %s",
		installerURL, shellQuote(r.controlPlaneURL), shellQuote(token))
	res, reason := r.cmd(ctx, cmd)
	if reason != "" {
		return reason
	}
	r.emitOutput("install_prereqs", res.Stdout)
	if res.ExitCode != 0 {
		r.emitOutput("install_prereqs", res.Stderr)
		return "the installer failed to prepare the server. See the log lines above for the failing step."
	}
	r.audit(ctx, "server_setup.prereq_change")
	r.emit("install_prereqs", "log", "", "Prerequisites installed and the agent service started.")
	return ""
}

func (r *Runner) provisionUser(ctx context.Context) string {
	pub, err := r.provisionKey(ctx)
	if err != nil {
		return "could not provision the management credential."
	}
	if reason := r.must(ctx, "id -u plorigo >/dev/null 2>&1 || sudo useradd --create-home --shell /bin/bash --comment 'Plorigo management' plorigo",
		"could not create the plorigo management user."); reason != "" {
		return reason
	}
	if reason := r.must(ctx, "sudo install -d -m 700 -o plorigo -g plorigo /home/plorigo/.ssh",
		"could not prepare the plorigo SSH directory."); reason != "" {
		return reason
	}
	installKey := fmt.Sprintf("printf '%%s\\n' %s | sudo tee /home/plorigo/.ssh/authorized_keys >/dev/null && sudo chmod 600 /home/plorigo/.ssh/authorized_keys && sudo chown plorigo:plorigo /home/plorigo/.ssh/authorized_keys", shellQuote(pub))
	if reason := r.must(ctx, installKey, "could not install the management public key."); reason != "" {
		return reason
	}
	// Write the scoped sudoers drop-in and validate it; a bad policy is removed, not left.
	writeSudoers := fmt.Sprintf("printf '%%s' %s | sudo tee /etc/sudoers.d/plorigo >/dev/null && sudo chmod 440 /etc/sudoers.d/plorigo && sudo visudo -cf /etc/sudoers.d/plorigo", shellQuote(plorigoSudoers))
	res, reason := r.cmd(ctx, writeSudoers)
	if reason != "" {
		return reason
	}
	if res.ExitCode != 0 {
		_, _ = r.cmd(ctx, "sudo rm -f /etc/sudoers.d/plorigo")
		return "failed to install a valid scoped sudo policy for the plorigo user."
	}
	r.emit("provision_user", "log", "", "Management user 'plorigo' created with a key-only login and scoped sudo.")
	return ""
}

func (r *Runner) awaitAgent(ctx context.Context) string {
	for i := 0; i < r.heartbeatAttempts; i++ {
		if ctx.Err() != nil {
			return "setup was canceled while waiting for the agent."
		}
		online, err := r.agents.AgentOnline(ctx, r.workspaceID, r.serverID)
		if err == nil && online {
			r.emit("await_agent", "log", "", "The agent connected and is online.")
			return ""
		}
		if i < r.heartbeatAttempts-1 {
			r.sleep(r.heartbeatDelay)
		}
	}
	return "the agent did not connect in time. Check the server can reach the control plane and that plorigo-agent.service is running, then retry."
}

// cmd runs a command and converts a transport error into a plain-English reason. A non-zero
// exit code is returned in the result for the caller to interpret.
func (r *Runner) cmd(ctx context.Context, command string) (ExecResult, string) {
	res, err := r.exec.Run(ctx, command)
	if err != nil {
		return ExecResult{}, "lost the SSH connection to the server while preparing it."
	}
	return res, ""
}

// must runs a command that must exit 0, returning failMsg on a non-zero exit.
func (r *Runner) must(ctx context.Context, command, failMsg string) string {
	res, reason := r.cmd(ctx, command)
	if reason != "" {
		return reason
	}
	if res.ExitCode != 0 {
		return failMsg
	}
	return ""
}

// emitOutput emits up to the last 80 non-empty lines of command output as log events.
func (r *Runner) emitOutput(step, output string) {
	lines := make([]string, 0, 16)
	for _, ln := range strings.Split(output, "\n") {
		if s := strings.TrimRight(ln, "\r"); strings.TrimSpace(s) != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) > 80 {
		lines = lines[len(lines)-80:]
	}
	for _, ln := range lines {
		r.emit(step, "log", "", ln)
	}
}

// osReleaseField extracts a KEY=value field from /etc/os-release output, unquoting the value.
func osReleaseField(content, key string) string {
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(ln)
		if rest, ok := strings.CutPrefix(ln, key+"="); ok {
			return strings.Trim(rest, `"'`)
		}
	}
	return ""
}

// dockerMajor parses the leading major version from a "24.0.7"-style string (0 if unparseable).
func dockerMajor(v string) int {
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(strings.TrimSpace(major))
	if err != nil {
		return 0
	}
	return n
}

// shellQuote single-quotes a string for safe interpolation into a remote /bin/sh command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
