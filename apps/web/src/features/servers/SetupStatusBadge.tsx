import type { ReactNode } from "react";

import { Badge } from "@/components/ui";
import { useSetupRun } from "@/lib/queries";
import { statusTone } from "@/lib/status";

// SetupStatusBadge reflects an in-session managed setup run on a server card: "Setting up…"
// while it runs, "Setup failed" if it fails. Once it succeeds it renders the fallback (the
// normal agent/readiness badge), since the agent itself now reports the server's state.
export function SetupStatusBadge({ runId, fallback }: { runId: string; fallback: ReactNode }) {
  const run = useSetupRun(runId);
  const status = run.data?.status;
  if (status === "succeeded") return <>{fallback}</>;
  if (status === "failed") {
    return (
      <Badge tone={statusTone("setup failed")} className="shrink-0">
        Setup failed
      </Badge>
    );
  }
  return (
    <Badge tone={statusTone("setting up")} className="shrink-0">
      Setting up…
    </Badge>
  );
}
