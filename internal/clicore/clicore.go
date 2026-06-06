// Package clicore is the logic of the Plorigo CLI. It calls the control plane
// through the SAME generated ConnectRPC client the dashboard uses (no hand-rolled
// HTTP). `plorigo login` exchanges an email/password for a long-lived API token,
// stored under the user's config dir and attached as a bearer token on every other
// command.
package clicore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Version is the CLI build version, overridden via -ldflags in releases.
var Version = "dev"

const sessionCookieName = "plorigo_session"

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

	root.AddCommand(loginCmd(&baseURL))
	root.AddCommand(logoutCmd())
	root.AddCommand(workspacesCmd(&baseURL))
	root.AddCommand(projectsCmd(&baseURL))

	return root
}

func loginCmd(baseURL *string) *cobra.Command {
	var email string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in and store an API token for this CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				email = envOr("PLORIGO_EMAIL", "")
			}
			if email == "" {
				return errors.New("provide --email (or set PLORIGO_EMAIL)")
			}
			password, err := readPassword(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			auth := controlplanev1connect.NewAuthServiceClient(http.DefaultClient, *baseURL)

			// Log in to obtain a session, then mint a long-lived API token with it.
			loginResp, err := auth.Login(ctx, connect.NewRequest(&controlplanev1.LoginRequest{Email: email, Password: password}))
			if err != nil {
				return err
			}
			session := sessionFromResponse(loginResp.Header())
			if session == "" {
				return errors.New("login did not return a session")
			}
			tokenReq := connect.NewRequest(&controlplanev1.CreateAPITokenRequest{Name: "cli@" + hostname()})
			tokenReq.Header().Set("Cookie", sessionCookieName+"="+session)
			tokenResp, err := auth.CreateAPIToken(ctx, tokenReq)
			if err != nil {
				return err
			}
			if err := saveCredential(credentials{APIURL: *baseURL, Token: tokenResp.Msg.GetToken()}); err != nil {
				return err
			}
			// Revoke the short-lived login session now that the long-lived API token is
			// stored — the CLI authenticates with the token from here on, so leaving the
			// session around would orphan a valid credential for its full lifetime.
			logoutReq := connect.NewRequest(&controlplanev1.LogoutRequest{})
			logoutReq.Header().Set("Cookie", sessionCookieName+"="+session)
			if _, err := auth.Logout(ctx, logoutReq); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: could not revoke the temporary login session:", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Logged in as %s.\n", email)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored API token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deleteCredential(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
			return nil
		},
	}
}

func workspacesCmd(baseURL *string) *cobra.Command {
	cmd := &cobra.Command{Use: "workspaces", Short: "Manage workspaces"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the workspaces you belong to",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := workspaceClient(*baseURL)
			if err != nil {
				return err
			}
			resp, err := client.ListMyWorkspaces(cmd.Context(), connect.NewRequest(&controlplanev1.ListMyWorkspacesRequest{}))
			if err != nil {
				return err
			}
			for _, w := range resp.Msg.GetWorkspaces() {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", w.GetId(), w.GetName(), w.GetSlug())
			}
			return nil
		},
	})
	return cmd
}

func projectsCmd(baseURL *string) *cobra.Command {
	cmd := &cobra.Command{Use: "projects", Short: "Manage projects"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <workspace-id>",
		Short: "List projects in a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := projectClient(*baseURL)
			if err != nil {
				return err
			}
			resp, err := client.ListProjectsByWorkspace(cmd.Context(),
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
	return cmd
}

// --- authenticated clients --------------------------------------------------

func workspaceClient(baseURL string) (controlplanev1connect.WorkspaceServiceClient, error) {
	opt, err := bearerOption()
	if err != nil {
		return nil, err
	}
	return controlplanev1connect.NewWorkspaceServiceClient(http.DefaultClient, baseURL, opt), nil
}

func projectClient(baseURL string) (controlplanev1connect.ProjectServiceClient, error) {
	opt, err := bearerOption()
	if err != nil {
		return nil, err
	}
	return controlplanev1connect.NewProjectServiceClient(http.DefaultClient, baseURL, opt), nil
}

// bearerOption attaches the stored API token to every request.
func bearerOption() (connect.ClientOption, error) {
	cred, err := loadCredential()
	if err != nil {
		return nil, fmt.Errorf("not logged in (run `plorigo login`): %w", err)
	}
	interceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+cred.Token)
			return next(ctx, req)
		}
	})
	return connect.WithInterceptors(interceptor), nil
}

// --- credential storage -----------------------------------------------------

type credentials struct {
	APIURL string `json:"api_url"`
	Token  string `json:"token"`
}

func credPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plorigo", "credentials.json"), nil
}

func saveCredential(c credentials) error {
	path, err := credPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadCredential() (credentials, error) {
	path, err := credPath()
	if err != nil {
		return credentials{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return credentials{}, err
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return credentials{}, err
	}
	return c, nil
}

func deleteCredential() error {
	path, err := credPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func sessionFromResponse(h http.Header) string {
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name == sessionCookieName {
			return c.Value
		}
	}
	return ""
}

// readPassword takes the password from PLORIGO_PASSWORD if set (non-interactive),
// otherwise prompts without echoing.
func readPassword(cmd *cobra.Command) (string, error) {
	if pw := os.Getenv("PLORIGO_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Fprint(cmd.OutOrStdout(), "Password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.OutOrStdout())
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
