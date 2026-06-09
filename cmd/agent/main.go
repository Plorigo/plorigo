// Command agent is the Plorigo server agent that runs on your servers. It registers
// with the control plane using a one-time token, heartbeats so the dashboard can show
// the server online, and polls for deployment jobs it runs via Docker. See
// docs/architecture/agent.md. Docker is reached through the standard environment
// (DOCKER_HOST, honored by the Docker client).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plorigo/plorigo/internal/agentcore"
)

func main() {
	var opts agentcore.Options
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	fs.StringVar(&opts.ControlPlaneURL, "control-plane", os.Getenv("PLORIGO_CONTROL_PLANE"), "control plane base URL")
	fs.StringVar(&opts.RegistrationToken, "token", os.Getenv("PLORIGO_AGENT_TOKEN"), "one-time registration token (first run only)")
	fs.StringVar(&opts.DataDir, "data-dir", os.Getenv("PLORIGO_AGENT_DATA_DIR"), "directory for the agent's identity (default: user config dir)")
	fs.DurationVar(&opts.PollInterval, "poll-interval", envDuration("PLORIGO_AGENT_POLL_INTERVAL"), "how often to poll for deployment work when idle (default 4s)")
	showVersion := fs.Bool("version", false, "print version and exit")
	// flag.ExitOnError makes Parse exit the process on a bad flag, so the error is nil.
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Println(agentcore.Version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agentcore.Run(ctx, os.Stdout, opts); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
		os.Exit(1)
	}
}

// envDuration parses a duration from env, or returns 0 so agentcore applies its default.
func envDuration(key string) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 0
}
