// Hermetic tests for scripts/install-agent.sh. They run the REAL installer with a PATH
// front-loaded by fake apt-get/dpkg/docker/caddy/systemctl/journalctl/curl/ss/id/sleep
// binaries and a fake /etc/os-release, so every branch — OS gating, idempotency, the named
// failure modes, port handling, connectivity, and token redaction — is exercised without
// Docker, without root, and in milliseconds. Privileged paths the script writes to (the
// systemd unit, apt keyrings/sources, the agent binary) are redirected to temp dirs via the
// script's documented override env vars. This runs under `go test ./...` (CI); the heavier
// real-container flow stays in the e2e test (build tag `e2e`).
package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installToken is the registration token every case passes; tests assert it never appears in
// the installer's output (only `***REDACTED***` may, when a leak is simulated).
const installToken = "plrt_SHIM_secret_token_value"

// fakeBins are the external commands the installer shells out to, replaced by shims that
// record their calls to $FAKE_LOG and return scripted results driven by FAKE_* env vars.
var fakeBins = map[string]string{
	"id": `#!/bin/sh
[ "$1" = "-u" ] && { echo "${FAKE_UID:-0}"; exit 0; }
echo 0
`,
	"dpkg": `#!/bin/sh
[ "$1" = "--print-architecture" ] && { echo "${FAKE_ARCH:-amd64}"; exit 0; }
exit 0
`,
	"apt-get": `#!/bin/sh
echo "apt-get $*" >> "$FAKE_LOG"
if [ "${FAKE_APT_LOCK:-0}" = 1 ]; then
  echo "E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by another process" >&2
  exit 100
fi
exit 0
`,
	"docker": `#!/bin/sh
echo "docker $*" >> "$FAKE_LOG"
case "$1" in
  buildx) [ "${FAKE_DOCKER_HAS_BUILDX:-0}" = 1 ] && exit 0 || exit 1 ;;
  info) [ "${FAKE_DOCKER_UP:-0}" = 1 ] && exit 0
        echo "Cannot connect to the Docker daemon" >&2; exit 1 ;;
  version|--version) echo "Docker version 27.0.0"; exit 0 ;;
esac
exit 0
`,
	"caddy": `#!/bin/sh
echo "caddy $*" >> "$FAKE_LOG"
case "$1" in
  version) [ "${FAKE_CADDY_INSTALLED:-0}" = 1 ] && { echo "v2.8.4"; exit 0; } || exit 1 ;;
esac
exit 0
`,
	"systemctl": `#!/bin/sh
echo "systemctl $*" >> "$FAKE_LOG"
case "$1" in
  is-active) [ "${FAKE_AGENT_ACTIVE:-1}" = 1 ] && exit 0 || exit 3 ;;
  list-unit-files) [ "${FAKE_CADDY_UNIT:-0}" = 1 ] && echo "caddy.service enabled"; exit 0 ;;
esac
exit 0
`,
	"journalctl": `#!/bin/sh
[ -n "${FAKE_JOURNAL_LEAK_TOKEN:-}" ] && echo "agent boot failed; token=$FAKE_JOURNAL_LEAK_TOKEN rejected"
exit 0
`,
	"curl": `#!/bin/sh
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    --max-time) shift 2 ;;
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
echo "curl url=$url out=$out" >> "$FAKE_LOG"
if [ "$out" = "/dev/null" ]; then exit "${FAKE_CONNECT_EXIT:-0}"; fi
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'; else shasum -a 256 | awk '{print $1}'; fi; }
content="${FAKE_AGENT_CONTENT:-plorigo-fake-agent}"
case "$url" in
  file://*) src=$(printf '%s' "$url" | sed 's#^file://##'); cp "$src" "$out" ;;
  *checksums.txt)
    h=$(printf '%s' "$content" | sha)
    [ "${FAKE_BAD_CHECKSUM:-0}" = 1 ] && h=0000000000000000000000000000000000000000000000000000000000000000
    { echo "$h  plorigo-agent-linux-amd64"; echo "$h  plorigo-agent-linux-arm64"; } > "$out" ;;
  *plorigo-agent-linux-*) printf '%s' "$content" > "$out" ;;
  *) echo "fake-key-or-binary" > "$out" ;;
esac
exit "${FAKE_DOWNLOAD_EXIT:-0}"
`,
	"ss": `#!/bin/sh
emit() { echo "LISTEN 0 0 $1 0.0.0.0:* users:((\"$2\",pid=1,fd=3))"; }
[ "${FAKE_PORT80:-free}" != free ] && emit "0.0.0.0:80" "$FAKE_PORT80"
[ "${FAKE_PORT443:-free}" != free ] && emit "0.0.0.0:443" "$FAKE_PORT443"
exit 0
`,
	"sleep": `#!/bin/sh
exit 0
`,
}

const (
	osUbuntu2204 = "ID=ubuntu\nVERSION_ID=\"22.04\"\nVERSION_CODENAME=jammy\nUBUNTU_CODENAME=jammy\nNAME=\"Ubuntu\"\n"
	osUbuntu2404 = "ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\nNAME=\"Ubuntu\"\n"
	osDebian12   = "ID=debian\nVERSION_ID=\"12\"\nNAME=\"Debian GNU/Linux\"\n"
)

