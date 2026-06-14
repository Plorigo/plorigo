import type { TimelineStatus, TimelineStep } from "@/components/Timeline";
import type { DeploymentEvent } from "@/gen/controlplane/v1/deployments_pb";

// The canonical deploy stages, in order. The agent reports a status as it ENTERS each
// stage, so the furthest-reached status is the stage currently in flight (or, on failure,
// the stage that failed). The stages differ by source: an image deployment pulls a
// pre-built image; a git deployment clones the repo and builds its Dockerfile instead.
const IMAGE_STEPS = ["Queued", "Pull image", "Start container", "Route traffic", "Running"] as const;
const GIT_STEPS = ["Queued", "Clone", "Build", "Start container", "Route traffic", "Running"] as const;

const IMAGE_RANK: Record<string, number> = {
  queued: 0,
  assigned: 0,
  pulling: 1,
  starting: 2,
  routing: 3,
  running: 4,
  superseded: 4,
};
const GIT_RANK: Record<string, number> = {
  queued: 0,
  assigned: 0,
  cloning: 1,
  building: 2,
  starting: 3,
  routing: 4,
  running: 5,
  superseded: 5,
};

// deploymentRefLabel is the compact label for a deployment: the image ref for an image
// deployment, or "owner/repo@commit" for a git one (its image ref is empty until built).
export function deploymentRefLabel(d: { sourceKind: string; imageRef: string; cloneUrl: string; commitSha: string }): string {
  if (d.sourceKind === "git") {
    const repo = shortRepoUrl(d.cloneUrl);
    return d.commitSha ? `${repo}@${d.commitSha.slice(0, 7)}` : repo || "git deployment";
  }
  return d.imageRef || "deployment";
}

// shortRepoUrl strips protocol/host noise from a clone URL for a compact "owner/repo" label.
export function shortRepoUrl(url: string): string {
  return url
    .trim()
    .replace(/^https?:\/\//, "")
    .replace(/^www\./, "")
    .replace(/^github\.com\//, "")
    .replace(/\.git$/, "")
    .replace(/\/+$/, "");
}

// deploymentTimeline derives the stage timeline from a deployment's status events and its
// current status, choosing the image or git stage set by sourceKind. Steps before the
// reached stage are done; the reached stage is active (or done when running); a failed
// deployment marks its reached stage failed.
export function deploymentTimeline(events: DeploymentEvent[], status: string, sourceKind = ""): TimelineStep[] {
  const isGit = sourceKind === "git";
  const steps = isGit ? GIT_STEPS : IMAGE_STEPS;
  const rank = isGit ? GIT_RANK : IMAGE_RANK;
  const last = steps.length - 1;
  let reached = 0;
  for (const e of events) {
    if (e.kind !== "status") continue;
    const r = rank[e.status];
    if (r !== undefined && r > reached) reached = r;
  }
  const failed = status === "failed";
  return steps.map((label, i): TimelineStep => {
    let state: TimelineStatus;
    if (failed) state = i < reached ? "done" : i === reached ? "failed" : "pending";
    else if (i < reached) state = "done";
    else if (i === reached) state = reached === last ? "done" : "active";
    else state = "pending";
    return { label, status: state };
  });
}
