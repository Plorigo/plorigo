package serversetup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshDialer is the production SSHDialer: it opens a real SSH connection with the one-time
// bootstrap credentials and enforces TOFU host-key pinning. This is the thin boundary to the
// network — the bootstrap logic that drives the returned executor is unit-tested against a
// fake, while this adapter is exercised end-to-end against a real server (manual/integration).
type sshDialer struct {
	dialTimeout time.Duration
}

// NewSSHDialer returns the production SSH dialer.
func NewSSHDialer() SSHDialer { return sshDialer{dialTimeout: 15 * time.Second} }

func (d sshDialer) Dial(ctx context.Context, t DialTarget) (SSHExecutor, string, error) {
	auth, err := authMethods(t)
	if err != nil {
		return nil, "", err
	}

	var fingerprint string
	var mismatch bool
	cfg := &ssh.ClientConfig{
		User: t.Username,
		Auth: auth,
		// TOFU: capture the fingerprint; reject only when a pin exists and differs.
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			if t.PinnedHostKeyFingerprint != "" && fingerprint != t.PinnedHostKeyFingerprint {
				mismatch = true
				return ErrHostKeyMismatch
			}
			return nil
		},
		Timeout: d.dialTimeout,
	}

	port := t.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(port))

	conn, err := (&net.Dialer{Timeout: d.dialTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("dial %s: %w", addr, err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		switch {
		case mismatch:
			return nil, fingerprint, ErrHostKeyMismatch
		case isAuthError(err):
			return nil, fingerprint, fmt.Errorf("%w: %v", ErrAuth, err)
		default:
			return nil, fingerprint, fmt.Errorf("ssh handshake with %s: %w", addr, err)
		}
	}
	return &sshSession{client: ssh.NewClient(sc, chans, reqs)}, fingerprint, nil
}

func authMethods(t DialTarget) ([]ssh.AuthMethod, error) {
	if len(t.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if t.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(t.PrivateKey, []byte(t.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(t.PrivateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: parse private key: %v", ErrAuth, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if t.Password != "" {
		return []ssh.AuthMethod{ssh.Password(t.Password)}, nil
	}
	return nil, fmt.Errorf("%w: no password or private key provided", ErrAuth)
}

func isAuthError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unable to authenticate")
}

// sshSession runs commands over an established SSH client, one session per command.
type sshSession struct {
	client *ssh.Client
}

func (s *sshSession) Run(ctx context.Context, cmd string) (ExecResult, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return ExecResult{}, err
	}
	defer func() { _ = sess.Close() }()

	var stdout, stderr strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return ExecResult{}, ctx.Err()
	case runErr := <-done:
		res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
		var exitErr *ssh.ExitError
		if errors.As(runErr, &exitErr) {
			// A non-zero exit is a command result the caller interprets, not a transport error.
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		return res, runErr
	}
}

func (s *sshSession) Close() error { return s.client.Close() }
