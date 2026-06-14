// Package agentcore is the logic of the Plorigo server agent: a small program that
// runs on a connected server. On first run it generates an ed25519 keypair and
// exchanges a one-time registration token for a durable credential; thereafter it
// sends periodic heartbeats over an OUTBOUND connection so the control plane can show
// the server online. It persists its identity locally and reconnects after a restart
// or network drop. It manages no Docker or Caddy yet — that is the next step (see
// docs/architecture/agent.md and docs/architecture/security.md).
package agentcore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
)

// Version is the agent build version, overridden via -ldflags in releases.
var Version = "dev"

const (
	defaultHeartbeat = 30 * time.Second
	defaultPoll      = 4 * time.Second
	maxBackoff       = 60 * time.Second
	stateFileName    = "agent.json"
)

// Options configure the agent; cmd/agent fills them from flags/env.
type Options struct {
	ControlPlaneURL   string        // base URL of the control plane (the outbound target)
	RegistrationToken string        // one-time token, needed only on the first run
	DataDir           string        // where the agent persists its identity (created 0700)
	HeartbeatInterval time.Duration // fallback when the control plane doesn't specify one
	PollInterval      time.Duration // how often to poll for deployment work when idle
}

// state is the agent's persisted identity. The credential and private key are secret,
// so the file is written 0600 inside a 0700 directory. The private key is stored for
// the next step (signing jobs); registration only sends the public key.
type state struct {
	AgentID       string `json:"agent_id"`
	Credential    string `json:"credential"`
	PrivateKeyB64 string `json:"private_key"`
}

// identity is the agent's CURRENT identity, shared by the heartbeat and deploy loops.
// It is mutex-guarded because the heartbeat loop can swap it at runtime: when the
// control plane rejects the stored credential (e.g. the server was deleted from the
// dashboard and re-created) and a registration token is available, the agent
// re-registers with it instead of erroring forever (see heartbeatLoop).
type identity struct {
	mu sync.Mutex
	st state
}

func (i *identity) get() state {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.st
}

func (i *identity) set(st state) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.st = st
}

// Run registers if there is no stored identity, then heartbeats until ctx is cancelled.
func Run(ctx context.Context, out io.Writer, opts Options) error {
	if opts.ControlPlaneURL == "" {
		return errors.New("control plane URL is required (set --control-plane or PLORIGO_CONTROL_PLANE)")
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultHeartbeat
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPoll
	}
	if opts.DataDir == "" {
		opts.DataDir = defaultDataDir()
	}

	client := agentv1connect.NewAgentServiceClient(http.DefaultClient, opts.ControlPlaneURL)
	deployClient := agentv1connect.NewDeployServiceClient(http.DefaultClient, opts.ControlPlaneURL)

	st, err := loadState(opts.DataDir)
	if err != nil {
		return fmt.Errorf("load agent state: %w", err)
	}
	if st == nil {
		fmt.Fprintf(out, "plorigo agent %s: registering with %s\n", Version, opts.ControlPlaneURL)
		st, err = register(ctx, client, opts)
		if err != nil {
			return err
		}
		if err := saveState(opts.DataDir, st); err != nil {
			return fmt.Errorf("save agent state: %w", err)
		}
		fmt.Fprintf(out, "registered as agent %s (identity: %s)\n", st.AgentID, filepath.Join(opts.DataDir, stateFileName))
	} else {
		fmt.Fprintf(out, "plorigo agent %s: resuming as agent %s (identity: %s)\n", Version, st.AgentID, filepath.Join(opts.DataDir, stateFileName))
		if opts.RegistrationToken != "" {
			fmt.Fprintf(out, "a registration token was provided; it will be used to re-register if the control plane rejects the stored credential\n")
		}
	}
	ident := &identity{st: *st}

	// The agent now manages Docker. If the daemon is unreachable the agent keeps
	// heartbeating (so the server stays visible) and reports any claimed deployment as
	// failed with a clear message, rather than going down.
	dk, derr := newDockerClient()
	var runtime deploymentRuntime
	var prober dockerProber // left nil when the client can't be built, so health reports Docker unavailable
	if derr != nil {
		fmt.Fprintf(out, "warning: Docker is unavailable; deployments will be reported as failed: %v\n", derr)
	} else {
		runtime = dk
		prober = dk
	}
	defer dk.close()

	// The three loops below log concurrently to out, so serialize writes — a bare
	// *strings.Builder (tests) or os.Stdout isn't safe for concurrent, interleaved writes.
	out = &syncWriter{w: out}

	// Run the heartbeat, deploy, and runtime-log loops together. Each returns only when its
	// context ends, so if any returns, cancel the siblings and wait for all to unwind.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 3)
	go func() { errc <- heartbeatLoop(loopCtx, out, client, ident, prober, opts) }()
	go func() { errc <- deployLoop(loopCtx, out, deployClient, ident, runtime, opts.PollInterval) }()
	go func() { errc <- runtimeLogLoop(loopCtx, out, deployClient, ident, runtime, defaultRuntimeLogInterval) }()
	first := <-errc
	cancel()
	<-errc
	<-errc
	return first
}

