// Package agentcore is the logic of the Plorigo server agent. In the scaffold it is
// a real but minimal program: it loads and validates configuration and prints
// version/build info (doctor-style startup). The privileged outbound connect /
// registration loop and Docker/Caddy execution are deferred to the `agents` module
// work — they require real Docker testing and extra review (see
// docs/architecture/agent.md and docs/architecture/security.md).
package agentcore

import (
	"fmt"
	"io"

	"github.com/plorigo/plorigo/internal/platform/config"
)

// Version is the agent build version, overridden via -ldflags in releases.
var Version = "dev"

// Run validates configuration and prints version/build info.
func Run(out io.Writer) error {
	cfg := config.Load()
	fmt.Fprintf(out, "plorigo agent %s\n", Version)
	fmt.Fprintf(out, "configuration loaded (dev=%t)\n", cfg.Dev)
	return nil
}
