// Command cli is the Plorigo command-line interface.
package main

import (
	"os"

	"github.com/plorigo/plorigo/internal/clicore"
)

func main() {
	if err := clicore.Execute(); err != nil {
		os.Exit(1)
	}
}
