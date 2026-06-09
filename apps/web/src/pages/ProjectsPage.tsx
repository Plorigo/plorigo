import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  Box,
  CheckCircle2,
  ChevronDown,
  Clock3,
  Command,
  Cpu,
  ExternalLink,
  GitBranch,
  Globe2,
  Gauge,
  HardDrive,
  Home,
  KeyRound,
  Layers3,
  LockKeyhole,
  LogOut,
  Plus,
  Rocket,
  Search,
  Server,
  Shield,
  ShieldCheck,
  Sun,
  Terminal,
  Users,
  type LucideIcon,
} from "lucide-react";

import {
  authClient,
  environmentClient,
  envVarClient,
  projectClient,
  secretClient,
  serverClient,
  workspaceClient,
} from "../lib/clients";
import {
  activity,
  apiSurface,
  backups,
  configItems,
  deployments,
  domains,
  logLines,
  prototypeProjects,
  serverHealth,
  type DashboardProject,
} from "../lib/mockDashboard";
import { useCurrentUser } from "../lib/auth";
import { plorigoIcon, plorigoLogoBlack } from "../lib/brand";
import { cn } from "../lib/cn";
import { useWorkspaceStore } from "../store";
import {
  Badge,
  Button,
  DataBadge,
  EmptyState,
  MiniBars,
  Panel,
  PanelHeader,
  Select,
  Skeleton,
  StatusDot,
  type DataStatus,
} from "../components/ui";

type Section =
  | "overview"
  | "projects"
  | "deployments"
  | "servers"
  | "resources"
  | "backups"
  | "security"
  | "team"
  | "activity";

const sections: Array<{ id: Section; label: string; icon: LucideIcon }> = [
  { id: "overview", label: "Overview", icon: Home },
  { id: "projects", label: "Projects", icon: Box },
  { id: "deployments", label: "Deployments", icon: Rocket },
  { id: "servers", label: "Servers", icon: Server },
  { id: "resources", label: "Resources", icon: Layers3 },
  { id: "backups", label: "Backups", icon: HardDrive },
  { id: "security", label: "Security", icon: Shield },
  { id: "team", label: "Team", icon: Users },
  { id: "activity", label: "Activity", icon: Clock3 },
];

const sectionCopy: Record<Section, { title: string; eyebrow: string }> = {
  overview: { title: "Workspace overview", eyebrow: "Control plane" },
  projects: { title: "Projects", eyebrow: "Workspace resources" },
  deployments: { title: "Deployments", eyebrow: "Release activity" },
  servers: { title: "Servers", eyebrow: "Owned infrastructure" },
  resources: { title: "Resources", eyebrow: "Env vars and services" },
  backups: { title: "Backups", eyebrow: "Recovery paths" },
  security: { title: "Security", eyebrow: "Production guardrails" },
  team: { title: "Team and access", eyebrow: "Membership" },
  activity: { title: "Activity", eyebrow: "Audit-ready timeline" },
};

const mobileSections = sections.slice(0, 5);

function formatDate(value?: string): string {
  if (!value) {
    return "recently";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "recently";
  }
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(date);
}

function errorMessage(error: unknown): string {
  if (!error) {
    return "";
  }
  return error instanceof Error ? error.message : "Could not load this data";
}

function statusTone(status: string): "green" | "amber" | "red" | "blue" | "purple" | "neutral" {
  if (["ready", "healthy", "online", "passing"].includes(status)) {
    return "green";
  }
  if (["building", "queued", "protected"].includes(status)) {
    return "blue";
  }
  if (["degraded", "needs restore test"].includes(status)) {
    return "amber";
  }
  if (["failed", "offline"].includes(status)) {
    return "red";
  }
  return "neutral";
}

function percentFromLabel(value: string): number {
  const directPercent = value.match(/(\d+)%/);
  if (directPercent) {
    return Number(directPercent[1]);
  }
  const fraction = value.match(/([\d.]+)\s*\/\s*([\d.]+)/);
  if (fraction) {
    return Math.min(100, Math.round((Number(fraction[1]) / Number(fraction[2])) * 100));
  }
  return 0;
}

