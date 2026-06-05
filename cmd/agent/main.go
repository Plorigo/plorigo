// Command agent is the Plorigo server agent that runs on your servers.
package main

import (
	"fmt"
	"os"

	"github.com/plorigo/plorigo/internal/agentcore"
)

func main() {
	if err := agentcore.Run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
		os.Exit(1)
	}
}
