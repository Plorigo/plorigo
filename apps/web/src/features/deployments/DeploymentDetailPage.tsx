import type { ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Terminal } from "lucide-react";

import { FailureSummary } from "@/components/FailureSummary";
import { Timeline } from "@/components/Timeline";
import { Badge, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import { isTerminalDeploymentStatus, useDeployment, useDeploymentEvents } from "@/lib/queries";
import { statusTone } from "@/lib/status";
import { deploymentRefLabel, deploymentTimeline, shortRepoUrl } from "./timeline";

export function DeploymentDetailPage() {
  const { deploymentId } = useParams({ strict: false }) as { deploymentId?: string };
  const id = deploymentId ?? "";
  const dep = useDeployment(id);
  const live = !dep.data || !isTerminalDeploymentStatus(dep.data.status);
  const events = useDeploymentEvents(id, live);

  if (dep.isLoading && !dep.data) {
    return (
      <div className="space-y-6">
        <BackLink />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!dep.data) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState title="Deployment not found" body="It may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const d = dep.data;
  const isGit = d.sourceKind === "git";
  const steps = deploymentTimeline(events.data ?? [], d.status, d.sourceKind);
  const logs = (events.data ?? []).filter((e) => e.kind === "log");

  return (
    <div className="space-y-6">
      <BackLink />

      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate font-mono text-xl font-semibold tracking-tight text-foreground">{deploymentRefLabel(d)}</h1>
            <StatusDot tone={statusTone(d.status)} label={d.status} />
          </div>
          <p className="mt-1.5 text-sm text-muted-foreground">Deployment {d.id.slice(0, 8)}</p>
        </div>
      </div>

      {d.status === "failed" && (
        <FailureSummary
          headline={d.message || "The deployment failed."}
          suggestion={
            isGit
              ? "The build or container did not succeed. Check that the repo has a Dockerfile at its root and that the app listens on the container port you set, then deploy again — any previous running release is kept."
              : "The container did not reach a healthy state. Check the image reference and that the app listens on the container port you set, then deploy again — any previous running release is kept."
          }
          logs={logs.slice(-6).map((l) => l.message)}
        />
      )}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <Panel>
          <PanelHeader
            title="Timeline"
            description={isGit ? "Clone → build → start → running." : "Pull → start → health check → running."}
          />
          <div className="p-5">
            <Timeline steps={steps} />
          </div>
        </Panel>

        <Panel>
          <PanelHeader title="Details" />
          <div className="space-y-3 p-4 text-sm">
            <Row label="Status" value={<Badge tone={statusTone(d.status)}>{d.status}</Badge>} />
            <Row
              label="Host port"
              value={
                d.hostPort > 0 ? (
                  <span className="font-mono text-foreground">:{d.hostPort}</span>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )
              }
            />
            {isGit ? (
              <>
                <Row label="Source" value={<span className="truncate font-mono text-foreground">{shortRepoUrl(d.cloneUrl)}</span>} />
                <Row
                  label="Branch"
                  value={d.gitRef ? <span className="font-mono text-foreground">{d.gitRef}</span> : <span className="text-muted-foreground">default</span>}
                />
                <Row
                  label="Commit"
                  value={
                    d.commitSha ? (
                      <span className="font-mono text-foreground">{d.commitSha.slice(0, 12)}</span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )
                  }
                />
              </>
            ) : (
              <Row label="Image" value={<span className="truncate font-mono text-foreground">{d.imageRef}</span>} />
            )}
            <Row label="Created" value={<span className="text-muted-foreground">{new Date(d.createdAt).toLocaleString()}</span>} />
            {d.hostPort > 0 && (
              <p className="border-t border-border pt-3 text-xs text-muted-foreground">
                Reachable at your server&apos;s address on port {d.hostPort}.
              </p>
            )}
          </div>
        </Panel>
      </div>

      <Panel>
        <PanelHeader title="Runtime logs" description="Streamed from the deploy agent." />
        {logs.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="No logs yet"
              body={
                live
                  ? isGit
                    ? "Logs appear here as the agent clones, builds, and starts the container."
                    : "Logs appear here as the agent pulls and starts the container."
                  : "This deployment produced no captured logs."
              }
            />
          </div>
        ) : (
          <div className="space-y-2 p-4">
            {logs.map((line) => (
              <div
                key={String(line.seq)}
                className="grid gap-2 rounded-md bg-zinc-950 px-3 py-2 text-xs text-zinc-100 sm:grid-cols-[64px_minmax(0,1fr)]"
              >
                <span className="font-mono text-zinc-400">{new Date(line.createdAt).toLocaleTimeString()}</span>
                <span className="inline-flex min-w-0 items-start gap-1.5 break-words font-mono">
                  <Terminal className="mt-0.5 h-3 w-3 shrink-0 text-emerald-300" aria-hidden="true" />
                  <span className="min-w-0 break-words">{line.message}</span>
                </span>
              </div>
            ))}
          </div>
        )}
      </Panel>
    </div>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right">{value}</span>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/deployments" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to deployments
    </Link>
  );
}