export function ProjectsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: user } = useCurrentUser();
  const [section, setSection] = useState<Section>("overview");
  const [activeProjectId, setActiveProjectId] = useState("");
  const [activeEnvironmentId, setActiveEnvironmentId] = useState("");
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);

  const workspaces = useQuery({
    queryKey: ["workspaces"],
    queryFn: async () => (await workspaceClient.listMyWorkspaces({})).workspaces,
  });

  useEffect(() => {
    if (workspaces.data && workspaces.data.length > 0) {
      if (!workspaces.data.some((w) => w.id === workspaceId)) {
        setWorkspaceId(workspaces.data[0].id);
      }
    }
  }, [workspaces.data, workspaceId, setWorkspaceId]);

  const projects = useQuery({
    queryKey: ["projects", workspaceId],
    queryFn: async () => (await projectClient.listProjectsByWorkspace({ workspaceId })).projects,
    enabled: workspaceId.length > 0,
  });

  const servers = useQuery({
    queryKey: ["servers", workspaceId],
    queryFn: async () => (await serverClient.listServersByWorkspace({ workspaceId })).servers,
    enabled: workspaceId.length > 0,
  });

  const members = useQuery({
    queryKey: ["members", workspaceId],
    queryFn: async () => (await workspaceClient.listMembers({ workspaceId })).members,
    enabled: workspaceId.length > 0,
  });

  const apiTokens = useQuery({
    queryKey: ["apiTokens"],
    queryFn: async () => (await authClient.listAPITokens({})).apiTokens,
  });

  const dashboardProjects = useMemo<DashboardProject[]>(() => {
    if (!projects.data || projects.data.length === 0) {
      return prototypeProjects;
    }
    return projects.data.map((project) => ({
      id: project.id,
      name: project.name,
      slug: project.slug,
      repo: "Backend project record",
      framework: "ConnectRPC",
      url: `${project.slug}.plorigo.local`,
      branch: "main",
      status: "healthy",
      source: "live",
      updated: `created ${formatDate(project.createdAt)}`,
    }));
  }, [projects.data]);

  useEffect(() => {
    if (dashboardProjects.length === 0) {
      setActiveProjectId("");
      return;
    }
    if (!dashboardProjects.some((project) => project.id === activeProjectId)) {
      setActiveProjectId(dashboardProjects[0].id);
    }
  }, [dashboardProjects, activeProjectId]);

  const selectedProject =
    dashboardProjects.find((project) => project.id === activeProjectId) ?? dashboardProjects[0];

  const selectedLiveProject = projects.data?.find((project) => project.id === selectedProject?.id);
  const liveProjectId = selectedLiveProject?.id ?? "";

  const environments = useQuery({
    queryKey: ["environments", liveProjectId],
    queryFn: async () => (await environmentClient.listEnvironmentsByProject({ projectId: liveProjectId })).environments,
    enabled: liveProjectId.length > 0,
  });

  useEffect(() => {
    if (environments.data && environments.data.length > 0) {
      if (!environments.data.some((environment) => environment.id === activeEnvironmentId)) {
        setActiveEnvironmentId(environments.data[0].id);
      }
      return;
    }
    setActiveEnvironmentId("");
  }, [environments.data, activeEnvironmentId]);

  const envVars = useQuery({
    queryKey: ["envVars", activeEnvironmentId],
    queryFn: async () => (await envVarClient.listEnvVars({ environmentId: activeEnvironmentId })).envVars,
    enabled: activeEnvironmentId.length > 0,
  });

  const secrets = useQuery({
    queryKey: ["secrets", activeEnvironmentId],
    queryFn: async () => (await secretClient.listSecrets({ environmentId: activeEnvironmentId })).secrets,
    enabled: activeEnvironmentId.length > 0,
  });

  async function logout() {
    await authClient.logout({});
    setWorkspaceId("");
    await queryClient.invalidateQueries();
    await navigate({ to: "/login" });
  }

  const currentWorkspace = workspaces.data?.find((workspace) => workspace.id === workspaceId);
  const userInitial = user?.email?.slice(0, 1).toUpperCase() ?? "P";
  const selectedEnvironment =
    environments.data?.find((environment) => environment.id === activeEnvironmentId) ??
    environments.data?.[0];

  return (
    <div className="min-h-screen bg-zinc-50 text-zinc-950">
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-64 flex-col border-r border-zinc-200 bg-white lg:flex">
        <div className="flex h-14 items-center border-b border-zinc-100 px-4">
          <img src={plorigoLogoBlack} alt="Plorigo" className="h-7 w-auto" />
        </div>

        <div className="border-b border-zinc-100 px-3 py-3">
          <label className="mb-1.5 block text-xs font-medium text-zinc-500">Workspace</label>
          <Select
            value={workspaceId}
            onChange={(event) => setWorkspaceId(event.target.value)}
            disabled={workspaces.isLoading || !workspaces.data?.length}
          >
            {workspaces.data?.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name}
              </option>
            ))}
          </Select>
        </div>

        <div className="border-b border-zinc-100 px-3 py-3">
          <div className="rounded-xl border border-zinc-200 bg-zinc-50 p-3">
            <div className="flex items-center justify-between gap-2">
              <p className="truncate text-xs font-medium text-zinc-500">Workspace status</p>
              <DataBadge status="live" />
            </div>
            <p className="mt-2 truncate text-sm font-semibold text-zinc-950">All systems operational</p>
            <div className="mt-2 flex items-center justify-between gap-2">
              <StatusDot tone="green" label="healthy" />
              <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-zinc-400" aria-hidden="true" />
            </div>
          </div>
        </div>

        <nav className="flex-1 space-y-1 px-3 py-3">
          <p className="px-2 pb-1 text-[11px] font-medium uppercase tracking-[0.14em] text-zinc-400">
            Dashboard
          </p>
          {sections.map((item) => {
            const Icon = item.icon;
            const active = section === item.id;
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => setSection(item.id)}
                className={cn(
                  "flex h-9 w-full items-center gap-2 rounded-md px-2.5 text-sm font-medium transition",
                  active
                    ? "bg-zinc-950 text-white shadow-sm"
                    : "text-zinc-600 hover:bg-zinc-100 hover:text-zinc-950",
                )}
              >
                <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
                <span className="truncate">{item.label}</span>
              </button>
            );
          })}
        </nav>

        <div className="border-t border-zinc-100 p-3">
          <div className="mb-3 rounded-md border border-zinc-200 bg-zinc-50 p-3">
            <div className="flex items-center justify-between gap-2">
              <p className="truncate text-xs font-medium text-zinc-900">API surface</p>
              <DataBadge status="live" />
            </div>
            <div className="mt-2 space-y-1.5">
              {apiSurface.slice(0, 3).map((item) => (
                <div key={item.name} className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] text-zinc-500">{item.name}</span>
                  <DataBadge status={item.status} />
                </div>
              ))}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-zinc-950 text-xs font-semibold text-white">
              {userInitial}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-medium text-zinc-900">{user?.email}</p>
              <p className="truncate text-[11px] text-zinc-500">{currentWorkspace?.name ?? "Workspace"}</p>
            </div>
            <Button variant="ghost" size="icon" onClick={logout} aria-label="Log out">
              <LogOut className="h-4 w-4" aria-hidden="true" />
            </Button>
          </div>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-10 border-b border-zinc-200 bg-white/95 backdrop-blur">
          <div className="flex min-h-14 items-center gap-3 px-4 sm:px-6 lg:px-8">
            <img src={plorigoIcon} alt="" className="h-8 w-8 rounded-md lg:hidden" />
            <div className="min-w-0 flex-1">
              <p className="text-xs font-medium text-zinc-500">{sectionCopy[section].eyebrow}</p>
              <h1 className="truncate text-lg font-semibold text-zinc-950">{sectionCopy[section].title}</h1>
            </div>

            <div className="hidden w-full max-w-sm items-center gap-2 rounded-md border border-zinc-200 bg-zinc-50 px-2.5 py-2 text-sm text-zinc-500 md:flex">
              <Search className="h-4 w-4 shrink-0" aria-hidden="true" />
              <span className="truncate">Search projects, servers, deployments</span>
              <span className="ml-auto inline-flex items-center gap-1 rounded border border-zinc-200 bg-white px-1.5 py-0.5 text-[11px] text-zinc-500">
                <Command className="h-3 w-3" aria-hidden="true" />K
              </span>
            </div>

            <div className="hidden min-w-[180px] sm:block">
              <Select
                value={selectedProject?.id ?? ""}
                onChange={(event) => setActiveProjectId(event.target.value)}
              >
                {dashboardProjects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </Select>
            </div>

            <Button variant="secondary" size="icon" aria-label="Theme">
              <Sun className="h-4 w-4" aria-hidden="true" />
            </Button>

            <Button size="sm">
              <Plus className="h-4 w-4" aria-hidden="true" />
              <span className="hidden sm:inline">New deployment</span>
              <ChevronDown className="hidden h-4 w-4 sm:block" aria-hidden="true" />
            </Button>
          </div>
        </header>

        <main className="mx-auto max-w-7xl px-4 py-5 pb-24 sm:px-6 lg:px-8">
          {section !== "overview" && (
            <WorkspaceHero
              selectedProject={selectedProject}
              currentWorkspace={currentWorkspace?.name ?? "Workspace"}
              section={section}
              projectCount={projects.data?.length ?? 0}
              serverCount={servers.data?.length ?? 0}
              memberCount={members.data?.length ?? 0}
            />
          )}

          {section === "overview" && (
            <OverviewSection
              projectCount={projects.data?.length ?? 0}
              serverCount={servers.data?.length ?? 0}
              memberCount={members.data?.length ?? 0}
              tokenCount={apiTokens.data?.length ?? 0}
              projectsLoading={projects.isLoading}
              serverError={errorMessage(servers.error)}
              currentWorkspace={currentWorkspace?.name ?? "Workspace"}
            />
          )}
          {section === "projects" && (
            <ProjectsSection
              projects={dashboardProjects}
              activeProjectId={activeProjectId}
              setActiveProjectId={setActiveProjectId}
              loading={projects.isLoading}
              error={errorMessage(projects.error)}
              liveCount={projects.data?.length ?? 0}
            />
          )}
          {section === "deployments" && <DeploymentsSection />}
          {section === "servers" && (
            <ServersSection
              liveServers={servers.data ?? []}
              loading={servers.isLoading}
              error={errorMessage(servers.error)}
            />
          )}
          {section === "resources" && (
            <div className="space-y-5">
              <ConfigSection
                environments={environments.data ?? []}
                selectedEnvironmentId={activeEnvironmentId}
                setSelectedEnvironmentId={setActiveEnvironmentId}
                envVars={envVars.data ?? []}
                secrets={secrets.data ?? []}
                environmentLoading={environments.isLoading}
                envVarsLoading={envVars.isLoading}
                secretsLoading={secrets.isLoading}
                error={errorMessage(environments.error || envVars.error || secrets.error)}
                selectedProject={selectedProject}
                selectedEnvironmentName={selectedEnvironment?.name}
              />
              <DomainsPanel />
            </div>
          )}
          {section === "backups" && <BackupSection />}
          {section === "security" && <SecuritySection />}
          {section === "team" && (
            <TeamSection
              members={members.data ?? []}
              apiTokens={apiTokens.data ?? []}
              membersLoading={members.isLoading}
              tokensLoading={apiTokens.isLoading}
              error={errorMessage(members.error || apiTokens.error)}
            />
          )}
          {section === "activity" && <ActivitySection />}
        </main>
      </div>

      <nav className="fixed inset-x-3 bottom-3 z-30 grid grid-cols-5 rounded-lg border border-zinc-200 bg-white p-1 shadow-lg lg:hidden">
        {mobileSections.map((item) => {
          const Icon = item.icon;
          const active = section === item.id;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => setSection(item.id)}
              className={cn(
                "flex h-12 min-w-0 flex-col items-center justify-center gap-1 rounded-md text-[11px] font-medium transition",
                active ? "bg-zinc-950 text-white" : "text-zinc-500 hover:bg-zinc-100 hover:text-zinc-950",
              )}
            >
              <Icon className="h-4 w-4" aria-hidden="true" />
              <span className="max-w-full truncate">{item.label}</span>
            </button>
          );
        })}
      </nav>
    </div>
  );
}

