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
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { StatCard } from "@/components/StatCard";
import { Timeline, type TimelineStep } from "@/components/Timeline";
import { Badge, Button, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { statusTone } from "@/lib/status";
import { useDashboardProjects, frameworkTone } from "./projectData";

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
  const { query, dashboardProjects } = useDashboardProjects();
  const project = dashboardProjects.find((p) => p.id === projectId);

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
          <ConfirmDialog
            trigger={
              <Button size="sm">
                <Rocket className="h-4 w-4" aria-hidden="true" />
                Deploy
              </Button>
            }
            title={`Deploy ${project.name} to production?`}
            description="This builds the latest commit and switches production traffic to the new release once health checks pass."
            recovery="The current release is kept as a rollback target — you can revert with one click if anything looks wrong."
            confirmLabel="Deploy to production"
            onConfirm={() => toast.success("Deploy queued. The previous release is kept for rollback.")}
          />
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Health" value={project.status === "attention" ? "Needs review" : "Healthy"} detail="Latest production" icon={CheckCircle2} intent={project.status === "attention" ? "warning" : "success"} accentBar />
        <StatCard label="Readiness" value={`${project.readiness}%`} detail="Production readiness" icon={Gauge} intent={project.readiness >= 90 ? "success" : "warning"} accentBar />
        <StatCard label="Branch" value={project.branch} detail={project.commit} icon={GitBranch} intent="info" accentBar />
        <StatCard label="Updated" value={project.updated.replace("created ", "")} detail="Last activity" icon={Clock3} intent="neutral" accentBar />
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <Panel>
          <PanelHeader title="Latest production release" description="Build, health, and route-switch stages." />
          <div className="p-5">
            {demo ? (
              <Timeline steps={releaseSteps} />
            ) : (
              <ComingSoon
                icon={Rocket}
                title="No deploys yet"
                description="Once this project deploys, its build → health → route-switch timeline and logs appear here, with one-click rollback."
              />
            )}
          </div>
        </Panel>

        <div className="space-y-6">
          <Panel>
            <PanelHeader title="Environments & services" description="Where this project runs." />
            <div className="space-y-3 p-4">
              <div className="flex flex-wrap gap-2">
                {project.environments.map((environment) => (
                  <Badge key={environment.name} tone={environment.tone}>
                    {environment.name}
                  </Badge>
                ))}
              </div>
              <div className="flex flex-wrap gap-2">
                {project.services.map((service) => (
                  <span key={service} className="rounded-md border border-border bg-background px-2 py-1 text-xs text-muted-foreground">
                    {service}
                  </span>
                ))}
              </div>
            </div>
          </Panel>

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
