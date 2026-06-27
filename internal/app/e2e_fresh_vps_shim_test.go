// Hermetic tests for scripts/e2e-fresh-vps.sh (the PLO-96 fresh-VPS verification driver).
// They run it in --dry-run, which prints the verification PLAN without touching any server or
// network, so they belong in the normal CI suite (no build tag). They guard the driver's logic
// — that it plans every acceptance check and validates its inputs — and, critically, that it
// NEVER prints the auth header or the bootstrap password (a redaction bug there would leak a
// credential into the release-gate log). The LIVE run against real Ubuntu 22.04/24.04 VPSes is
// the manual sign-off documented in docs/verification/fresh-vps-e2e.md.
package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFreshVPSDriverDryRunPlan(t *testing.T) {
	// Secrets deliberately carry regex/shell metacharacters, so the test also proves the
	// driver's literal (non-sed) redaction can't be defeated by an awkward password.
	const pwSecret = "p@ss/w&rd|with*meta"
	out, code := runFreshVPS(t, map[string]string{
		"PLORIGO_CP_URL":       "https://cp.example.com",
		"PLORIGO_AUTH_HEADER":  "Authorization: Bearer plo_SUPERSECRET|with*meta",
		"PLORIGO_WORKSPACE_ID": "ws-123",
		"E2E_MANUAL_SSH":       "root@203.0.113.10",
		"E2E_MANAGED_HOST":     "203.0.113.20",
		"E2E_MANAGED_USER":     "root",
		"E2E_MANAGED_PASSWORD": pwSecret,
	}, "--dry-run")

	if code != 0 {
		t.Fatalf("dry-run exited %d, want 0\n%s", code, out)
	}
	// It plans every acceptance check it can without a live server: the manual 22.04 path, the
	// managed 24.04 SSH path, the readiness poll, and the idempotent re-run.
	for _, want := range []string{
		"AC1: fresh Ubuntu 22.04",
		"AC2: fresh Ubuntu 24.04",
		"AC6: re-running the manual install is idempotent",
		"ServerService/CreateServer",
		"ServerSetupService/StartSetup",
		"AgentService/ListAgentsByWorkspace",
		"3 passed, 0 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run plan missing %q\n%s", want, out)
		}
	}
	// Redaction: neither the auth header nor the bootstrap password may appear anywhere.
	for _, secret := range []string{"plo_SUPERSECRET", pwSecret, "p@ss/w&rd"} {
		if strings.Contains(out, secret) {
			t.Errorf("SECURITY: driver leaked a secret (%q) into its output\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "***REDACTED***") {
		t.Errorf("expected redaction markers in the plan\n%s", out)
	}
}

func TestFreshVPSDriverRequiresEnv(t *testing.T) {
	out, code := runFreshVPS(t, map[string]string{}, "--dry-run")
	if code == 0 {
		t.Fatalf("expected a non-zero exit when required env is missing, got 0\n%s", out)
	}
	if !strings.Contains(out, "missing required environment") {
		t.Errorf("expected a missing-env error, got\n%s", out)
	}
}

// runFreshVPS resolves and runs scripts/e2e-fresh-vps.sh with a clean env (only PATH plus the
// provided vars), returning its combined output and exit code.
func runFreshVPS(t *testing.T, env map[string]string, args ...string) (string, int) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	script := filepath.Join(filepath.Dir(thisFile), "..", "..", "scripts", "e2e-fresh-vps.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("driver not found at %s: %v", script, err)
	}

	cmd := exec.Command("/bin/sh", append([]string{script}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running driver: %v\n%s", err, out)
	}
	return string(out), code
}