type shimCase struct {
	name        string
	osRelease   string            // defaults to Ubuntu 24.04
	env         map[string]string // FAKE_* overrides for the shims
	extraArgs   []string
	noBinaryURL bool // omit --binary-url so the script downloads from GitHub Releases
	wantFail    bool
	stdout      []string          // substrings expected on stdout
	stderr      []string          // substrings expected on stderr
	logHas      []string          // substrings expected in the shim call log
	logAbsent   []string          // substrings that must NOT appear in the shim call log
	sourceHas   map[string]string // apt source filename -> expected substring
	unitFile    bool              // assert the systemd unit was written 0600 with the agent env
}

func TestInstallAgentScript(t *testing.T) {
	script := installerScript(t)

	cases := []shimCase{
		{
			name:      "unsupported OS is rejected",
			osRelease: osDebian12,
			wantFail:  true,
			stderr:    []string{"unsupported OS", "Ubuntu 22.04 and 24.04"},
		},
		{
			name:      "fresh Ubuntu 22.04 prepares the host",
			osRelease: osUbuntu2204,
			env:       map[string]string{"FAKE_DOCKER_UP": "1", "FAKE_CADDY_UNIT": "1"},
			stdout:    []string{"Detected supported OS: Ubuntu 22.04", "Docker daemon is reachable", "installed and started"},
			logHas:    []string{"install -y docker-ce", "install -y caddy", "disable --now caddy", "enable --now plorigo-agent"},
			unitFile:  true,
		},
		{
			name:      "fresh Ubuntu 24.04 prepares the host",
			osRelease: osUbuntu2404,
			env:       map[string]string{"FAKE_DOCKER_UP": "1", "FAKE_CADDY_UNIT": "1"},
			stdout:    []string{"Detected supported OS: Ubuntu 24.04", "installed and started"},
			logHas:    []string{"install -y docker-ce", "install -y caddy"},
			sourceHas: map[string]string{"docker.list": "noble stable"},
			unitFile:  true,
		},
		{
			name:      "idempotent re-run skips Docker and Caddy installs",
			osRelease: osUbuntu2404,
			env:       map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1"},
			stdout:    []string{"already installed", "installed and started"},
			logAbsent: []string{"install -y docker-ce", "install -y caddy"},
			unitFile:  true,
		},
		{
			name:     "held apt lock yields a re-runnable error",
			env:      map[string]string{"FAKE_APT_LOCK": "1"},
			wantFail: true,
			stderr:   []string{"apt/dpkg lock", "safe to re-run"},
		},
		{
			name:     "non-root is rejected with a sudo hint",
			env:      map[string]string{"FAKE_UID": "1000"},
			wantFail: true,
			stderr:   []string{"must run as root", "sudo"},
		},
		{
			name:     "unreachable control plane (DNS) fails fast",
			env:      map[string]string{"FAKE_CONNECT_EXIT": "6"},
			wantFail: true,
			stderr:   []string{"cannot resolve", "DNS"},
		},
		{
			name:     "Docker daemon down is reported",
			env:      map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "0", "FAKE_CADDY_INSTALLED": "1"},
			wantFail: true,
			stderr:   []string{"Docker daemon is not reachable"},
		},
		{
			name:     "foreign listener on port 80 fails",
			env:      map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1", "FAKE_PORT80": "nginx"},
			wantFail: true,
			stderr:   []string{"port 80 is already in use"},
		},
		{
			name:   "Plorigo's own Caddy on port 80 is tolerated",
			env:    map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1", "FAKE_PORT80": "caddy"},
			stdout: []string{"held by Plorigo's own Caddy", "installed and started"},
		},
		{
			name: "agent startup failure redacts the token from logs",
			env: map[string]string{
				"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1",
				"FAKE_AGENT_ACTIVE": "0", "FAKE_JOURNAL_LEAK_TOKEN": installToken,
			},
			wantFail: true,
			stderr:   []string{"did not become active", "***REDACTED***"},
		},
		{
			name:      "skip-prep installs only the agent and service",
			extraArgs: []string{"--skip-prep"},
			stdout:    []string{"installed and started"},
			stderr:    []string{"skipping OS check"},
			logHas:    []string{"enable --now plorigo-agent"},
			logAbsent: []string{"apt-get"},
			unitFile:  true,
		},
		{
			name:        "downloads and checksum-verifies the agent from GitHub releases",
			osRelease:   osUbuntu2404,
			noBinaryURL: true,
			env:         map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1"},
			stdout:      []string{"Downloading the agent binary", "checksum verified", "installed and started"},
			logHas:      []string{"releases/latest/download/plorigo-agent-linux-amd64", "releases/latest/download/checksums.txt"},
			unitFile:    true,
		},
		{
			name:        "tampered agent binary is rejected (checksum mismatch)",
			osRelease:   osUbuntu2404,
			noBinaryURL: true,
			env:         map[string]string{"FAKE_DOCKER_HAS_BUILDX": "1", "FAKE_DOCKER_UP": "1", "FAKE_CADDY_INSTALLED": "1", "FAKE_BAD_CHECKSUM": "1"},
			wantFail:    true,
			stderr:      []string{"checksum mismatch", "Do not trust"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := runInstaller(t, script, tc)

			if tc.wantFail && res.err == nil {
				t.Fatalf("expected the installer to fail, but it exited 0\nstdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
			}
			if !tc.wantFail && res.err != nil {
				t.Fatalf("expected the installer to succeed, got %v\nstdout:\n%s\nstderr:\n%s", res.err, res.stdout, res.stderr)
			}

			// The raw token must never surface in any output, in any case.
			if strings.Contains(res.stdout+res.stderr, installToken) {
				t.Fatalf("registration token leaked into installer output:\nstdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
			}

			assertContains(t, "stdout", res.stdout, tc.stdout)
			assertContains(t, "stderr", res.stderr, tc.stderr)
			assertContains(t, "call log", res.log, tc.logHas)
			for _, s := range tc.logAbsent {
				if strings.Contains(res.log, s) {
					t.Fatalf("call log unexpectedly contains %q:\n%s", s, res.log)
				}
			}
			for file, want := range tc.sourceHas {
				body := readFile(t, filepath.Join(res.sourcesDir, file))
				if !strings.Contains(body, want) {
					t.Fatalf("apt source %s missing %q:\n%s", file, want, body)
				}
			}
			if tc.unitFile {
				assertUnitFile(t, res.systemdDir)
			}
		})
	}
}

type installResult struct {
	stdout, stderr, log    string
	systemdDir, sourcesDir string
	err                    error
}

func runInstaller(t *testing.T, script string, tc shimCase) installResult {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mkdir(t, binDir)
	for name, body := range fakeBins {
		writeExec(t, filepath.Join(binDir, name), body)
	}

	osRelease := tc.osRelease
	if osRelease == "" {
		osRelease = osUbuntu2404
	}
	osFile := filepath.Join(dir, "os-release")
	writeFile(t, osFile, osRelease)

	agentBin := filepath.Join(dir, "agent-bin")
	writeFile(t, agentBin, "#!/bin/sh\necho agent\n")

	systemdDir := filepath.Join(dir, "systemd")
	sourcesDir := filepath.Join(dir, "sources")
	logFile := filepath.Join(dir, "calls.log")
	writeFile(t, logFile, "")

	args := []string{
		script,
		"--control-plane", "http://cp.example.test:9999",
		"--token", installToken,
		"--data-dir", filepath.Join(dir, "data"),
	}
	if !tc.noBinaryURL {
		args = append(args, "--binary-url", "file://"+agentBin)
	}
	args = append(args, tc.extraArgs...)

	env := map[string]string{
		"PATH":                     binDir + string(os.PathListSeparator) + "/usr/bin:/bin:/usr/sbin:/sbin",
		"FAKE_LOG":                 logFile,
		"PLORIGO_OS_RELEASE":       osFile,
		"PLORIGO_SYSTEMD_DIR":      systemdDir,
		"PLORIGO_APT_KEYRINGS_DIR": filepath.Join(dir, "keyrings"),
		"PLORIGO_APT_SOURCES_DIR":  sourcesDir,
		"PLORIGO_AGENT_BIN_PATH":   filepath.Join(dir, "usr-local-bin", "plorigo-agent"),
	}
	for k, v := range tc.env {
		env[k] = v
	}

	cmd := exec.Command("/bin/sh", args...)
	cmd.Dir = dir // a dir with no go.mod, so the no-binary-url path downloads from releases
	cmd.Env = envSlice(env)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	return installResult{
		stdout:     stdout.String(),
		stderr:     stderr.String(),
		log:        readFile(t, logFile),
		systemdDir: systemdDir,
		sourcesDir: sourcesDir,
		err:        err,
	}
}

func assertContains(t *testing.T, what, haystack string, wants []string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(haystack, w) {
			t.Fatalf("%s missing %q:\n%s", what, w, haystack)
		}
	}
}

func assertUnitFile(t *testing.T, systemdDir string) {
	t.Helper()
	unit := filepath.Join(systemdDir, "plorigo-agent.service")
	fi, err := os.Stat(unit)
	if err != nil {
		t.Fatalf("systemd unit not written: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("systemd unit mode = %o, want 600 (it carries the token)", perm)
	}
	body := readFile(t, unit)
	for _, w := range []string{
		"PLORIGO_CONTROL_PLANE=http://cp.example.test:9999",
		"PLORIGO_AGENT_TOKEN=" + installToken,
		"PLORIGO_AGENT_DATA_DIR=",
	} {
		if !strings.Contains(body, w) {
			t.Fatalf("systemd unit missing %q:\n%s", w, body)
		}
	}
}

// installerScript resolves scripts/install-agent.sh from this test file's location.
func installerScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	script := filepath.Join(filepath.Dir(thisFile), "..", "..", "scripts", "install-agent.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("installer script not found at %s: %v", script, err)
	}
	return script
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
