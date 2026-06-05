// Command controlplane is the Plorigo control plane: API + dashboard + workers.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/plorigo/plorigo/internal/app"
	"github.com/plorigo/plorigo/internal/platform/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, config.Load())
	if err != nil {
		fmt.Fprintln(os.Stderr, "control plane: startup failed:", err)
		os.Exit(1)
	}
	if err := a.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "control plane: exited with error:", err)
		os.Exit(1)
	}
}
