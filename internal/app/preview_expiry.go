package app

import (
	"context"
	"time"
)

// previewExpirySweepInterval is how often the control plane scans for previews past their TTL. The
// TTL itself (cfg.PreviewTTL) is hours, so a coarse sweep is plenty; a teardown is idempotent, so
// the exact moment of reaping doesn't matter.
const previewExpirySweepInterval = 15 * time.Minute

// runPreviewExpiry periodically tears down preview deployments older than the configured TTL, until
// ctx is cancelled. It runs beside the HTTP server (started from Run). It is a no-op when auto-expiry
// is disabled (cfg.PreviewTTL <= 0), and each sweep is idempotent, so a missed or repeated tick is
// harmless.
func (a *App) runPreviewExpiry(ctx context.Context) {
	if a.cfg.PreviewTTL <= 0 {
		a.log.Info("preview auto-expiry is disabled (PLORIGO_PREVIEW_TTL_HOURS=0)")
		return
	}
	a.log.Info("preview auto-expiry enabled", "ttl", a.cfg.PreviewTTL.String(), "sweep", previewExpirySweepInterval.String())
	t := time.NewTicker(previewExpirySweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := a.deployments.Service().ExpirePreviews(ctx, a.cfg.PreviewTTL); err != nil {
				a.log.Warn("preview expiry sweep failed", "error", err)
			} else if n > 0 {
				a.log.Info("preview expiry sweep enqueued teardowns", "count", n)
			}
		}
	}
}
