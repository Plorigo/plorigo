package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/webhooks"
)

// maxWebhookBytes caps the webhook body read (GitHub pull_request payloads are well under this); it
// bounds memory against a hostile sender. The whole body must be buffered because the HMAC is
// computed over the RAW bytes.
const maxWebhookBytes = 5 << 20 // 5 MiB

// githubWebhookPayload is the subset of a GitHub `pull_request` webhook delivery Plorigo parses.
type githubWebhookPayload struct {
	Action       string `json:"action"`
	Number       int32  `json:"number"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Merged bool `json:"merged"`
	} `json:"pull_request"`
}

// githubWebhookHandler is the inbound GitHub App webhook endpoint. It verifies the HMAC-SHA256
// signature against GITHUB_WEBHOOK_SECRET over the RAW body BEFORE parsing anything, then dispatches
// pull_request events to the webhooks service (which re-scopes them to the installation's workspace
// and the repo's services). Verification fails closed: an unset secret rejects every delivery. This
// is the one external entry point that drives deployments, so it is deliberately strict.
func (a *App) githubWebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes))
		if err != nil {
			http.Error(w, "could not read body", http.StatusBadRequest)
			return
		}
		// The webhook secret is resolved through githubapp (env, or the registered App), so a
		// dashboard-registered App's secret verifies without a restart. Empty (no App configured)
		// fails closed.
		if !github.VerifyWebhookSignature(a.githubapp.WebhookSecret(r.Context()), body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		switch r.Header.Get("X-GitHub-Event") {
		case "ping":
			w.WriteHeader(http.StatusOK)
			return
		case "pull_request":
			// handled below
		default:
			// Verified, but not an event we act on — acknowledge so GitHub doesn't redeliver.
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var p githubWebhookPayload
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, "could not parse payload", http.StatusBadRequest)
			return
		}
		installationID := ""
		if p.Installation.ID > 0 {
			installationID = strconv.FormatInt(p.Installation.ID, 10)
		}
		if _, err := a.webhooks.Service().HandlePullRequest(r.Context(), webhooks.PullRequestEvent{
			Action:         p.Action,
			InstallationID: installationID,
			Owner:          p.Repository.Owner.Login,
			Repo:           p.Repository.Name,
			PRNumber:       p.Number,
			Merged:         p.PullRequest.Merged,
		}); err != nil {
			// A systemic failure (e.g. the database is down) — 500 so GitHub retries later.
			// Per-service failures are handled inside HandlePullRequest and don't surface here.
			http.Error(w, "could not handle webhook", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
