package agents

import (
	"strings"
	"testing"
)

// installCommand must follow the environment the control plane runs in: production
// fetches the public installer script; dev runs the agent from the local checkout so a
// developer tests their working copy, not the published agent.
func TestInstallCommand(t *testing.T) {
	prod := installCommand("https://cp.example.com", "plrt_tok", false)
	if !strings.HasPrefix(prod, "curl -fsSL "+agentInstallScript) {
		t.Errorf("prod command = %q, want the public installer script", prod)
	}
	if !strings.Contains(prod, "--control-plane https://cp.example.com") || !strings.Contains(prod, "--token plrt_tok") {
		t.Errorf("prod command = %q, want control plane + token flags", prod)
	}

	dev := installCommand("http://localhost:8080", "plrt_tok", true)
	if !strings.HasPrefix(dev, "go run ./cmd/agent") {
		t.Errorf("dev command = %q, want a go run form for the local checkout", dev)
	}
	if strings.Contains(dev, agentInstallScript) {
		t.Errorf("dev command = %q, must not fetch the published installer", dev)
	}
	if !strings.Contains(dev, "--control-plane http://localhost:8080") || !strings.Contains(dev, "--token plrt_tok") {
		t.Errorf("dev command = %q, want control plane + token flags", dev)
	}
	if !strings.Contains(dev, "--caddy-config .context/plorigo-agent.Caddyfile") ||
		!strings.Contains(dev, "--caddy-http-port 8083") ||
		!strings.Contains(dev, "--caddy-admin 127.0.0.1:8084") {
		t.Errorf("dev command = %q, want local Caddy config and non-privileged ports", dev)
	}
}
