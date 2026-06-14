package agentcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCaddyRender_StableSortedRoutes(t *testing.T) {
	m := testCaddyManager(t)

	got, err := m.render([]managedRoute{
		{EnvironmentID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", DeploymentID: "dep-b", ContainerID: "c-b", HostPort: 3002},
		{EnvironmentID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DeploymentID: "dep-a", ContainerID: "c-a", HostPort: 3001},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, caddyManagedMarker) {
		t.Fatalf("rendered config missing managed marker:\n%s", got)
	}
	first := strings.Index(got, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.example.test")
	second := strings.Index(got, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb.example.test")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("routes are not rendered in stable environment order:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3001") || !strings.Contains(got, "reverse_proxy 127.0.0.1:3002") {
		t.Fatalf("rendered config missing reverse proxy targets:\n%s", got)
	}
}

func TestCaddyRender_RejectsUnsafeHostParts(t *testing.T) {
	m := testCaddyManager(t)
	if _, err := m.render([]managedRoute{{EnvironmentID: "bad label", DeploymentID: "dep", ContainerID: "c", HostPort: 3000}}); err == nil {
		t.Fatal("render accepted an unsafe environment label")
	}
	if _, err := newCaddyManager(Options{DataDir: t.TempDir(), CaddyBaseDomain: "https://example.com"}); err == nil {
		t.Fatal("newCaddyManager accepted a base domain with a scheme")
	}
}

func TestCaddyApply_ValidatesBeforeReloadAndCapturesOutput(t *testing.T) {
	m := testCaddyManager(t)
	var calls []string
	m.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "validate" {
			return []byte("valid config\n"), nil
		}
		return []byte("reloaded\n"), nil
	}

	logs, err := m.apply(context.Background(), []managedRoute{{EnvironmentID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DeploymentID: "dep", ContainerID: "c", HostPort: 3000}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "validate ") || !strings.HasPrefix(calls[1], "reload ") {
		t.Fatalf("calls = %v, want validate before reload", calls)
	}
	if !reflect.DeepEqual(logs, []string{"caddy validate: valid config", "caddy reload: reloaded"}) {
		t.Fatalf("logs = %v, want captured command output", logs)
	}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		t.Fatalf("read active config: %v", err)
	}
	if !strings.Contains(string(data), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.example.test") {
		t.Fatalf("active config missing route:\n%s", data)
	}
}

func TestCaddyApply_ReloadFailureRestoresPreviousConfig(t *testing.T) {
	m := testCaddyManager(t)
	old := []byte(caddyManagedMarker + "\n{\n\tadmin 127.0.0.1:2999\n}\n")
	if err := os.WriteFile(m.configPath, old, 0o600); err != nil {
		t.Fatalf("write old config: %v", err)
	}
	m.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "reload":
			return []byte("connect: connection refused\n"), errors.New("exit status 1")
		case "start":
			return []byte("listen tcp :8081: bind: address already in use\n"), errors.New("exit status 1")
		}
		return nil, nil
	}

	logs, err := m.apply(context.Background(), []managedRoute{{EnvironmentID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DeploymentID: "dep", ContainerID: "c", HostPort: 3000}})
	if err == nil || !strings.Contains(err.Error(), "reload Caddy config") {
		t.Fatalf("apply err = %v, want reload failure", err)
	}
	if !reflect.DeepEqual(logs, []string{"caddy reload: connect: connection refused", "caddy start: listen tcp :8081: bind: address already in use"}) {
		t.Fatalf("logs = %v, want reload and start output", logs)
	}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(data) != string(old) {
		t.Fatalf("config was not restored:\n%s", data)
	}
}

func TestCaddyApply_ReloadFailureStartsCaddy(t *testing.T) {
	m := testCaddyManager(t)
	var calls []string
	m.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		if args[0] == "reload" {
			return []byte("connect: connection refused\n"), errors.New("exit status 1")
		}
		if args[0] == "start" {
			return []byte("started\n"), nil
		}
		return nil, nil
	}

	logs, err := m.apply(context.Background(), []managedRoute{{EnvironmentID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DeploymentID: "dep", ContainerID: "c", HostPort: 3000}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"validate", "reload", "start"}) {
		t.Fatalf("calls = %v, want validate, reload, start", calls)
	}
	if !reflect.DeepEqual(logs, []string{"caddy reload: connect: connection refused", "caddy start: started"}) {
		t.Fatalf("logs = %v, want reload and start output", logs)
	}
}

func TestCaddyApply_RefusesNonPlorigoConfig(t *testing.T) {
	m := testCaddyManager(t)
	if err := os.WriteFile(m.configPath, []byte("localhost {\n\treverse_proxy 127.0.0.1:8080\n}\n"), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}
	if _, err := m.apply(context.Background(), []managedRoute{{EnvironmentID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DeploymentID: "dep", ContainerID: "c", HostPort: 3000}}); err == nil {
		t.Fatal("apply overwrote a non-Plorigo Caddyfile")
	}
}

func TestRunCaddyCommand_DoesNotWaitForBackgroundDescendantOutput(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is not available")
	}
	start := time.Now()
	out, err := runCaddyCommand(context.Background(), "/bin/sh", "-c", "sleep 2 & echo started")
	if err != nil {
		t.Fatalf("runCaddyCommand: %v, output: %s", err, out)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runCaddyCommand waited %s for a background descendant", elapsed)
	}
	if !strings.Contains(string(out), "started") {
		t.Fatalf("output = %q, want command output", out)
	}
}

func testCaddyManager(t *testing.T) *caddyManager {
	t.Helper()
	dir := t.TempDir()
	m, err := newCaddyManager(Options{
		DataDir:           dir,
		CaddyConfig:       filepath.Join(dir, "Caddyfile"),
		CaddyBaseDomain:   "example.test",
		CaddyHTTPPort:     8081,
		CaddyAdmin:        "127.0.0.1:2999",
		RegistrationToken: "unused",
		ControlPlaneURL:   "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("newCaddyManager: %v", err)
	}
	return m
}
