import { useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ArrowLeft,
  Boxes,
  CheckCircle2,
  ExternalLink,
  GitBranch,
  GitFork,
  Globe,
  Layers3,
  Lock,
  Rocket,
} from "lucide-react";
import { toast } from "sonner";

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
import { deploymentRefLabel } from "@/features/deployments/timeline";
import { sourceClient } from "@/lib/clients";
import { useDeploymentsByProject, useEnvironments, useProjectSource } from "@/lib/queries";
import { statusTone, type Tone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import { ImportFromGitHubDialog } from "./ImportFromGitHubDialog";
import { AddEnvironmentDialog } from "./EnvironmentDialog";
import { useDashboardProjects, frameworkTone } from "./projectData";

// environmentTone tints an environment badge by its type.
function environmentTone(type: string): Tone {
  if (type === "production") return "green";
  if (type === "staging") return "amber";
  if (type === "preview") return "blue";
  return "purple";
}

// ProjectOverview is the project as a container: who it is, then everything deployed under
// it, plus its environments and source. A project is created empty (name + environments) —
// deployments are added here afterward. Rendered both at /projects/$projectId (embedded
// false, with a back link) and inline on the Overview page when a project filter is active.
export function ProjectOverview({
  projectId,
  embedded = false,
}: {
  projectId: string;
  embedded?: boolean;
}) {
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const { query, dashboardProjects } = useDashboardProjects();
  const project = dashboardProjects.find((p) => p.id === projectId);
  // Everything deployed under this project — gated on a non-empty id, so "" is safe.
  const deployments = useDeploymentsByProject(projectId);
  const environments = useEnvironments(projectId);
  const source = useProjectSource(projectId);
  const queryClient = useQueryClient();
  const [addEnvOpen, setAddEnvOpen] = useState(false);
  const [sourceOpen, setSourceOpen] = useState(false);

  async function disconnectSource() {
    if (!projectId) return;
    try {
      await sourceClient.disconnectRepository({ projectId });
      await queryClient.invalidateQueries({ queryKey: ["projectSource", projectId] });
      await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
      toast.success("Repository disconnected");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not disconnect the repository");
    }
  }

  if (query.isLoading && dashboardProjects.length === 0) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (!project) {
    return (
      <div className="space-y-6">
        {!embedded && <BackLink />}
        <EmptyState title="Project not found" body="This project may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const rows = deployments.data ?? [];
  const latest = rows[0];
  // Use the real deployment URL from the latest running deployment, falling back to the placeholder
  const displayUrl = latest?.routeUrl || project.url;
  const envName = new Map((environments.data ?? []).map((e) => [e.id, `${e.name} · ${e.type}`]));

  return (
    <div className="space-y-6">
      {!embedded && <BackLink />}

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{project.name}</h1>
            <Badge tone={frameworkTone(project.framework)}>{project.framework}</Badge>
            {latest && <StatusDot tone={statusTone(latest.status)} label={latest.status} />}
          </div>
          <p className="mt-1.5 text-sm text-muted-foreground">{displayUrl}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {latest?.routeUrl ? (
            <a
              href={latest.routeUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-8 items-center gap-1.5 rounded-md bg-secondary px-3 text-sm font-medium text-secondary-foreground transition-colors hover:bg-secondary/80"
            >
              <ExternalLink className="h-4 w-4" aria-hidden="true" />
              Visit
            </a>
          ) : (
            <Button variant="secondary" size="sm" onClick={() => toast.info("Deploy to get a live URL.")}>
              <ExternalLink className="h-4 w-4" aria-hidden="true" />
              Visit
            </Button>
          )}
          <Button
            size="sm"
            disabled={!workspaceId}
            onClick={() => navigate({ to: "/deployments/new", search: { project: project.id } })}
          >
            <Rocket className="h-4 w-4" aria-hidden="true" />
            Deploy
          </Button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard label="Deployments" value={String(rows.length)} detail="Under this project" icon={Boxes} intent="info" accentBar />
        <StatCard label="Environments" value={String((environments.data ?? []).length)} detail="Targets" icon={Layers3} intent="violet" accentBar />
        <StatCard
          label="Latest"
          value={latest ? latest.status : "—"}
          detail={latest ? timeAgo(latest.createdAt) : "Nothing deployed yet"}
          icon={CheckCircle2}
          intent={latest ? (latest.status === "running" ? "success" : latest.status === "failed" ? "warning" : "info") : "neutral"}
          accentBar
        />
      </div>

      <Panel>
        <PanelHeader
          title="Deployments"
          description="Everything deployed under this project."
          action={
            <Button
              size="sm"
              variant="secondary"
              disabled={!workspaceId}
              onClick={() => navigate({ to: "/deployments/new", search: { project: project.id } })}
            >
              <Rocket className="h-4 w-4" aria-hidden="true" />
              Deploy
            </Button>
          }
        />
        {deployments.isLoading ? (
          <div className="p-4">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : rows.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="Nothing deployed yet"
              body="This project is ready. Click Deploy to run a container under it — pick an environment and server, give a public image and its port, and watch it go live."
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Deployment</TableHead>
                  <TableHead>Environment</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((d) => (
                  <TableRow
                    key={d.id}
                    className="cursor-pointer"
                    onClick={() => navigate({ to: "/deployments/$deploymentId", params: { deploymentId: d.id } })}
                  >
                    <TableCell>
                      <p className="truncate font-mono text-sm font-medium text-foreground">{deploymentRefLabel(d)}</p>
                      <p className="text-xs text-muted-foreground">{d.id.slice(0, 8)}</p>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{envName.get(d.environmentId) ?? "—"}</TableCell>
                    <TableCell>
                      <StatusDot tone={statusTone(d.status)} label={d.status} />
                    </TableCell>
                    <TableCell className="max-w-[14rem] truncate font-mono text-xs text-muted-foreground">
                      {d.routeUrl ? (
                        <a href={d.routeUrl} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300 hover:underline">
                          {d.routeUrl}
                        </a>
                      ) : d.customDomain ? (
                        <span className="text-blue-400">{d.customDomain}</span>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{timeAgo(d.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel>
          <PanelHeader
            title="Environments"
            description="The deployment targets within this project."
            action={
              <Button size="sm" variant="secondary" onClick={() => setAddEnvOpen(true)}>
                Add environment
              </Button>
            }
          />
          <div className="space-y-3 p-4">
            {(environments.data ?? []).length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {(environments.data ?? []).map((environment) => (
                  <Badge key={environment.id} tone={environmentTone(environment.type)}>
                    {environment.name} · {environment.type}
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No environments yet — add one to deploy this project.</p>
            )}
          </div>
        </Panel>

        <Panel>
          <PanelHeader title="Source" description="The GitHub repository this project deploys from." />
          <div className="p-4">
            {source.isLoading ? (
              <Skeleton className="h-12 w-full" />
            ) : source.data ? (
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <a
                    href={source.data.htmlUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 truncate text-sm font-medium text-foreground hover:underline"
                  >
                    <GitFork className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                    {source.data.fullName}
                    <ExternalLink className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                  </a>
                  <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1">
                      <GitBranch className="h-3 w-3" aria-hidden="true" />
                      {source.data.branch}
                    </span>
                    {source.data.isPrivate && (
                      <span className="inline-flex items-center gap-1">
                        <Lock className="h-3 w-3" aria-hidden="true" />
                        private
                      </span>
                    )}
                    {source.data.access === "public" ? (
                      <span className="inline-flex items-center gap-1">
                        <Globe className="h-3 w-3" aria-hidden="true" />
                        public repo
                      </span>
                    ) : (
                      source.data.githubLogin && <span>connected as {source.data.githubLogin}</span>
                    )}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Button size="sm" variant="secondary" onClick={() => setSourceOpen(true)}>
                    Reconnect
                  </Button>
                  <Button size="sm" variant="ghost" onClick={disconnectSource}>
                    Disconnect
                  </Button>
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-between gap-3">
                <p className="text-sm text-muted-foreground">No repository connected.</p>
                <Button size="sm" variant="secondary" disabled={!workspaceId} onClick={() => setSourceOpen(true)}>
                  <GitFork className="h-4 w-4" aria-hidden="true" />
                  Connect repository
                </Button>
              </div>
            )}
          </div>
        </Panel>
      </div>

      {projectId && (
        <>
          <ImportFromGitHubDialog
            workspaceId={workspaceId}
            open={sourceOpen}
            onOpenChange={setSourceOpen}
            projectId={projectId}
          />
          <AddEnvironmentDialog projectId={projectId} open={addEnvOpen} onOpenChange={setAddEnvOpen} />
        </>
      )}
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

function BackLink() {
  return (
    <Link to="/projects" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to projects
    </Link>
  );
}