function WorkspaceHero({
  selectedProject,
  currentWorkspace,
  section,
  projectCount,
  serverCount,
  memberCount,
}: {
  selectedProject?: DashboardProject;
  currentWorkspace: string;
  section: Section;
  projectCount: number;
  serverCount: number;
  memberCount: number;
}) {
  const projectStatus = selectedProject?.status ?? "healthy";
  const tone = statusTone(projectStatus);

  return (
    <section className="mb-5 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-[0_18px_60px_rgba(24,24,27,0.08)]">
      <div className="grid lg:grid-cols-[minmax(0,1.35fr)_390px]">
        <div className="min-w-0 p-5 sm:p-6">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone="blue">Light operational UI</Badge>
            <Badge tone="neutral">{sectionCopy[section].title}</Badge>
            <DataBadge status={selectedProject?.source ?? "prototype"} />
          </div>

          <div className="mt-5 flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
            <div className="min-w-0">
              <p className="truncate text-xs font-medium uppercase tracking-[0.16em] text-zinc-400">
                {currentWorkspace}
              </p>
              <h2 className="mt-2 truncate text-3xl font-semibold tracking-tight text-zinc-950 sm:text-4xl">
                {selectedProject?.name ?? "Prototype workspace"}
              </h2>
              <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-500">
                {selectedProject
                  ? `${selectedProject.repo} · ${selectedProject.framework} · ${selectedProject.branch}`
                  : "Backend projects load through ConnectRPC; prototype content keeps the product shape visible."}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="secondary" size="sm">
                <ExternalLink className="h-4 w-4" aria-hidden="true" />
                Visit
              </Button>
              <Button size="sm">
                <Rocket className="h-4 w-4" aria-hidden="true" />
                Deploy
              </Button>
            </div>
          </div>

          <div className="mt-6 grid grid-cols-3 gap-2 sm:gap-3">
            <HeroFact
              icon={CheckCircle2}
              label="Health"
              value={projectStatus === "attention" ? "Needs review" : projectStatus}
              tone={projectStatus === "attention" ? "amber" : tone}
            />
            <HeroFact icon={Server} label="Servers" value={`${serverCount} live`} tone="blue" />
            <HeroFact icon={Users} label="Members" value={`${memberCount} active`} tone="purple" />
          </div>

          <div className="mt-5 grid grid-cols-3 gap-2 rounded-xl border border-zinc-200 bg-zinc-50 p-2">
            {[
              ["Projects", projectCount, "Live workspace records"],
              ["Deploys", "4", "Planned timeline"],
              ["Rollback", "Ready", "Mock safety state"],
            ].map(([label, value, detail]) => (
              <div key={label} className="rounded-lg bg-white px-3 py-2 shadow-sm">
                <div className="text-lg font-semibold text-zinc-950">{value}</div>
                <div className="truncate text-xs font-medium text-zinc-500">{label}</div>
                <div className="mt-1 hidden truncate text-[11px] text-zinc-400 sm:block">{detail}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="border-t border-zinc-200 bg-zinc-950 p-4 text-white lg:border-l lg:border-t-0">
          <div className="rounded-xl border border-white/10 bg-white/[0.04] p-3 shadow-2xl">
            <div className="mb-3 flex items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold text-white">Production release</p>
                <p className="mt-0.5 truncate text-xs text-zinc-400">
                  {selectedProject?.branch ?? "main"} · rollback target kept
                </p>
              </div>
              <Badge tone="green">Ready</Badge>
            </div>

            <div className="space-y-2">
              {[
                ["Build", "48s", "green"],
                ["Health", "Passing", "green"],
                ["Caddy route", "Switched", "blue"],
              ].map(([label, value, stepTone]) => (
                <div
                  key={label}
                  className="flex items-center justify-between gap-3 rounded-lg border border-white/10 bg-black/20 px-3 py-2"
                >
                  <StatusDot tone={stepTone as "green" | "blue"} label={label} />
                  <span className="text-sm font-medium text-white">{value}</span>
                </div>
              ))}
            </div>

            <div className="mt-3 rounded-lg border border-white/10 bg-black px-3 py-2 font-mono text-[11px] leading-5 text-zinc-300">
              <div className="flex items-center gap-2 text-zinc-500">
                <Terminal className="h-3.5 w-3.5" aria-hidden="true" />
                preview log
              </div>
              <p className="mt-1 truncate text-emerald-300">health check passed on retry 2</p>
              <p className="truncate text-sky-300">route switched to {selectedProject?.slug ?? "storefront"}</p>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function HeroFact({
  icon: Icon,
  label,
  value,
  tone,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  tone: "green" | "amber" | "red" | "blue" | "purple" | "neutral";
}) {
  const iconClasses = {
    green: "border-emerald-200 bg-emerald-50 text-emerald-700",
    amber: "border-amber-200 bg-amber-50 text-amber-700",
    red: "border-red-200 bg-red-50 text-red-700",
    blue: "border-blue-200 bg-blue-50 text-blue-700",
    purple: "border-violet-200 bg-violet-50 text-violet-700",
    neutral: "border-zinc-200 bg-zinc-50 text-zinc-600",
  };

  return (
    <div className="flex min-w-0 flex-col items-start gap-2 rounded-xl border border-zinc-200 bg-white px-2.5 py-3 shadow-sm sm:flex-row sm:items-center sm:gap-3 sm:px-3">
      <span className={cn("grid h-8 w-8 shrink-0 place-items-center rounded-lg border sm:h-9 sm:w-9", iconClasses[tone])}>
        <Icon className="h-4 w-4" aria-hidden="true" />
      </span>
      <div className="min-w-0">
        <p className="truncate text-xs text-zinc-500">{label}</p>
        <p className="truncate text-sm font-semibold capitalize text-zinc-950">{value}</p>
      </div>
    </div>
  );
}

function OverviewSection({
  projectCount,
  serverCount,
  memberCount,
  tokenCount,
  projectsLoading,
  serverError,
  currentWorkspace,
}: {
  projectCount: number;
  serverCount: number;
  memberCount: number;
  tokenCount: number;
  projectsLoading: boolean;
  serverError: string;
  currentWorkspace: string;
}) {
  const displayProjects = projectsLoading ? "..." : String(projectCount || 6);
  const displayServers = serverCount || 3;
  const displayMembers = memberCount || 4;
  const displayTokens = tokenCount || 2;

  return (
    <div className="space-y-5">
      <div className="rounded-2xl border border-zinc-200 bg-white p-5 shadow-[0_18px_60px_rgba(24,24,27,0.06)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <div className="mb-4 flex flex-wrap items-center gap-2 text-xs font-medium text-zinc-500">
              <Users className="h-3.5 w-3.5 text-violet-600" aria-hidden="true" />
              <span className="truncate">{currentWorkspace}</span>
              <span>/</span>
              <span className="font-semibold text-zinc-900">Workspace dashboard</span>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-3xl font-semibold tracking-tight text-zinc-950">
                Workspace dashboard
              </h2>
              <DataBadge status="live" />
              <DataBadge status="prototype" />
            </div>
            <p className="mt-3 max-w-3xl text-sm leading-6 text-zinc-500">
              Monitor the whole workspace: projects, servers, access, configuration,
              readiness, backups, and connected backend surfaces from one screen.
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="secondary" size="sm">
              <Search className="h-4 w-4" aria-hidden="true" />
              Inspect workspace
            </Button>
            <Button size="sm">
              <Plus className="h-4 w-4" aria-hidden="true" />
              Add resource
            </Button>
          </div>
        </div>

        <div className="mt-6 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <WireStatCard
            icon={ShieldCheck}
            label="Workspace health"
            value="Passing"
            detail="All systems operational"
            tone="green"
            bars={[22, 30, 25, 42, 28, 36, 44, 38, 46, 52]}
          />
          <WireStatCard
            icon={Box}
            label="Projects"
            value={displayProjects}
            detail="Live workspace records"
            tone="blue"
            bars={[24, 34, 30, 38, 42, 36, 44, 39, 41, 43]}
          />
          <WireStatCard
            icon={Server}
            label="Servers"
            value={String(displayServers)}
            detail={serverError || "Connected machines"}
            tone="purple"
            bars={[32, 28, 36, 33, 40, 35, 46, 38, 44, 54]}
          />
          <WireStatCard
            icon={HardDrive}
            label="Backup posture"
            value="7 checks"
            detail="Restore confidence planned"
            tone="neutral"
          />
        </div>
      </div>

      <Panel className="overflow-hidden">
        <div className="grid grid-cols-2 divide-y divide-zinc-100 sm:grid-cols-4 lg:grid-cols-8 lg:divide-x lg:divide-y-0">
          <WireSummaryItem icon={Box} label="Projects" value={displayProjects} />
          <WireSummaryItem icon={Server} label="Servers" value={String(displayServers)} />
          <WireSummaryItem icon={Users} label="Members" value={String(displayMembers)} />
          <WireSummaryItem icon={KeyRound} label="API tokens" value={String(displayTokens)} />
          <WireSummaryItem icon={Gauge} label="Capacity" value="42%" />
          <WireSummaryItem icon={HardDrive} label="Backups" value="7" ok />
          <WireSummaryItem icon={Activity} label="Monthly cost" value="$28.74" />
          <WireSummaryItem icon={ShieldCheck} label="Security" value="Excellent" ok />
        </div>
      </Panel>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.25fr)_minmax(320px,0.75fr)]">
        <ReadinessDoctor />
        <WorkspaceOperationsPanel />
      </div>

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.78fr)]">
        <RecentActivityWire />
        <SurfaceStatusWire />
      </div>

      <ByosCard />
    </div>
  );
}

function WireStatCard({
  icon: Icon,
  label,
  value,
  detail,
  tone,
  bars,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  detail: string;
  tone: "green" | "blue" | "purple" | "neutral";
  bars?: number[];
}) {
  const toneClasses = {
    green: "border-emerald-200 bg-emerald-50 text-emerald-700",
    blue: "border-blue-200 bg-blue-50 text-blue-700",
    purple: "border-violet-200 bg-violet-50 text-violet-700",
    neutral: "border-zinc-200 bg-zinc-50 text-zinc-600",
  };

  return (
    <div className="rounded-xl border border-zinc-200 bg-white p-4 shadow-sm">
      <div className="flex items-start gap-3">
        <span className={cn("grid h-10 w-10 shrink-0 place-items-center rounded-lg border", toneClasses[tone])}>
          <Icon className="h-5 w-5" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <p className="truncate text-sm text-zinc-500">{label}</p>
          <p className="mt-1 truncate text-xl font-semibold text-zinc-950">{value}</p>
        </div>
      </div>
      <div className="mt-4 flex items-end justify-between gap-3">
        <StatusDot tone={tone === "neutral" ? "green" : tone} label={detail} />
        {bars && <MiniBars values={bars} />}
      </div>
    </div>
  );
}

function WireSummaryItem({
  icon: Icon,
  label,
  value,
  ok,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  ok?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-center gap-3 px-4 py-3">
      <Icon className="h-5 w-5 shrink-0 text-zinc-500" aria-hidden="true" />
      <div className="min-w-0">
        <p className="truncate text-xs text-zinc-500">{label}</p>
        <div className="flex items-center gap-1.5">
          <p className="truncate text-sm font-semibold text-zinc-950">{value}</p>
          {ok && <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-emerald-600" aria-hidden="true" />}
        </div>
      </div>
    </div>
  );
}

function ReadinessDoctor() {
  const checks = [
    ["Build", "Ready", "green"],
    ["Start command", "Ready", "green"],
    ["Environment vars", "Ready", "green"],
    ["Secrets", "Ready", "green"],
    ["Database", "Ready", "green"],
    ["DNS & SSL", "Ready", "green"],
    ["Recovery & rollback", "Warning", "amber"],
    ["Observability", "Ready", "green"],
  ];

  return (
    <Panel>
      <div className="flex flex-col gap-4 border-b border-zinc-100 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-blue-200 bg-blue-50 text-blue-700">
            <ShieldCheck className="h-5 w-5" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-base font-semibold text-zinc-950">Production readiness doctor</h3>
              <DataBadge status="planned" />
            </div>
            <p className="mt-1 text-sm text-zinc-500">
              We continuously validate your production configuration and best practices.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-zinc-500">Readiness score</span>
          <div className="grid h-10 w-10 place-items-center rounded-full border-[5px] border-emerald-500 border-l-zinc-200 text-sm font-semibold text-zinc-950">
            92
          </div>
          <span className="text-sm text-zinc-400">/100</span>
        </div>
      </div>
      <div className="grid gap-0 p-4 sm:grid-cols-2 lg:grid-cols-4">
        {checks.map(([label, value, tone]) => (
          <div key={label} className="flex items-start gap-3 border-zinc-100 px-2 py-3 lg:border-r last:border-r-0">
            {tone === "amber" ? (
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" aria-hidden="true" />
            ) : (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" aria-hidden="true" />
            )}
            <div className="min-w-0">
              <p className="truncate text-sm text-zinc-700">{label}</p>
              <p className={cn("mt-1 text-xs font-medium", tone === "amber" ? "text-amber-600" : "text-emerald-700")}>
                {value}
              </p>
            </div>
          </div>
        ))}
      </div>
      <div className="border-t border-zinc-100 px-5 py-3 text-center">
        <button className="inline-flex items-center gap-2 text-xs font-medium text-zinc-500" type="button">
          View full report
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    </Panel>
  );
}

function WorkspaceOperationsPanel() {
  const rows = [
    { label: "Deployments", value: "4 recent", detail: "Timeline is planned", icon: Rocket, status: "planned" as const },
    { label: "Servers", value: "3 online", detail: "Names come from live API", icon: Server, status: "live" as const },
    { label: "Resources", value: "Env + secrets", detail: "Secret values stay write-only", icon: KeyRound, status: "live" as const },
    { label: "Backups", value: "7 checks", detail: "Restore testing planned", icon: HardDrive, status: "planned" as const },
  ];

  return (
    <Panel>
      <PanelHeader
        title="Workspace operations"
        description="Dashboard-wide operational state without drilling into a single deployment."
        status="prototype"
      />
      <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-1">
        {rows.map((row) => {
          const Icon = row.icon;
          return (
            <div
              key={row.label}
              className="flex items-center gap-3 rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-3"
            >
              <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-zinc-200 bg-white text-zinc-600 shadow-sm">
                <Icon className="h-5 w-5" aria-hidden="true" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <p className="truncate text-sm font-semibold text-zinc-950">{row.label}</p>
                  <DataBadge status={row.status} />
                </div>
                <p className="mt-1 truncate text-xs text-zinc-500">{row.detail}</p>
              </div>
              <span className="shrink-0 text-sm font-semibold text-zinc-900">{row.value}</span>
            </div>
          );
        })}
      </div>
    </Panel>
  );
}

function ByosCard() {
  return (
    <section className="rounded-2xl border border-zinc-200 bg-zinc-100/80 p-5 shadow-sm">
      <div className="flex items-start gap-4">
        <span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-zinc-200 bg-white text-zinc-900 shadow-sm">
          <ShieldCheck className="h-6 w-6" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h3 className="text-base font-semibold text-zinc-950">Your infrastructure. Your data.</h3>
          <p className="mt-1 text-sm leading-6 text-zinc-500">
            Plorigo is BYOS. You run it. You own it.
          </p>
          <button className="mt-4 inline-flex items-center gap-2 text-sm font-medium text-blue-600" type="button">
            Learn more about BYOS
            <ArrowUpRight className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
      </div>
    </section>
  );
}

function RecentActivityWire() {
  const items = [
    ["Build passed", "Production build completed successfully", "2m ago"],
    ["Health checks passed", "All health checks are passing", "2m ago"],
    ["Route switched to new release", "Traffic successfully routed to release #1287", "2m ago"],
    ["Automated backup completed", "Backup stored in us-east-1", "32m ago"],
  ];

  return (
    <Panel>
      <PanelHeader title="Recent activity" status="prototype" />
      <div className="divide-y divide-zinc-100 px-4">
        {items.map(([title, detail, time]) => (
          <div key={title} className="flex gap-3 py-3">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-zinc-950">{title}</p>
              <p className="mt-1 truncate text-xs text-zinc-500">{detail}</p>
            </div>
            <span className="shrink-0 text-xs text-zinc-500">{time}</span>
          </div>
        ))}
      </div>
      <div className="border-t border-zinc-100 px-4 py-3 text-center">
        <button className="inline-flex items-center gap-2 text-xs font-medium text-zinc-500" type="button">
          View all activity
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    </Panel>
  );
}

function SurfaceStatusWire() {
  return (
    <Panel>
      <PanelHeader title="Surface status" status="live" />
      <div className="divide-y divide-zinc-100 px-4">
        {apiSurface.slice(0, 4).map((item) => (
          <div key={item.name} className="flex items-center gap-3 py-3">
            <Users className="h-4 w-4 shrink-0 text-zinc-500" aria-hidden="true" />
            <p className="min-w-0 flex-1 truncate text-sm font-medium text-zinc-700">{item.name}</p>
            <span className="hidden text-xs text-zinc-500 sm:inline">All systems operational</span>
            <span className={cn("h-2 w-2 shrink-0 rounded-full", item.status === "live" ? "bg-emerald-500" : "bg-zinc-300")} />
          </div>
        ))}
      </div>
      <div className="border-t border-zinc-100 px-4 py-3 text-center">
        <button className="inline-flex items-center gap-2 text-xs font-medium text-zinc-500" type="button">
          Manage surfaces
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    </Panel>
  );
}

function ProjectsSection({
  projects,
  activeProjectId,
  setActiveProjectId,
  loading,
  error,
  liveCount,
}: {
  projects: DashboardProject[];
  activeProjectId: string;
  setActiveProjectId: (projectId: string) => void;
  loading: boolean;
  error: string;
  liveCount: number;
}) {
  return (
    <Panel>
      <PanelHeader
        title="Project inventory"
        description={
          liveCount > 0
            ? "Project rows are loaded from the current workspace."
            : "No backend projects yet, so prototype rows keep the dashboard complete."
        }
        status={liveCount > 0 ? "live" : "prototype"}
        action={
          <Button size="sm" disabled>
            <Box className="h-4 w-4" aria-hidden="true" />
            New project
          </Button>
        }
      />
      <div className="p-4">
        {loading && <ProjectSkeleton />}
        {error && (
          <EmptyState
            title="Project API returned an error"
            body={error}
            status="live"
          />
        )}
        {!loading && !error && (
          <div className="grid gap-3 lg:grid-cols-3">
            {projects.map((project) => (
              <button
                key={project.id}
                type="button"
                onClick={() => setActiveProjectId(project.id)}
                className={cn(
                  "group min-w-0 overflow-hidden rounded-xl border bg-white text-left shadow-sm transition hover:-translate-y-0.5 hover:border-zinc-300 hover:shadow-[0_12px_34px_rgba(24,24,27,0.08)]",
                  activeProjectId === project.id ? "border-zinc-950 ring-2 ring-zinc-950/5" : "border-zinc-200",
                )}
              >
                <div className="border-b border-zinc-100 bg-zinc-950 p-3 text-white">
                  <div className="rounded-lg border border-white/10 bg-white/[0.04] p-2">
                    <div className="mb-3 flex items-center gap-1.5">
                      <span className="h-2 w-2 rounded-full bg-red-400" />
                      <span className="h-2 w-2 rounded-full bg-amber-400" />
                      <span className="h-2 w-2 rounded-full bg-emerald-400" />
                      <span className="ml-auto text-[10px] text-zinc-500">{project.slug}</span>
                    </div>
                    <div className="grid gap-1.5">
                      <div className="h-2 w-3/4 rounded bg-white/25" />
                      <div className="h-2 w-1/2 rounded bg-white/15" />
                      <div className="mt-2 grid grid-cols-3 gap-1.5">
                        <div className="h-9 rounded bg-emerald-400/20" />
                        <div className="h-9 rounded bg-sky-400/20" />
                        <div className="h-9 rounded bg-violet-400/20" />
                      </div>
                    </div>
                  </div>
                </div>

                <div className="p-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-zinc-950">{project.name}</p>
                      <p className="mt-1 truncate text-xs text-zinc-500">{project.repo}</p>
                    </div>
                    <DataBadge status={project.source} />
                  </div>
                  <div className="mt-4 space-y-2">
                    <ProjectMeta icon={Globe2} label={project.url} />
                    <ProjectMeta icon={GitBranch} label={project.branch} />
                    <ProjectMeta icon={Layers3} label={project.framework} />
                  </div>
                  <div className="mt-4 flex items-center justify-between border-t border-zinc-100 pt-3">
                    <StatusDot tone={statusTone(project.status)} label={project.status} />
                    <span className="text-xs text-zinc-500">{project.updated}</span>
                  </div>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </Panel>
  );
}

function ProjectMeta({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <div className="flex min-w-0 items-center gap-2 text-xs text-zinc-500">
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="truncate">{label}</span>
    </div>
  );
}

function DeploymentsSection() {
  return (
    <div className="space-y-5">
      <div className="grid gap-3 md:grid-cols-3">
        {[
          { label: "Latest production", value: "Ready", detail: "main · 8f41c2a", tone: "green" as const, icon: CheckCircle2 },
          { label: "Preview build", value: "Building", detail: "release/checkout", tone: "blue" as const, icon: Rocket },
          { label: "Needs attention", value: "1 failed", detail: "staging worker", tone: "amber" as const, icon: AlertTriangle },
        ].map((item) => {
          const Icon = item.icon;
          return (
            <div
              key={item.label}
              className="rounded-xl border border-zinc-200 bg-white p-4 shadow-sm"
            >
              <div className="flex items-center justify-between gap-3">
                <StatusDot tone={item.tone} label={item.label} />
                <DataBadge status="planned" />
              </div>
              <div className="mt-3 flex items-end justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-2xl font-semibold tracking-tight text-zinc-950">{item.value}</p>
                  <p className="mt-1 truncate text-xs text-zinc-500">{item.detail}</p>
                </div>
                <Icon className="h-5 w-5 text-zinc-400" aria-hidden="true" />
              </div>
            </div>
          );
        })}
      </div>
      <Panel>
        <PanelHeader
          title="Deployment timeline"
          description="Dense prototype list for planned deployment records, branches, commits, and rollback context."
          status="planned"
          action={
            <Button variant="secondary" size="sm" disabled>
              <ChevronDown className="h-4 w-4" aria-hidden="true" />
              Status filter
            </Button>
          }
        />
        <div className="overflow-x-auto">
          <table className="min-w-full text-left text-sm">
            <thead className="border-b border-zinc-100 bg-zinc-50 text-xs font-medium text-zinc-500">
              <tr>
                <th className="px-4 py-2">Deployment</th>
                <th className="px-4 py-2">Environment</th>
                <th className="px-4 py-2">Branch</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2">Duration</th>
                <th className="px-4 py-2">Source</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100">
              {deployments.map((deployment) => (
                <tr key={deployment.id} className="bg-white hover:bg-zinc-50">
                  <td className="px-4 py-3">
                    <div className="min-w-0">
                      <p className="truncate font-medium text-zinc-950">{deployment.project}</p>
                      <p className="truncate text-xs text-zinc-500">
                        {deployment.commit} by {deployment.author} · {deployment.age}
                      </p>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-zinc-600">{deployment.environment}</td>
                  <td className="px-4 py-3 text-zinc-600">{deployment.branch}</td>
                  <td className="px-4 py-3">
                    <StatusDot tone={statusTone(deployment.status)} label={deployment.status} />
                  </td>
                  <td className="px-4 py-3 text-zinc-600">{deployment.duration}</td>
                  <td className="px-4 py-3">
                    <DataBadge status={deployment.source} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
      <div className="grid gap-5 lg:grid-cols-2">
        <LogsPanel />
        <BackupsPanel />
      </div>
    </div>
  );
}

function ServersSection({
  liveServers,
  loading,
  error,
}: {
  liveServers: Array<{ id: string; name: string; slug: string; createdAt: string }>;
  loading: boolean;
  error: string;
}) {
  const rows =
    liveServers.length > 0
      ? liveServers.map((server) => ({
          name: server.name,
          region: "Workspace server",
          cpu: "not reported",
          memory: "not reported",
          disk: "not reported",
          status: "online" as const,
          source: "live" as DataStatus,
          slug: server.slug,
          createdAt: server.createdAt,
        }))
      : serverHealth;

  return (
    <Panel>
      <PanelHeader
        title="Server fleet"
        description={
          liveServers.length > 0
            ? "Server names and slugs are loaded from the backend; runtime metrics are not implemented yet."
            : "Prototype health rows show the target server operations view."
        }
        status={liveServers.length > 0 ? "live" : "prototype"}
        action={
          <Button size="sm" disabled>
            <Server className="h-4 w-4" aria-hidden="true" />
            Connect server
          </Button>
        }
      />
      <div className="p-4">
        {loading && <ProjectSkeleton />}
        {error && <EmptyState title="Server API returned an error" body={error} status="live" />}
        {!loading && !error && (
          <div className="grid gap-3 lg:grid-cols-3">
            {rows.map((server) => (
              <div key={server.name} className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold text-zinc-950">{server.name}</p>
                    <p className="mt-1 truncate text-xs text-zinc-500">{server.region}</p>
                  </div>
                  <DataBadge status={server.source} />
                </div>
                <div className="mt-4 space-y-3 text-xs text-zinc-600">
                  <ResourceMeter label="CPU" value={server.cpu} tone="blue" />
                  <ResourceMeter label="Memory" value={server.memory} tone="purple" />
                  <ResourceMeter label="Disk" value={server.disk} tone="green" />
                </div>
                <div className="mt-4 flex items-center justify-between">
                  <StatusDot tone={statusTone(server.status)} label={server.status} />
                  <Cpu className="h-4 w-4 text-zinc-400" aria-hidden="true" />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </Panel>
  );
}

function ResourceMeter({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone: "blue" | "green" | "purple";
}) {
  const width = percentFromLabel(value);
  const tones = {
    blue: "bg-blue-500",
    green: "bg-emerald-500",
    purple: "bg-violet-500",
  };

  return (
    <div>
      <div className="mb-1 flex items-center justify-between gap-3">
        <span>{label}</span>
        <span className="truncate font-medium text-zinc-900">{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-zinc-100">
        <div
          className={cn("h-full rounded-full", tones[tone])}
          style={{ width: `${width}%` }}
        />
      </div>
    </div>
  );
}

function ConfigSection({
  environments,
  selectedEnvironmentId,
  setSelectedEnvironmentId,
  envVars,
  secrets,
  environmentLoading,
  envVarsLoading,
  secretsLoading,
  error,
  selectedProject,
  selectedEnvironmentName,
}: {
  environments: Array<{ id: string; name: string; slug: string; type: string }>;
  selectedEnvironmentId: string;
  setSelectedEnvironmentId: (environmentId: string) => void;
  envVars: Array<{ id: string; key: string; value: string; updatedAt: string }>;
  secrets: Array<{ id: string; key: string; updatedAt: string }>;
  environmentLoading: boolean;
  envVarsLoading: boolean;
  secretsLoading: boolean;
  error: string;
  selectedProject?: DashboardProject;
  selectedEnvironmentName?: string;
}) {
  const liveItems = [
    ...envVars.map((item) => ({
      key: item.key,
      value: item.value,
      kind: "env" as const,
      updated: formatDate(item.updatedAt),
      source: "live" as DataStatus,
    })),
    ...secrets.map((item) => ({
      key: item.key,
      value: "write-only",
      kind: "secret" as const,
      updated: formatDate(item.updatedAt),
      source: "live" as DataStatus,
    })),
  ];
  const rows = liveItems.length > 0 ? liveItems : configItems;

  return (
    <div className="space-y-5">
      <Panel>
        <PanelHeader
          title="Environment selector"
          description={
            selectedProject?.source === "live"
              ? "Environment records load from the selected backend project."
              : "Prototype project selected, so environment data is illustrative."
          }
          status={selectedProject?.source === "live" ? "live" : "prototype"}
        />
        <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <Select
              value={selectedEnvironmentId}
              onChange={(event) => setSelectedEnvironmentId(event.target.value)}
              disabled={environments.length === 0}
            >
              {environments.map((environment) => (
                <option key={environment.id} value={environment.id}>
                  {environment.name} ({environment.type || environment.slug})
                </option>
              ))}
              {environments.length === 0 && <option value="">Production prototype</option>}
            </Select>
          </div>
          <DataBadge status={environments.length > 0 ? "live" : "prototype"} />
        </div>
      </Panel>

      <Panel>
        <PanelHeader
          title={`Variables and secrets${selectedEnvironmentName ? `: ${selectedEnvironmentName}` : ""}`}
          description="Env vars can be returned by the backend; secret values stay write-only and show metadata only."
          status={liveItems.length > 0 ? "live" : "prototype"}
          action={
            <Button size="sm" disabled>
              <LockKeyhole className="h-4 w-4" aria-hidden="true" />
              Add value
            </Button>
          }
        />
        <div className="p-4">
          {(environmentLoading || envVarsLoading || secretsLoading) && <ProjectSkeleton />}
          {error && <EmptyState title="Config API returned an error" body={error} status="live" />}
          {!environmentLoading && !envVarsLoading && !secretsLoading && !error && (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-sm">
                <thead className="border-b border-zinc-100 bg-zinc-50 text-xs font-medium text-zinc-500">
                  <tr>
                    <th className="px-4 py-2">Key</th>
                    <th className="px-4 py-2">Value</th>
                    <th className="px-4 py-2">Kind</th>
                    <th className="px-4 py-2">Updated</th>
                    <th className="px-4 py-2">Source</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-100">
                  {rows.map((item) => (
                    <tr key={`${item.kind}-${item.key}`} className="bg-white hover:bg-zinc-50">
                      <td className="px-4 py-3 font-medium text-zinc-950">{item.key}</td>
                      <td className="max-w-[260px] truncate px-4 py-3 text-zinc-600">
                        {item.kind === "secret" ? "••••••••••••" : item.value}
                      </td>
                      <td className="px-4 py-3">
                        <Badge tone={item.kind === "secret" ? "purple" : "blue"}>{item.kind}</Badge>
                      </td>
                      <td className="px-4 py-3 text-zinc-600">{item.updated}</td>
                      <td className="px-4 py-3">
                        <DataBadge status={item.source} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </Panel>
    </div>
  );
}

function TeamSection({
  members,
  apiTokens,
  membersLoading,
  tokensLoading,
  error,
}: {
  members: Array<{ userId: string; email: string; role: string; createdAt: string }>;
  apiTokens: Array<{ id: string; name: string; tokenPrefix: string; lastUsedAt: string; createdAt: string }>;
  membersLoading: boolean;
  tokensLoading: boolean;
  error: string;
}) {
  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
      <Panel>
        <PanelHeader
          title="Workspace members"
          description="Membership rows are loaded from the workspace API when available."
          status="live"
          action={
            <Button size="sm" disabled>
              <Users className="h-4 w-4" aria-hidden="true" />
              Invite
            </Button>
          }
        />
        <div className="p-4">
          {membersLoading && <ProjectSkeleton />}
          {error && <EmptyState title="Access API returned an error" body={error} status="live" />}
          {!membersLoading && !error && members.length === 0 && (
            <EmptyState title="No members returned" body="Workspace membership is live, but this account has no rows to show." status="live" />
          )}
          {!membersLoading && !error && members.length > 0 && (
            <div className="space-y-2">
              {members.map((member) => (
                <div
                  key={member.userId}
                  className="flex items-center justify-between gap-3 rounded-md border border-zinc-200 bg-white px-3 py-2"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-zinc-950">{member.email}</p>
                    <p className="text-xs text-zinc-500">Joined {formatDate(member.createdAt)}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge tone="neutral">{member.role}</Badge>
                    <DataBadge status="live" />
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Panel>

      <Panel>
        <PanelHeader
          title="API tokens"
          description="Token metadata is live; raw token values are never returned."
          status="live"
        />
        <div className="space-y-2 p-4">
          {tokensLoading && <ProjectSkeleton />}
          {!tokensLoading && apiTokens.length === 0 && (
            <EmptyState title="No tokens returned" body="Create-token actions stay disabled in this mock dashboard pass." status="live" />
          )}
          {!tokensLoading &&
            apiTokens.map((token) => (
              <div key={token.id} className="rounded-md border border-zinc-200 bg-zinc-50 p-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="truncate text-sm font-medium text-zinc-950">{token.name}</p>
                  <DataBadge status="live" />
                </div>
                <p className="mt-1 text-xs text-zinc-500">
                  {token.tokenPrefix} · last used {token.lastUsedAt ? formatDate(token.lastUsedAt) : "never"}
                </p>
              </div>
            ))}
        </div>
      </Panel>
    </div>
  );
}

function BackupSection() {
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
      <BackupsPanel />
      <Panel>
        <PanelHeader
          title="Restore confidence"
          description="Restore tests are planned and shown here as a wire state."
          status="planned"
        />
        <div className="space-y-3 p-4">
          {[
            ["Last restore test", "Pending"],
            ["Retention", "14 days"],
            ["Encrypted snapshots", "Planned"],
          ].map(([label, value]) => (
            <div key={label} className="flex items-center justify-between rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2">
              <span className="text-sm text-zinc-600">{label}</span>
              <span className="text-sm font-semibold text-zinc-950">{value}</span>
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}

function SecuritySection() {
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
      <Panel>
        <PanelHeader
          title="Production guardrails"
          description="Planned safety controls modeled from the architecture principles."
          status="planned"
        />
        <div className="grid gap-3 p-4 sm:grid-cols-2">
          {[
            ["Recovery path", "Rollback target kept", "green"],
            ["Secret handling", "Write-only values", "green"],
            ["AI operations", "Approval required", "amber"],
            ["Audit trail", "Action history", "green"],
          ].map(([label, value, tone]) => (
            <div key={label} className="rounded-xl border border-zinc-200 bg-white p-4 shadow-sm">
              <StatusDot tone={tone as "green" | "amber"} label={label} />
              <p className="mt-3 text-sm font-semibold text-zinc-950">{value}</p>
            </div>
          ))}
        </div>
      </Panel>
      <ByosCard />
    </div>
  );
}

function ActivitySection() {
  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
      <Panel>
        <PanelHeader
          title="Recent activity"
          description="Prototype timeline for deploy, server, secret, and approval events."
          status="prototype"
        />
        <div className="divide-y divide-zinc-100">
          {activity.map((item) => (
            <div key={`${item.title}-${item.time}`} className="flex gap-3 px-4 py-3">
              <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-blue-500" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-sm font-medium text-zinc-950">{item.title}</p>
                  <DataBadge status={item.status} />
                </div>
                <p className="mt-1 text-sm leading-6 text-zinc-500">{item.detail}</p>
              </div>
              <span className="shrink-0 text-xs text-zinc-500">{item.time}</span>
            </div>
          ))}
        </div>
      </Panel>
      <Panel>
        <PanelHeader title="Production guardrails" description="Planned safety flows shown as mock UI." status="planned" />
        <div className="space-y-3 p-4">
          {["Human approval before production migrations", "Rollback target visible for each deploy", "Backup restore confidence tracked"].map(
            (item) => (
              <div key={item} className="flex items-center gap-3 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
                <ShieldCheck className="h-4 w-4 shrink-0 text-emerald-600" aria-hidden="true" />
                <span className="min-w-0 text-sm text-zinc-700">{item}</span>
              </div>
            ),
          )}
        </div>
      </Panel>
    </div>
  );
}

function LogsPanel() {
  return (
    <Panel>
      <PanelHeader
        title="Runtime logs"
        description="Searchable logs are planned; these rows are mock lines for visual confirmation."
        status="planned"
        action={
          <Button variant="secondary" size="sm" disabled>
            <Search className="h-4 w-4" aria-hidden="true" />
            Filter
          </Button>
        }
      />
      <div className="space-y-2 p-4">
        {logLines.map((line) => (
          <div
            key={`${line.time}-${line.service}`}
            className="grid gap-2 rounded-md border border-zinc-200 bg-zinc-950 px-3 py-2 text-xs text-zinc-100 sm:grid-cols-[64px_92px_minmax(0,1fr)]"
          >
            <span className="font-mono text-zinc-400">{line.time}</span>
            <span className={cn("font-mono", line.level === "error" ? "text-red-300" : line.level === "warn" ? "text-amber-300" : "text-emerald-300")}>
              {line.service}
            </span>
            <span className="min-w-0 truncate font-mono">{line.message}</span>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function DomainsPanel() {
  return (
    <Panel>
      <PanelHeader
        title="Domains"
        description="Domain and SSL management is planned in the deployment engine."
        status="planned"
      />
      <div className="space-y-2 p-4">
        {domains.map((domain) => (
          <div
            key={domain.domain}
            className="flex items-center justify-between gap-3 rounded-md border border-zinc-200 bg-white px-3 py-2"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-zinc-950">{domain.domain}</p>
              <p className="truncate text-xs text-zinc-500">{domain.target}</p>
            </div>
            <div className="flex items-center gap-2">
              <StatusDot tone={statusTone(domain.status)} label={domain.status} />
              <DataBadge status={domain.source} />
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function BackupsPanel() {
  return (
    <Panel>
      <PanelHeader
        title="Backups"
        description="Backup and restore confidence is planned, with restore status always visible."
        status="planned"
      />
      <div className="space-y-2 p-4">
        {backups.map((backup) => (
          <div
            key={backup.name}
            className="flex items-center justify-between gap-3 rounded-md border border-zinc-200 bg-white px-3 py-2"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-zinc-950">{backup.name}</p>
              <p className="truncate text-xs text-zinc-500">
                {backup.schedule} · last run {backup.lastRun}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <StatusDot tone={statusTone(backup.status)} label={backup.status} />
              <DataBadge status={backup.source} />
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function ProjectSkeleton() {
  return (
    <div className="grid gap-3 md:grid-cols-3">
      {[0, 1, 2].map((item) => (
        <div key={item} className="rounded-lg border border-zinc-200 bg-white p-4">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="mt-3 h-3 w-full" />
          <Skeleton className="mt-2 h-3 w-4/5" />
          <Skeleton className="mt-5 h-8 w-full" />
        </div>
      ))}
    </div>
  );
}
