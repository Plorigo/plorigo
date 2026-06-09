import { useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import {
  ArrowLeft,
  CheckCircle2,
  Clock3,
  ExternalLink,
  GitBranch,
  Gauge,
  Rocket,
} from "lucide-react";
import { toast } from "sonner";

import { ComingSoon } from "@/components/ComingSoon";
import { StatCard } from "@/components/StatCard";
import { Timeline, type TimelineStep } from "@/components/Timeline";
import { Badge, Button, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { useDeploymentsByProject, useEnvironments } from "@/lib/queries";
import { statusTone, type Tone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import { NewDeploymentDialog } from "../deployments/NewDeploymentDialog";
import { AddEnvironmentDialog } from "./NewProjectDialog";
import { useDashboardProjects, frameworkTone } from "./projectData";

// environmentTone tints an environment badge by its type.
function environmentTone(type: string): Tone {
  if (type === "production") return "green";
  if (type === "staging") return "amber";
  if (type === "preview") return "blue";
  return "purple";
}

const releaseSteps: TimelineStep[] = [
  { label: "Build", value: "48s", detail: "Layer cache restored", status: "done" },
  { label: "Health check", value: "Passing", detail: "Ready after retry 2", status: "done" },
  { label: "Caddy route", value: "Switched", detail: "Traffic on new release", status: "done" },
  { label: "Live", value: "Production", detail: "Previous release kept for rollback", status: "active" },
];

const readinessChecks: Array<[string, "done" | "warn"]> = [
  ["Build", "done"],
  ["Start command", "done"],
  ["Environment vars", "done"],
  ["Secrets", "done"],
  ["Database", "done"],
  ["DNS & SSL", "done"],
  ["Recovery & rollback", "warn"],
  ["Observability", "done"],
];

export function ProjectDetailPage() {
  const { projectId } = useParams({ strict: false });
  const demo = useDemoData();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const { query, dashboardProjects } = useDashboardProjects();
  const project = dashboardProjects.find((p) => p.id === projectId);
  const projectDeployments = useDeploymentsByProject(projectId ?? "");
  const latestDeployment = projectDeployments.data?.[0];
  const environments = useEnvironments(projectId ?? "");
  const [deployOpen, setDeployOpen] = useState(false);
  const [addEnvOpen, setAddEnvOpen] = useState(false);

  if (query.isLoading && dashboardProjects.length === 0) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (!project) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState title="Project not found" body="This project may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <BackLink />

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{project.name}</h1>
            <Badge tone={frameworkTone(project.framework)}>{project.framework}</Badge>
            <StatusDot tone={statusTone(project.status)} label={project.status === "attention" ? "Needs attention" : project.status} />
          </div>
          <p className="mt-1.5 text-sm text-muted-foreground">
            {project.repo} · {project.branch} · {project.url}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="secondary" size="sm" onClick={() => toast.info("Opening the live site is coming soon.")}>
            <ExternalLink className="h-4 w-4" aria-hidden="true" />
            Visit
          </Button>
          <Button size="sm" disabled={!workspaceId} onClick={() => setDeployOpen(true)}>
            <Rocket className="h-4 w-4" aria-hidden="true" />
            Deploy
          </Button>
        </div>
      </div>

      <NewDeploymentDialog
        workspaceId={workspaceId}
        open={deployOpen}
        onOpenChange={setDeployOpen}
        defaultProjectId={project.id}
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Health" value={project.status === "attention" ? "Needs review" : "Healthy"} detail="Latest production" icon={CheckCircle2} intent={project.status === "attention" ? "warning" : "success"} accentBar />
        <StatCard label="Readiness" value={`${project.readiness}%`} detail="Production readiness" icon={Gauge} intent={project.readiness >= 90 ? "success" : "warning"} accentBar />
        <StatCard label="Branch" value={project.branch} detail={project.commit} icon={GitBranch} intent="info" accentBar />
        <StatCard label="Updated" value={project.updated.replace("created ", "")} detail="Last activity" icon={Clock3} intent="neutral" accentBar />
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <Panel>
          <PanelHeader title="Latest deployment" description="The most recent release for this project." />
          <div className="p-5">
            {latestDeployment ? (
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm font-medium text-foreground">{latestDeployment.imageRef}</p>
                    <p className="text-xs text-muted-foreground">
                      {latestDeployment.hostPort > 0 ? `host port :${latestDeployment.hostPort}` : "not yet published"}
                    </p>
                  </div>
                  <StatusDot tone={statusTone(latestDeployment.status)} label={latestDeployment.status} />
                </div>
                <Link
                  to="/deployments/$deploymentId"
                  params={{ deploymentId: latestDeployment.id }}
                  className="inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline"
                >
                  View deployment →
                </Link>
              </div>
            ) : demo ? (
              <Timeline steps={releaseSteps} />
            ) : (
              <ComingSoon
                icon={Rocket}
                title="No deploys yet"
                description="Deploy this project to a connected server and its release timeline and logs appear here."
              />
            )}
          </div>
        </Panel>

        <div className="space-y-6">
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
                <p className="text-sm text-muted-foreground">
                  No environments yet — add one to deploy this project.
                </p>
              )}
            </div>
          </Panel>

          {projectId && (
            <AddEnvironmentDialog projectId={projectId} open={addEnvOpen} onOpenChange={setAddEnvOpen} />
          )}

          <Panel>
            <PanelHeader title="Production readiness" description={demo ? "Continuous configuration checks." : "Runs after the first deploy."} />
            {demo ? (
              <div className="grid grid-cols-2 gap-px overflow-hidden p-4">
                {readinessChecks.map(([label, state]) => (
                  <div key={label} className="flex items-start gap-2 px-2 py-2">
                    {state === "warn" ? (
                      <span className="mt-0.5 h-4 w-4 shrink-0 rounded-full bg-warning/20 text-center text-[10px] leading-4 text-warning">!</span>
                    ) : (
                      <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden="true" />
                    )}
                    <span className="min-w-0 truncate text-sm text-foreground">{label}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="p-4">
                <EmptyState title="No readiness report yet" body="Readiness checks run automatically after this project deploys." />
              </div>
            )}
          </Panel>
        </div>
      </div>
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
