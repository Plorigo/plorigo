import type { TimelineStatus, TimelineStep } from "@/components/Timeline";
import type { DeploymentEvent } from "@/gen/controlplane/v1/deployments_pb";

// The canonical deploy stages, in order. The agent reports a status as it ENTERS each
// stage, so the furthest-reached status is the stage currently in flight (or, on
// failure, the stage that failed).
const STEPS = ["Queued", "Pull image", "Start container", "Running"] as const;

const RANK: Record<string, number> = {
  queued: 0,
  assigned: 0,
  pulling: 1,
  starting: 2,
  running: 3,
  superseded: 3,
};

// deploymentTimeline derives the four-stage timeline from a deployment's status events
// and its current status. Steps before the reached stage are done; the reached stage is
// active (or done when running); a failed deployment marks its reached stage failed.
export function deploymentTimeline(events: DeploymentEvent[], status: string): TimelineStep[] {
  let reached = 0;
  for (const e of events) {
    if (e.kind !== "status") continue;
    const r = RANK[e.status];
    if (r !== undefined && r > reached) reached = r;
  }
  const failed = status === "failed";
  return STEPS.map((label, i): TimelineStep => {
    let state: TimelineStatus;
    if (failed) state = i < reached ? "done" : i === reached ? "failed" : "pending";
    else if (i < reached) state = "done";
    else if (i === reached) state = reached === 3 ? "done" : "active";
    else state = "pending";
    return { label, status: state };
  });
}
