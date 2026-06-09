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
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
)

// Version is the agent build version, overridden via -ldflags in releases.
var Version = "dev"

const (
	defaultHeartbeat = 30 * time.Second
	maxBackoff       = 60 * time.Second
	stateFileName    = "agent.json"
)

// Options configure the agent; cmd/agent fills them from flags/env.
type Options struct {
	ControlPlaneURL   string        // base URL of the control plane (the outbound target)
	RegistrationToken string        // one-time token, needed only on the first run
	DataDir           string        // where the agent persists its identity (created 0700)
	HeartbeatInterval time.Duration // fallback when the control plane doesn't specify one
}

// state is the agent's persisted identity. The credential and private key are secret,
// so the file is written 0600 inside a 0700 directory. The private key is stored for
// the next step (signing jobs); registration only sends the public key.
type state struct {
	AgentID       string `json:"agent_id"`
	Credential    string `json:"credential"`
	PrivateKeyB64 string `json:"private_key"`
}

// Run registers if there is no stored identity, then heartbeats until ctx is cancelled.
func Run(ctx context.Context, out io.Writer, opts Options) error {
	if opts.ControlPlaneURL == "" {
		return errors.New("control plane URL is required (set --control-plane or PLORIGO_CONTROL_PLANE)")
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultHeartbeat
	}
	if opts.DataDir == "" {
		opts.DataDir = defaultDataDir()
	}

	client := agentv1connect.NewAgentServiceClient(http.DefaultClient, opts.ControlPlaneURL)

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
		fmt.Fprintf(out, "registered as agent %s\n", st.AgentID)
	} else {
		fmt.Fprintf(out, "plorigo agent %s: resuming as agent %s\n", Version, st.AgentID)
	}

	return heartbeatLoop(ctx, out, client, st, opts.HeartbeatInterval)
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
func heartbeatLoop(ctx context.Context, out io.Writer, client agentv1connect.AgentServiceClient, st *state, interval time.Duration) error {
	backoff := time.Second
	for {
		resp, err := client.Heartbeat(ctx, connect.NewRequest(&agentv1.HeartbeatRequest{
			AgentId:      st.AgentID,
			Credential:   st.Credential,
			AgentVersion: Version,
		}))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "heartbeat failed (retrying in %s): %v\n", backoff, err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		next := interval
		if s := resp.Msg.GetNextIntervalSeconds(); s > 0 {
			next = time.Duration(s) * time.Second
		}
		if !sleep(ctx, next) {
			return nil
		}
	}
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
