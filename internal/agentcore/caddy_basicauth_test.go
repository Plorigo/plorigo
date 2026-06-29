package agentcore

import (
	"strings"
	"testing"
)

func TestCaddyRender_BasicAuthForProtectedPreview(t *testing.T) {
	m := testCaddyManager(t)
	got, err := m.render([]managedRoute{{
		ServiceID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		DeploymentID:  "dep",
		ContainerID:   "c",
		HostPort:      3001,
		BasicAuthUser: "alice",
		BasicAuthHash: "$2a$14$abcdefghijklmnopqrstuvCQ",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "basic_auth {") {
		t.Fatalf("rendered config missing basic_auth block:\n%s", got)
	}
	if !strings.Contains(got, "alice $2a$14$abcdefghijklmnopqrstuvCQ") {
		t.Fatalf("rendered config missing the username + hash:\n%s", got)
	}
	// The basic_auth block precedes the proxy, so auth is enforced before traffic reaches the app.
	if strings.Index(got, "basic_auth {") > strings.Index(got, "reverse_proxy") {
		t.Fatalf("basic_auth must come before reverse_proxy:\n%s", got)
	}
}

func TestCaddyRender_UsesRouteHostForPreview(t *testing.T) {
	m := testCaddyManager(t)
	got, err := m.render([]managedRoute{{
		ServiceID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		DeploymentID: "dep",
		ContainerID:  "c",
		HostPort:     3001,
		RouteHost:    "my-app-pr-7-abc123",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "my-app-pr-7-abc123.") {
		t.Fatalf("rendered config should serve the pretty RouteHost:\n%s", got)
	}
	if strings.Contains(got, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.") {
		t.Fatalf("a preview must not route on the UUID service-id host:\n%s", got)
	}
}

func TestCaddyRender_FallsBackToServiceIDHost(t *testing.T) {
	m := testCaddyManager(t)
	got, err := m.render([]managedRoute{{
		ServiceID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		DeploymentID: "dep",
		ContainerID:  "c",
		HostPort:     3001,
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.") {
		t.Fatalf("production (no RouteHost) must route on the service-id host:\n%s", got)
	}
}

func TestCaddyRender_NoBasicAuthWhenUnset(t *testing.T) {
	m := testCaddyManager(t)
	got, err := m.render([]managedRoute{{
		ServiceID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		DeploymentID: "dep",
		ContainerID:  "c",
		HostPort:     3001,
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(got, "basic_auth") {
		t.Fatalf("an unprotected route must not render basic_auth:\n%s", got)
	}
}
