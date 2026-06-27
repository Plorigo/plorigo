import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ArrowLeft,
  Boxes,
  CheckCircle2,
  Container,
  ExternalLink,
  GitFork,
  Layers3,
  Lock,
  Plus,
} from "lucide-react";

import { StatCard } from "@/components/StatCard";
import { Badge, Button, EmptyState, Panel, PanelHeader, Skeleton } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { isPublic, sourceLabel } from "@/features/services/serviceData";
import { useEnvironments, useServicesByProject } from "@/lib/queries";
import { type Tone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import { AddEnvironmentDialog } from "./EnvironmentDialog";
import { useDashboardProjects, frameworkTone } from "./projectData";

// environmentTone tints an environment badge by its type.
function environmentTone(type: string): Tone {
  if (type === "production") return "green";
  if (type === "staging") return "amber";
  if (type === "preview") return "blue";
  return "purple";
}

// ProjectOverview is the project as a container: who it is, then the services running under
// it, plus its environments. A project is created empty (name + environments) — services
// are added here afterward. Rendered both at /projects/$projectId (embedded false, with a
// back link) and inline on the Overview page when a project filter is active.
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
  // Everything running under this project — gated on a non-empty id, so "" is safe.
  const services = useServicesByProject(projectId);
  const environments = useEnvironments(projectId);
  const [addEnvOpen, setAddEnvOpen] = useState(false);

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

  const rows = services.data ?? [];
  const envName = new Map((environments.data ?? []).map((e) => [e.id, `${e.name} · ${e.type}`]));

  return (
    <div className="space-y-6">
      {!embedded && <BackLink />}

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{project.name}</h1>
            <Badge tone={frameworkTone(project.framework)}>{project.framework}</Badge>
          </div>
          <p className="mt-1.5 text-sm text-muted-foreground">{project.url}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button
            size="sm"
            disabled={!workspaceId}
            onClick={() => navigate({ to: "/deployments/new", search: { project: project.id } })}
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add service
          </Button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard label="Services" value={String(rows.length)} detail="Under this project" icon={Boxes} intent="info" accentBar />
        <StatCard label="Environments" value={String((environments.data ?? []).length)} detail="Targets" icon={Layers3} intent="violet" accentBar />
        <StatCard
          label="Public"
          value={String(rows.filter((s) => isPublic(s)).length)}
          detail="Internet-reachable"
          icon={CheckCircle2}
          intent={rows.length > 0 ? "success" : "neutral"}
          accentBar
        />
      </div>

      <Panel>
        <PanelHeader
          title="Services"
          description="The deployable components running under this project."
          action={
            <Button
              size="sm"
              variant="secondary"
              disabled={!workspaceId}
              onClick={() => navigate({ to: "/deployments/new", search: { project: project.id } })}
            >
              <Plus className="h-4 w-4" aria-hidden="true" />
              Add service
            </Button>
          }
        />
        {services.isLoading ? (
          <div className="p-4">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : rows.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="No services yet"
              body="This project is ready. Click Add service to run a component under it — a public image, a public Git repo, or a connected repository — pick an environment and server, and watch it go live."
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Service</TableHead>
                  <TableHead>Environment</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Port</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((s) => (
                  <TableRow
                    key={s.id}
                    className="cursor-pointer"
                    onClick={() =>
                      navigate({
                        to: "/projects/$projectId/services/$serviceId",
                        params: { projectId: project.id, serviceId: s.id },
                      })
                    }
                  >
                    <TableCell>
                      <p className="truncate text-sm font-medium text-foreground">{s.name}</p>
                      <p className="text-xs text-muted-foreground">{s.id.slice(0, 8)}</p>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{envName.get(s.environmentId) ?? "—"}</TableCell>
                    <TableCell>
                      <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                        {s.sourceKind === "git" ? (
                          <GitFork className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                        ) : (
                          <Container className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                        )}
                        <span className="truncate font-mono text-xs">{sourceLabel(s)}</span>
                      </span>
                    </TableCell>
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      {isPublic(s) ? (
                        s.routeUrl ? (
                          <a
                            href={s.routeUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 truncate text-sm text-blue-400 hover:text-blue-300 hover:underline"
                          >
                            <ExternalLink className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                            Visit
                          </a>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )
                      ) : (
                        <Badge tone="purple">
                          <Lock className="mr-1 h-3 w-3" aria-hidden="true" />
                          Internal
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-muted-foreground">{s.containerPort > 0 ? `:${s.containerPort}` : "—"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <Panel>
        <PanelHeader
          title="Environments"
          description="The deployment targets within this project. Open one to manage its secrets."
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
                <button
                  key={environment.id}
                  type="button"
                  title={`Open ${environment.name} to manage its secrets`}
                  onClick={() =>
                    navigate({
                      to: "/projects/$projectId/environments/$environmentId",
                      params: { projectId: project.id, environmentId: environment.id },
                    })
                  }
                  className="rounded-full focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <Badge tone={environmentTone(environment.type)} className="cursor-pointer hover:opacity-80">
                    {environment.name} · {environment.type}
                  </Badge>
                </button>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">No environments yet — add one to deploy services under this project.</p>
          )}
        </div>
      </Panel>

      {projectId && <AddEnvironmentDialog projectId={projectId} open={addEnvOpen} onOpenChange={setAddEnvOpen} />}
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/projects" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to projects
    </Link>
  );
}
