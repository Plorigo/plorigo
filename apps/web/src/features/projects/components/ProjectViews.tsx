import { Link } from "@tanstack/react-router";
import {
  BarChart3,
  Clock3,
  Code2,
  Database,
  GitBranch,
  Globe2,
  Layers3,
  LockKeyhole,
  RefreshCcw,
  Users,
  type LucideIcon,
} from "lucide-react";

import { Badge, StatusDot } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { DashboardProject } from "@/lib/mockDashboard";
import { intentDot, intentSoft, statusTone, toneToIntent, type Tone } from "@/lib/status";
import { frameworkTone } from "../projectData";

const kindMap: Record<DashboardProject["kind"], { icon: LucideIcon; intent: Tone }> = {
  web: { icon: Globe2, intent: "blue" },
  api: { icon: Code2, intent: "blue" },
  worker: { icon: Layers3, intent: "purple" },
  service: { icon: LockKeyhole, intent: "purple" },
  analytics: { icon: BarChart3, intent: "purple" },
};

function ProjectKindIcon({ project }: { project: DashboardProject }) {
  const { icon: Icon, intent } = kindMap[project.kind];
  return (
    <span
      className={cn(
        "grid h-10 w-10 shrink-0 place-items-center rounded-lg border",
        intentSoft[toneToIntent[intent]],
      )}
    >
      <Icon className="h-5 w-5" aria-hidden="true" />
    </span>
  );
}

function ReadinessRing({ value }: { value: number }) {
  const color = value >= 90 ? "var(--success)" : value >= 80 ? "var(--warning)" : "var(--destructive)";
  return (
    <span
      className="grid h-5 w-5 place-items-center rounded-full"
      style={{ background: `conic-gradient(${color} ${value * 3.6}deg, var(--muted) 0deg)` }}
      aria-hidden="true"
    >
      <span className="h-3.5 w-3.5 rounded-full bg-card" />
    </span>
  );
}

function ProjectSparkline({ values, tone }: { values: number[]; tone: Tone }) {
  return (
    <div className="flex h-7 shrink-0 items-end gap-0.5" aria-hidden="true">
      {values.map((value, index) => (
        <span
          key={`${value}-${index}`}
          className={cn("w-1 rounded-t", intentDot[toneToIntent[tone]])}
          style={{ height: `${Math.max(6, value)}%` }}
        />
      ))}
    </div>
  );
}

function CompactMeta({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="truncate">{label}</span>
    </div>
  );
}

function ServiceMeta({ service }: { service: string }) {
  const Icon = service === "DB" || service === "Redis" ? Database : service === "Worker" ? RefreshCcw : Globe2;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="truncate">{service}</span>
    </span>
  );
}

export function ProjectCard({ project }: { project: DashboardProject }) {
  const tone = statusTone(project.status);

  return (
    <Link
      to="/projects/$projectId"
      params={{ projectId: project.id }}
      className="group block min-w-0 rounded-xl border border-border bg-card p-4 shadow-card outline-none transition hover:-translate-y-0.5 hover:shadow-card-hover focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <ProjectKindIcon project={project} />
          <div className="min-w-0">
            <h2 className="truncate text-base font-semibold text-foreground">{project.name}</h2>
            <p className="mt-1 truncate text-xs text-muted-foreground">{project.repo}</p>
          </div>
        </div>
        <Badge tone={frameworkTone(project.framework)}>{project.framework}</Badge>
      </div>

      <div className="mt-4 flex min-w-0 flex-wrap gap-2">
        {project.environments.map((environment) => (
          <Badge key={`${project.id}-${environment.name}`} tone={environment.tone}>
            {environment.name}
          </Badge>
        ))}
      </div>

      <div className="mt-4 flex items-center justify-between gap-3 border-b border-border pb-3">
        <StatusDot tone={tone} label={project.status === "attention" ? "Needs attention" : project.status} />
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>Readiness</span>
          <span className="font-semibold text-foreground">{project.readiness}%</span>
          <ReadinessRing value={project.readiness} />
        </div>
      </div>

      <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-muted-foreground">
        <CompactMeta icon={GitBranch} label={project.branch} />
        <CompactMeta icon={Code2} label={project.commit} />
        <CompactMeta icon={Clock3} label={project.updated} />
      </div>

      <div className="mt-3 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
        {project.services.map((service) => (
          <ServiceMeta key={`${project.id}-${service}`} service={service} />
        ))}
        <span className="ml-auto inline-flex items-center gap-1">
          <Users className="h-3.5 w-3.5" aria-hidden="true" />
          {project.collaborators}
        </span>
      </div>

      <div className="mt-4 flex min-w-0 items-end justify-between gap-3 border-t border-border pt-3">
        <span className="min-w-0 truncate text-sm text-muted-foreground group-hover:text-foreground">
          {project.url}
        </span>
        <ProjectSparkline values={project.sparkline} tone={tone} />
      </div>
    </Link>
  );
}

export function ProjectListRow({ project }: { project: DashboardProject }) {
  return (
    <Link
      to="/projects/$projectId"
      params={{ projectId: project.id }}
      className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-card px-4 py-3 text-left shadow-card outline-none transition hover:border-ring/40 focus-visible:ring-2 focus-visible:ring-ring"
    >
      <ProjectKindIcon project={project} />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <p className="truncate text-sm font-semibold text-foreground">{project.name}</p>
          <Badge tone={frameworkTone(project.framework)}>{project.framework}</Badge>
        </div>
        <p className="mt-1 truncate text-xs text-muted-foreground">
          {project.repo} · {project.branch} · {project.commit}
        </p>
      </div>
      <StatusDot tone={statusTone(project.status)} label={`${project.readiness}%`} />
      <span className="hidden text-xs text-muted-foreground sm:block">{project.updated}</span>
    </Link>
  );
}
