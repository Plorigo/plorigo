package sources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// appState is the sealed payload carried across the GitHub App install redirect. Like oauthState it
// binds the handshake to the workspace + the user that started it, with a nonce echoed back as the
// `state` parameter and an expiry. GitHub does not send a code for an App install — it appends the
// installation_id to the setup URL — so the state is what proves the install was started here.
type appState struct {
	WorkspaceID string `json:"w"`
	UserID      string `json:"u"`
	Nonce       string `json:"n"`
	ExpiresAt   int64  `json:"e"`
}

// BeginAppInstall starts a GitHub App installation for a workspace: it authorizes the caller, seals
// the bound state, and returns the install URL (carrying the nonce as `state`) plus the sealed state
// to set as a cookie and verify on the setup callback.
func (s *service) BeginAppInstall(ctx context.Context, in BeginAuthInput) (BeginAuthResult, error) {
	if _, err := id.Parse(in.WorkspaceID); err != nil {
		return BeginAuthResult{}, problem.InvalidInput("a valid workspace_id is required")
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: in.WorkspaceID}); err != nil {
		return BeginAuthResult{}, err
	}
	if !s.app.Configured() {
		return BeginAuthResult{}, problem.InvalidInput("the GitHub App is not configured on this server")
	}

	nonce, err := newNonce()
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "begin app install")
	}
	payload, err := json.Marshal(appState{
		WorkspaceID: in.WorkspaceID,
		UserID:      caller.UserID,
		Nonce:       nonce,
		ExpiresAt:   time.Now().Add(stateTTL).Unix(),
	})
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "begin app install")
	}
	sealed, err := s.box.Seal(payload)
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "seal app state")
	}
	return BeginAuthResult{
		AuthorizeURL: s.app.InstallURL(nonce),
		State:        base64.RawURLEncoding.EncodeToString(sealed),
	}, nil
}

// CompleteAppInstall finishes the install on the setup callback: it verifies the sealed state
// against the echoed nonce, confirms the same user, resolves the installation's account, and stores
// the App connection for the workspace.
func (s *service) CompleteAppInstall(ctx context.Context, in CompleteAppInput) (CompleteAuthResult, error) {
	if strings.TrimSpace(in.InstallationID) == "" {
		return CompleteAuthResult{}, problem.InvalidInput("missing installation id")
	}
	st, err := s.openAppState(in.CookieState)
	if err != nil {
		return CompleteAuthResult{}, err
	}
	if in.State == "" || st.Nonce != in.State {
		return CompleteAuthResult{}, problem.InvalidInput("install state mismatch; please try connecting again")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return CompleteAuthResult{}, problem.InvalidInput("the GitHub App install request expired; please try again")
	}

	caller := principal.FromContext(ctx)
	if !caller.IsAuthenticated() || caller.UserID != st.UserID {
		return CompleteAuthResult{}, problem.PermissionDenied("this GitHub App install was started by a different session")
	}
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: st.WorkspaceID}); err != nil {
		return CompleteAuthResult{}, err
	}

	// Resolve the installation's account (login) to display the connection, authenticating as the
	// App. This also validates that the installation id is real and belongs to this App.
	inst, err := s.gh.GetInstallation(ctx, in.InstallationID)
	if err != nil {
		return CompleteAuthResult{}, mapGitHubErr(err)
	}

	userID := caller.UserID
	accountID := inst.AccountID
	var conn Connection
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		conn, txErr = s.store.UpsertAppConnection(ctx, tx, AppConnectionWrite{
			WorkspaceID:    st.WorkspaceID,
			GitHubLogin:    inst.Account,
			GitHubUserID:   &accountID,
			InstallationID: in.InstallationID,
			ConnectedBy:    &userID,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.github_app.connect", "source_connection", conn.ID, st.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return CompleteAuthResult{}, mapErr(err, "connect github app")
	}
	// Log the account + installation id, never a token (the App connection mints tokens on demand).
	s.log.Info("github app installed", "workspace_id", st.WorkspaceID, "github_login", inst.Account, "installation_id", in.InstallationID, "actor", caller.UserID)
	return CompleteAuthResult{WorkspaceID: st.WorkspaceID, GitHubLogin: inst.Account}, nil
}

// InstallationToken mints a short-lived installation access token for a workspace's connected App
// installation, for server-side private-repo reads. It is an internal credential-resolution seam:
// the caller (deploy/preview path) is already authorized, and the token is never returned by an RPC,
// logged, or sent to the agent. ok is false when no App installation is connected.
func (s *service) InstallationToken(ctx context.Context, workspaceID string) (string, bool, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return "", false, problem.InvalidInput("a valid workspace_id is required")
	}
	installationID, ok, err := s.store.InstallationForWorkspace(ctx, workspaceID)
	if err != nil {
		return "", false, problem.Internalf(err, "resolve installation")
	}
	if !ok {
		return "", false, nil
	}
	token, err := s.gh.InstallationToken(ctx, installationID)
	if err != nil {
		return "", false, mapGitHubErr(err)
	}
	return token, true, nil
}

func (s *service) openAppState(cookie string) (appState, error) {
	if strings.TrimSpace(cookie) == "" {
		return appState{}, problem.InvalidInput("missing install state; please try connecting again")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return appState{}, problem.InvalidInput("invalid install state; please try connecting again")
	}
	plain, err := s.box.Open(sealed)
	if err != nil {
		return appState{}, problem.InvalidInput("invalid install state; please try connecting again")
	}
	var st appState
	if err := json.Unmarshal(plain, &st); err != nil {
		return appState{}, problem.InvalidInput("invalid install state; please try connecting again")
	}
	return st, nil
}
