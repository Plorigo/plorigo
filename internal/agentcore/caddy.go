package agentcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCaddyBin        = "caddy"
	defaultCaddyBaseDomain = "localhost"
	defaultCaddyHTTPPort   = 80
	defaultCaddyAdmin      = "localhost:2019"
	caddyManagedMarker     = "# plorigo-managed-caddyfile: true"
	caddyCommandTimeout    = 20 * time.Second
)

type caddyRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// deploymentRouter owns the reverse-proxy route switch for a deployment. Caddy is the
// production implementation; tests use a fake so deployment sequencing stays unit-testable.
type deploymentRouter interface {
	apply(ctx context.Context, routes []managedRoute) ([]string, error)
	routeURL(serviceID string) (string, error)
}

// managedRoute is the desired Caddy route for one running Plorigo-managed container. It is
// keyed by the service id (the route host label), so two services in one environment get
// distinct hosts.
type managedRoute struct {
	ServiceID    string
	DeploymentID string
	ContainerID  string
	HostPort     int32
	CustomHosts  []string
}

type caddyManager struct {
	bin        string
	configPath string
	baseDomain string
	httpPort   int
	admin      string
	run        caddyRunner
}

func newCaddyManager(opts Options) (*caddyManager, error) {
	bin := strings.TrimSpace(opts.CaddyBin)
	if bin == "" {
		bin = defaultCaddyBin
	}
	baseDomain := strings.ToLower(strings.TrimSpace(opts.CaddyBaseDomain))
	if baseDomain == "" {
		baseDomain = defaultCaddyBaseDomain
	}
	if err := validateDomainName(baseDomain); err != nil {
		return nil, fmt.Errorf("caddy base domain: %w", err)
	}
	httpPort := opts.CaddyHTTPPort
	if httpPort == 0 {
		httpPort = defaultCaddyHTTPPort
	}
	if err := validatePort("caddy http port", httpPort); err != nil {
		return nil, err
	}
	admin := strings.TrimSpace(opts.CaddyAdmin)
	if admin == "" {
		admin = defaultCaddyAdmin
	}
	if err := validateCaddyToken("caddy admin address", admin); err != nil {
		return nil, err
	}
	configPath := strings.TrimSpace(opts.CaddyConfig)
	if configPath == "" {
		configPath = filepath.Join(opts.DataDir, "Caddyfile")
	}
	if configPath == "" {
		return nil, fmt.Errorf("caddy config path is required")
	}
	return &caddyManager{
		bin:        bin,
		configPath: configPath,
		baseDomain: baseDomain,
		httpPort:   httpPort,
		admin:      admin,
		run:        runCaddyCommand,
	}, nil
}

func runCaddyCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, caddyCommandTimeout)
	defer cancel()

	output, err := os.CreateTemp("", "plorigo-caddy-command-*.log")
	if err != nil {
		return nil, fmt.Errorf("create Caddy command log: %w", err)
	}
	outputPath := output.Name()
	defer func() { _ = os.Remove(outputPath) }()

	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()

	closeErr := output.Close()
	out, readErr := os.ReadFile(outputPath)
	if commandCtx.Err() != nil {
		return out, commandCtx.Err()
	}
	if err != nil {
		return out, err
	}
	if closeErr != nil {
		return out, fmt.Errorf("close Caddy command log: %w", closeErr)
	}
	if readErr != nil {
		return out, fmt.Errorf("read Caddy command log: %w", readErr)
	}
	return out, nil
}

func (m *caddyManager) routeURL(serviceID string) (string, error) {
	host, err := routeHost(serviceID, m.baseDomain)
	if err != nil {
		return "", err
	}
	if m.httpPort == 80 {
		return "http://" + host, nil
	}
	return "http://" + host + ":" + strconv.Itoa(m.httpPort), nil
}

func (m *caddyManager) apply(ctx context.Context, routes []managedRoute) ([]string, error) {
	rendered, err := m.render(routes)
	if err != nil {
		return nil, err
	}
	old, hadOld, err := m.readExistingConfig()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Caddy config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".plorigo-caddy-*.caddyfile")
	if err != nil {
		return nil, fmt.Errorf("create temporary Caddy config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.WriteString(rendered); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write temporary Caddy config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temporary Caddy config: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return nil, fmt.Errorf("chmod temporary Caddy config: %w", err)
	}

	var logs []string
	out, err := m.run(ctx, m.bin, "validate", "--config", tmpPath, "--adapter", "caddyfile")
	logs = appendCommandOutput(logs, "caddy validate", out)
	if err != nil {
		return logs, commandError("validate Caddy config", err, out)
	}

	if err := os.Rename(tmpPath, m.configPath); err != nil {
		return logs, fmt.Errorf("activate Caddy config: %w", err)
	}
	out, err = m.run(ctx, m.bin, "reload", "--config", m.configPath, "--adapter", "caddyfile", "--address", m.admin)
	logs = appendCommandOutput(logs, "caddy reload", out)
	if err != nil {
		startOut, startErr := m.run(ctx, m.bin, "start", "--config", m.configPath, "--adapter", "caddyfile")
		logs = appendCommandOutput(logs, "caddy start", startOut)
		if startErr == nil {
			return logs, nil
		}
		if restoreErr := restoreConfig(m.configPath, old, hadOld); restoreErr != nil {
			return logs, fmt.Errorf("%w; starting Caddy also failed: %v; additionally could not restore previous Caddy config: %v", commandError("reload Caddy config", err, out), commandError("start Caddy", startErr, startOut), restoreErr)
		}
		return logs, fmt.Errorf("%w; starting Caddy also failed: %v", commandError("reload Caddy config", err, out), commandError("start Caddy", startErr, startOut))
	}
	return logs, nil
}

