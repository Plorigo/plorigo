// Package clicore is the logic of the Plorigo CLI. It calls the control plane
// through the SAME generated ConnectRPC client the dashboard uses — proving the
// shared typed client works from the CLI (no hand-rolled HTTP).
package clicore

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Version is the CLI build version, overridden via -ldflags in releases.
var Version = "dev"

// Execute runs the root command.
func Execute() error {
	return rootCmd().Execute()
}

func rootCmd() *cobra.Command {
	var baseURL string

	root := &cobra.Command{
		Use:           "plorigo",
		Short:         "Plorigo CLI",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVar(&baseURL, "api", envOr("PLORIGO_API", "http://localhost:8080"),
		"control plane base URL")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
			return nil
		},
	})

	projectsCmd := &cobra.Command{Use: "projects", Short: "Manage projects"}
	projectsCmd.AddCommand(&cobra.Command{
		Use:   "list <workspace-id>",
		Short: "List projects in a workspace via the control plane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := controlplanev1connect.NewProjectServiceClient(http.DefaultClient, baseURL)
			resp, err := client.ListProjectsByWorkspace(context.Background(),
				connect.NewRequest(&controlplanev1.ListProjectsByWorkspaceRequest{WorkspaceId: args[0]}))
			if err != nil {
				return err
			}
			for _, p := range resp.Msg.GetProjects() {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", p.GetId(), p.GetName(), p.GetSlug())
			}
			return nil
		},
	})
	root.AddCommand(projectsCmd)

	return root
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