// register generates the keypair and exchanges the one-time token for a credential.
func register(ctx context.Context, client agentv1connect.AgentServiceClient, opts Options) (*state, error) {
	if opts.RegistrationToken == "" {
		return nil, errors.New("no stored identity and no registration token (set --token or PLORIGO_AGENT_TOKEN)")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}
	resp, err := client.Register(ctx, connect.NewRequest(&agentv1.RegisterRequest{
		RegistrationToken: opts.RegistrationToken,
		PublicKey:         pub,
		AgentVersion:      Version,
	}))
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	return &state{
		AgentID:       resp.Msg.GetAgentId(),
		Credential:    resp.Msg.GetCredential(),
		PrivateKeyB64: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

// heartbeatLoop beats at the interval the control plane returns, backing off and
// reconnecting on failure, until ctx is cancelled.
//
// Self-heal: when the control plane REJECTS the stored credential (the server was
// deleted from the dashboard, or the agent was re-registered elsewhere) and a
// registration token was provided, the loop re-registers with it once — rotating the
// identity in place — instead of erroring forever with a stale identity. Tokens are
// single-use, so only one attempt is made per process run.
func heartbeatLoop(ctx context.Context, out io.Writer, client agentv1connect.AgentServiceClient, ident *identity, prober dockerProber, opts Options) error {
	backoff := time.Second
	reregisterTried := false
	for {
		st := ident.get()
		facts := collectHealth(ctx, prober)
		resp, err := client.Heartbeat(ctx, connect.NewRequest(&agentv1.HeartbeatRequest{
			AgentId:         st.AgentID,
			Credential:      st.Credential,
			AgentVersion:    Version,
			DockerAvailable: facts.DockerAvailable,
			DockerVersion:   facts.DockerVersion,
			Os:              facts.OS,
			Arch:            facts.Arch,
		}))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if connect.CodeOf(err) == connect.CodePermissionDenied && opts.RegistrationToken != "" && !reregisterTried {
				reregisterTried = true
				fmt.Fprintf(out, "stored credential rejected by the control plane; re-registering with the provided token\n")
				newSt, rerr := register(ctx, client, opts)
				if rerr == nil {
					if serr := saveState(opts.DataDir, newSt); serr != nil {
						fmt.Fprintf(out, "warning: could not persist the new identity: %v\n", serr)
					}
					ident.set(*newSt)
					fmt.Fprintf(out, "re-registered as agent %s\n", newSt.AgentID)
					backoff = time.Second
					continue
				}
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(out, "re-registration failed: %v (mint a fresh install command from the server card in the dashboard)\n", rerr)
			}
			fmt.Fprintf(out, "heartbeat failed (retrying in %s): %v\n", backoff, err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		next := opts.HeartbeatInterval
		if s := resp.Msg.GetNextIntervalSeconds(); s > 0 {
			next = time.Duration(s) * time.Second
		}
		if !sleep(ctx, next) {
			return nil
		}
	}
}

// syncWriter serializes concurrent writes to an io.Writer, so the heartbeat, deploy, and
// runtime-log loops can log to the same destination without interleaving or racing.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// sleep waits for d or until ctx is done; it reports false when ctx ended.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	if d *= 2; d > maxBackoff {
		return maxBackoff
	}
	return d
}

// --- identity persistence ---------------------------------------------------

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "plorigo", "agent")
	}
	return ".plorigo-agent"
}

func loadState(dir string) (*state, error) {
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.AgentID == "" || st.Credential == "" {
		return nil, nil // incomplete file: re-register
	}
	return &st, nil
}

func saveState(dir string, st *state) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFileName), data, 0o600)
}
