import { useNavigate } from "@tanstack/react-router";
import { AlertTriangle, CheckCircle2, Rocket, Terminal } from "lucide-react";

import { FailureSummary } from "@/components/FailureSummary";
import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Badge, Button, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/cn";
import { useDemoData } from "@/lib/demo";
import { errorMessage } from "@/lib/format";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { deploymentRefLabel } from "./timeline";
import { deployments, logLines } from "@/lib/mockDashboard";
import { statusTone } from "@/lib/status";
import { useDeploymentsByProject, useDeploymentsByWorkspace, useProjects } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

export function DeploymentsPage() {
  const demo = useDemoData();
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const projectId = useEffectiveProjectId();
  // Both hooks are called (hooks rule); the project one is gated on a non-empty id, so it
  // makes no request when no project is selected. Read whichever matches the active scope.
  const byWorkspace = useDeploymentsByWorkspace(workspaceId);
  const byProject = useDeploymentsByProject(projectId);
  const deps = projectId ? byProject : byWorkspace;
  const projects = useProjects(workspaceId);

  const error = errorMessage(deps.error);
  const loading = deps.isLoading;
  const rows = deps.data ?? [];
  const projectName = new Map((projects.data ?? []).map((p) => [p.id, p.name]));

  return (
    <div className="space-y-6">
      <PageHeader
        title="Deployments"
        description="A timeline for every release, with build and runtime logs and rollback context."
        actions={
          <Button
            size="sm"
            disabled={!workspaceId}
            onClick={() => navigate({ to: "/deployments/new", search: projectId ? { project: projectId } : {} })}
          >
            <Rocket className="h-4 w-4" aria-hidden="true" />
            New deployment
          </Button>
        }
      />

      {loading && <Skeleton className="h-48 w-full" />}

      {!loading && error && <EmptyState title="Couldn't load deployments" body={error} />}

      {!loading && !error && rows.length > 0 && (
        <Panel>
          <PanelHeader
            title="Deployments"
            description={projectId ? "Recent releases for this project." : "Recent releases across this workspace."}
          />
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Deployment</TableHead>
                  <TableHead>Project</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Host port</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((d) => (
                  <TableRow
                    key={d.id}
                    className="cursor-pointer"
                    onClick={() => {
                      if (projectId) {
                        navigate({
                          to: "/projects/$projectId/deployments/$deploymentId",
                          params: { projectId, deploymentId: d.id },
                        });
                        return;
                      }
                      navigate({ to: "/deployments/$deploymentId", params: { deploymentId: d.id } });
                    }}
                  >
                    <TableCell>
                      <p className="truncate font-mono text-sm font-medium text-foreground">{deploymentRefLabel(d)}</p>
                      <p className="text-xs text-muted-foreground">{d.id.slice(0, 8)}</p>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{projectName.get(d.projectId) ?? "—"}</TableCell>
                    <TableCell>
                      <StatusDot tone={statusTone(d.status)} label={d.status} />
                    </TableCell>
                    <TableCell className="font-mono text-muted-foreground">{d.hostPort > 0 ? `:${d.hostPort}` : "—"}</TableCell>
                    <TableCell className="text-muted-foreground">{timeAgo(d.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </Panel>
      )}

      {!loading && !error && rows.length === 0 && (demo ? <DeploymentsDemo /> : (
        <EmptyState
          title="No deployments yet"
          body="Deploy a container to a connected server. Pick a project, environment, and server, give a public image and its port, and watch it go live."
        />
      ))}
    </div>
  );
}

// timeAgo renders a short relative time for an RFC 3339 timestamp.
function timeAgo(iso: string): string {
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}

function DeploymentsDemo() {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-3">
        <StatCard label="Latest production" value="Ready" detail="main · 8f41c2a" icon={CheckCircle2} intent="success" accentBar />
        <StatCard label="Preview build" value="Building" detail="release/checkout" icon={Rocket} intent="info" accentBar />
        <StatCard label="Needs attention" value="1 failed" detail="staging worker" icon={AlertTriangle} intent="warning" accentBar />
      </div>

      <FailureSummary
        headline="Queue Worker failed to start on staging — the health check never passed."
        suggestion="The container exited before binding its port. This usually means a missing environment variable or a crash on boot. Check REDIS_URL, then redeploy — the previous release is still serving."
        logs={[
          "14:21:03  worker   Error: connect ECONNREFUSED 127.0.0.1:6379",
          "14:21:03  worker   at TCPConnectWrap.afterConnect [as oncomplete]",
          "14:21:04  health   readiness probe failed: container not listening on :8080",
          "14:21:34  deploy   health check timed out after 30s — rolling back",
        ]}
      />

      <Panel>
        <PanelHeader title="Deployment timeline" description="Recent releases across environments." />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Deployment</TableHead>
                <TableHead>Environment</TableHead>
                <TableHead>Branch</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Duration</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deployments.map((deployment) => (
                <TableRow key={deployment.id}>
                  <TableCell>
                    <p className="font-medium text-foreground">{deployment.project}</p>
                    <p className="text-xs text-muted-foreground">
                      {deployment.commit} by {deployment.author} · {deployment.age}
                    </p>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{deployment.environment}</TableCell>
                  <TableCell className="text-muted-foreground">{deployment.branch}</TableCell>
                  <TableCell>
                    <StatusDot tone={statusTone(deployment.status)} label={deployment.status} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">{deployment.duration}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Panel>

      <Panel>
        <PanelHeader title="Runtime logs" description="Streamed from the deploy agent." />
        <div className="space-y-2 p-4">
          {logLines.map((line) => (
            <div
              key={`${line.time}-${line.service}`}
              className="grid gap-2 rounded-md bg-zinc-950 px-3 py-2 text-xs text-zinc-100 sm:grid-cols-[64px_92px_minmax(0,1fr)]"
            >
              <span className="font-mono text-zinc-400">{line.time}</span>
              <span
                className={cn(
                  "inline-flex items-center gap-1 font-mono",
                  line.level === "error"
                    ? "text-red-300"
                    : line.level === "warn"
                      ? "text-amber-300"
                      : "text-emerald-300",
                )}
              >
                <Terminal className="h-3 w-3" aria-hidden="true" />
                {line.service}
              </span>
              <span className="min-w-0 truncate font-mono">{line.message}</span>
            </div>
          ))}
        </div>
      </Panel>

      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Badge tone="purple">Demo</Badge>
        Showing prototype deployment data. Turn off demo mode to see your real releases.
      </p>
    </div>
  );
}
