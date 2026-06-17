import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { FailureSummary } from "@/components/FailureSummary";
import { Timeline } from "@/components/Timeline";
import { Badge, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { DeploymentEvent } from "@/gen/controlplane/v1/deployments_pb";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { isTerminalDeploymentStatus, useDeployment, useDeploymentEvents } from "@/lib/queries";
import { statusTone } from "@/lib/status";
import { classifyFailure } from "./failure";
import { deploymentRefLabel, deploymentTimeline, shortRepoUrl } from "./timeline";

export function DeploymentDetailPage() {
  const { deploymentId } = useParams({ strict: false }) as { deploymentId?: string };
  const id = deploymentId ?? "";
  const projectId = useEffectiveProjectId();
  const dep = useDeployment(id);
  const live = !dep.data || !isTerminalDeploymentStatus(dep.data.status);
  const events = useDeploymentEvents(id, live);
  const logsRef = useRef<HTMLDivElement>(null);
  // The log tab is controlled so the failure summary can switch the user straight to the
  // stream that explains the failure; null = follow defaultLogTab.
  const [logTab, setLogTab] = useState<string | null>(null);

  if (dep.isLoading && !dep.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={projectId} />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!dep.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={projectId} />
        <EmptyState title="Deployment not found" body="It may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const d = dep.data;
  const isGit = d.sourceKind === "git";
  const steps = deploymentTimeline(events.data ?? [], d.status, d.sourceKind);
  const allLogs = (events.data ?? []).filter((e) => e.kind === "log");
  // A log line belongs to the build stream (the agent's clone/build/pull/start output) or
  // the runtime stream (the container's stdout/stderr). Legacy events predate the
  // distinction, so they fall back into the build view.
  const buildLogs = allLogs.filter((e) => e.stream === "build" || e.stream === "");
  const runtimeLogs = allLogs.filter((e) => e.stream === "runtime");
  // Classify a failure into a plain-English summary + the log stream that explains it (see
  // ./failure). failedPhase names the timeline step that failed, so the summary ties back to it.
  const failure = d.status === "failed" ? classifyFailure(d.message, d.sourceKind) : null;
  const failedPhase = steps.find((s) => s.status === "failed")?.label;
  // Show the tail of the stream the failure points to, falling back to whichever has output.
  const failureStreamLogs = failure?.stream === "runtime" ? runtimeLogs : buildLogs;
  const failureTail = (failureStreamLogs.length > 0 ? failureStreamLogs : runtimeLogs.length > 0 ? runtimeLogs : buildLogs)
    .slice(-6)
    .map((l) => l.message);
  // Default to runtime logs once the app is up; otherwise watch the build/deploy output.
  const defaultLogTab = d.status === "running" || d.status === "superseded" ? "runtime" : "build";

  // viewFailureLogs jumps to the log stream that explains the failure and scrolls it into view.
  function viewFailureLogs() {
    if (failure) setLogTab(failure.stream);
    logsRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  return (
    <div className="space-y-6">
      <BackLink projectId={projectId} />

      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate font-mono text-xl font-semibold tracking-tight text-foreground">{deploymentRefLabel(d)}</h1>
            <StatusDot tone={statusTone(d.status)} label={d.status} />
          </div>
          <p className="mt-1.5 text-sm text-muted-foreground">Deployment {d.id.slice(0, 8)}</p>
        </div>
      </div>

      {failure && (
        <FailureSummary
          headline={failure.headline}
          explanation={failure.explanation}
          fix={failure.fix}
          failedPhase={failedPhase}
          rawMessage={d.message && d.message !== failure.explanation ? d.message : undefined}
          logStreamLabel={failure.stream}
          onViewLogs={viewFailureLogs}
          logs={failureTail}
        />
      )}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <Panel>
          <PanelHeader
            title="Timeline"
            description={isGit ? "Clone → build → start → health check → route → running." : "Pull → start → health check → route → running."}
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
                Container is published internally on host port {d.hostPort}; Caddy handles the public route.
              </p>
            )}
            {d.routeUrl && (
              <div className="space-y-1">
                <span className="text-muted-foreground">URL</span>
                <a
                  href={d.routeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block break-all font-mono text-sm text-blue-400 hover:text-blue-300 hover:underline"
                >
                  {d.routeUrl}
                </a>
              </div>
            )}
          </div>
        </Panel>
      </div>

      <Panel>
        <PanelHeader title="Logs" description="Build and runtime output, streamed from the deploy agent." />
        <div className="p-4" ref={logsRef}>
          <Tabs value={logTab ?? defaultLogTab} onValueChange={setLogTab}>
            <TabsList>
              <TabsTrigger value="build">Build{buildLogs.length > 0 ? ` (${buildLogs.length})` : ""}</TabsTrigger>
              <TabsTrigger value="runtime">Runtime{runtimeLogs.length > 0 ? ` (${runtimeLogs.length})` : ""}</TabsTrigger>
            </TabsList>
            <TabsContent value="build">
              <LogStream
                logs={buildLogs}
                emptyTitle="No build logs yet"
                emptyBody={
                  isGit
                    ? "Output appears here as the agent clones the repo and builds its Dockerfile."
                    : "Output appears here as the agent pulls the image and starts the container."
                }
              />
            </TabsContent>
            <TabsContent value="runtime">
              <LogStream
                logs={runtimeLogs}
                newestFirst
                emptyTitle="No runtime logs yet"
                emptyBody={
                  live
                    ? "The container's output appears here once it is running."
                    : "This deployment's container produced no captured output."
                }
              />
            </TabsContent>
          </Tabs>
        </div>
      </Panel>
    </div>
  );
}

function LogStream({
  logs,
  newestFirst = false,
  emptyTitle,
  emptyBody,
}: {
  logs: DeploymentEvent[];
  newestFirst?: boolean;
  emptyTitle: string;
  emptyBody: string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const atFollowEdgeRef = useRef(true);
  const visibleLogs = useMemo(() => {
    if (!newestFirst) return logs;

    return [...logs].sort((a, b) => {
      if (a.seq === b.seq) return 0;
      return a.seq > b.seq ? -1 : 1;
    });
  }, [logs, newestFirst]);

  // Follow new lines only while the user is already at the live edge: bottom for
  // chronological build logs, top for newest-first runtime logs.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !atFollowEdgeRef.current) return;

    el.scrollTop = newestFirst ? 0 : el.scrollHeight;
  }, [visibleLogs, newestFirst]);

  if (logs.length === 0) {
    return (
      <div className="mt-3">
        <EmptyState title={emptyTitle} body={emptyBody} />
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      onScroll={(e) => {
        const el = e.currentTarget;
        atFollowEdgeRef.current = newestFirst
          ? el.scrollTop < 24
          : el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
      className="mt-3 max-h-[32rem] overflow-y-auto rounded-lg border border-zinc-800 bg-zinc-950 py-1 font-mono text-xs leading-5 text-zinc-100 shadow-inner"
    >
      {visibleLogs.map((line) => (
        <div
          key={String(line.seq)}
          className="grid gap-0.5 border-b border-white/5 px-3 py-1.5 last:border-b-0 hover:bg-white/[0.04] sm:grid-cols-[72px_minmax(0,1fr)] sm:gap-3"
        >
          <span className="whitespace-nowrap text-zinc-500">{formatLogTime(line.createdAt)}</span>
          <span className="min-w-0 whitespace-pre-wrap break-words text-zinc-100">{line.message}</span>
        </div>
      ))}
    </div>
  );
}

function formatLogTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "--:--:--";

  return [date.getHours(), date.getMinutes(), date.getSeconds()]
    .map((part) => String(part).padStart(2, "0"))
    .join(":");
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right">{value}</span>
    </div>
  );
}

function BackLink({ projectId = "" }: { projectId?: string }) {
  if (projectId) {
    return (
      <Link
        to="/projects/$projectId/deployments"
        params={{ projectId }}
        className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to deployments
      </Link>
    );
  }
  return (
    <Link to="/deployments" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to deployments
    </Link>
  );
}