func (m *caddyManager) readExistingConfig() ([]byte, bool, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read existing Caddy config: %w", err)
	}
	if !strings.Contains(string(data), caddyManagedMarker) {
		return nil, false, fmt.Errorf("refusing to overwrite non-Plorigo Caddy config at %s", m.configPath)
	}
	return data, true, nil
}

func restoreConfig(path string, old []byte, hadOld bool) error {
	if !hadOld {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.WriteFile(path, old, 0o600); err != nil {
		return err
	}
	return nil
}

func (m *caddyManager) render(routes []managedRoute) (string, error) {
	normalized, err := normalizeRoutes(routes)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(caddyManagedMarker)
	b.WriteString("\n# Generated by the Plorigo agent. Do not edit by hand.\n")
	b.WriteString("{\n")
	fmt.Fprintf(&b, "\tadmin %s\n", m.admin)
	b.WriteString("\tauto_https off\n")
	b.WriteString("}\n\n")
	for _, r := range normalized {
		host, err := routeHost(r.ServiceID, m.baseDomain)
		if err != nil {
			return "", err
		}
		for _, siteHost := range append([]string{host}, r.CustomHosts...) {
			fmt.Fprintf(&b, "%s {\n", siteAddress(siteHost, m.httpPort))
			fmt.Fprintf(&b, "\treverse_proxy 127.0.0.1:%d\n", r.HostPort)
			b.WriteString("}\n\n")
		}
	}
	return b.String(), nil
}

func normalizeRoutes(routes []managedRoute) ([]managedRoute, error) {
	byService := make(map[string]managedRoute, len(routes))
	seenHosts := map[string]string{}
	for _, r := range routes {
		svc := strings.ToLower(strings.TrimSpace(r.ServiceID))
		if err := validateDNSLabel("service route label", svc); err != nil {
			return nil, err
		}
		if r.HostPort <= 0 || r.HostPort > 65535 {
			return nil, fmt.Errorf("route for service %s has invalid host port %d", svc, r.HostPort)
		}
		customHosts, err := normalizeCustomHosts(r.CustomHosts)
		if err != nil {
			return nil, err
		}
		r.ServiceID = svc
		r.CustomHosts = customHosts
		cur, ok := byService[svc]
		if !ok || routeTieBreak(r, cur) > 0 {
			byService[svc] = r
		}
	}
	out := make([]managedRoute, 0, len(byService))
	for _, r := range byService {
		for _, host := range r.CustomHosts {
			if owner, ok := seenHosts[host]; ok && owner != r.ServiceID {
				return nil, fmt.Errorf("custom host %s is attached to multiple services", host)
			}
			seenHosts[host] = r.ServiceID
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServiceID < out[j].ServiceID
	})
	return out, nil
}

func normalizeCustomHosts(hosts []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		host := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(h, ".")))
		if host == "" || seen[host] {
			continue
		}
		if strings.Contains(host, "*") {
			return nil, fmt.Errorf("custom host %q uses a wildcard, which is not supported yet", host)
		}
		if err := validateDomainName(host); err != nil {
			return nil, fmt.Errorf("custom host %q: %w", host, err)
		}
		seen[host] = true
		out = append(out, host)
	}
	sort.Strings(out)
	return out, nil
}

func routeTieBreak(a, b managedRoute) int {
	if a.DeploymentID != b.DeploymentID {
		if a.DeploymentID > b.DeploymentID {
			return 1
		}
		return -1
	}
	if a.ContainerID > b.ContainerID {
		return 1
	}
	if a.ContainerID < b.ContainerID {
		return -1
	}
	return 0
}

func routeHost(serviceID, baseDomain string) (string, error) {
	svc := strings.ToLower(strings.TrimSpace(serviceID))
	if err := validateDNSLabel("service route label", svc); err != nil {
		return "", err
	}
	base := strings.ToLower(strings.TrimSpace(baseDomain))
	if err := validateDomainName(base); err != nil {
		return "", err
	}
	return svc + "." + base, nil
}

func siteAddress(host string, port int) string {
	if port == 80 {
		return "http://" + host
	}
	return "http://" + host + ":" + strconv.Itoa(port)
}

func validateDomainName(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain is too long")
	}
	if strings.Contains(domain, "://") {
		return fmt.Errorf("domain must not include a scheme")
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if err := validateDNSLabel("domain label", label); err != nil {
			return err
		}
	}
	return nil
}

func validateDNSLabel(name, label string) error {
	if label == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(label) > 63 {
		return fmt.Errorf("%s %q is too long", name, label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("%s %q must not start or end with '-'", name, label)
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("%s %q contains invalid character %q", name, label, r)
	}
	return nil
}

func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", name)
	}
	return nil
}

func validateCaddyToken(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(value, " \t\r\n{}") {
		return fmt.Errorf("%s must not contain whitespace or Caddyfile control characters", name)
	}
	return nil
}

func appendCommandOutput(logs []string, prefix string, out []byte) []string {
	for _, line := range tailLines(string(out), maxReportLogLines) {
		logs = append(logs, prefix+": "+line)
	}
	return logs
}

func commandError(action string, err error, out []byte) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s: Caddy CLI was not found in PATH; install Caddy or set --caddy-bin / PLORIGO_AGENT_CADDY_BIN to its path", action)
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, msg)
}
