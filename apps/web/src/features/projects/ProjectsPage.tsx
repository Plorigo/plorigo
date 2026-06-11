import { useEffect, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Box, GitBranch, GitFork, Globe2, Grid2X2, List, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Button, EmptyState, Select, Skeleton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { errorMessage } from "@/lib/format";
import type { DashboardProject } from "@/lib/mockDashboard";
import { useWorkspaceStore } from "@/store";
import { ProjectCard, ProjectListRow } from "./components/ProjectViews";
import { ImportFromGitHubDialog } from "./ImportFromGitHubDialog";
import { NewProjectDialog } from "./NewProjectDialog";
import { useDashboardProjects } from "./projectData";

function statusRank(status: DashboardProject["status"]): number {
  if (status === "attention") return 3;
  if (status === "warning") return 2;
  if (status === "building") return 1;
  return 0;
}

export function ProjectsPage() {
  const { query, dashboardProjects, liveCount } = useDashboardProjects();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const [search, setSearch] = useState("");
  const [tab, setTab] = useState("all");
  const [frameworkFilter, setFrameworkFilter] = useState("all");
  const [healthFilter, setHealthFilter] = useState("all");
  const [sortKey, setSortKey] = useState("newest");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [createOpen, setCreateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const queryClient = useQueryClient();

  // Surface the outcome of the GitHub OAuth redirect (?github=connected|error) as a
  // toast, refresh the connection state, then strip the params so it doesn't repeat.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const status = params.get("github");
    if (!status) return;
    if (status === "connected") {
      toast.success("GitHub connected");
      void queryClient.invalidateQueries({ queryKey: ["githubConnection"] });
    } else if (status === "error") {
      toast.error(params.get("reason") || "Could not connect GitHub");
    }
    params.delete("github");
    params.delete("reason");
    const qs = params.toString();
    window.history.replaceState({}, "", window.location.pathname + (qs ? `?${qs}` : ""));
  }, [queryClient]);

  const error = errorMessage(query.error);

  const frameworks = useMemo(
    () => ["all", ...Array.from(new Set(dashboardProjects.map((p) => p.framework))).sort()],
    [dashboardProjects],
  );

  const productionCount = dashboardProjects.filter((p) =>
    p.environments.some((e) => e.name.toLowerCase().includes("production")),
  ).length;
  const previewCount = dashboardProjects.filter((p) =>
    p.environments.some((e) => e.name.toLowerCase().includes("preview")),
  ).length;
  const attentionCount = dashboardProjects.filter((p) => ["attention", "warning"].includes(p.status)).length;

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const rows = dashboardProjects.filter((project) => {
      const matchesQuery =
        q.length === 0 ||
        [project.name, project.repo, project.framework, project.branch, project.url, project.status]
          .join(" ")
          .toLowerCase()
          .includes(q);
      const matchesTab =
        tab === "all" ||
        (tab === "production" && project.environments.some((e) => e.name.toLowerCase().includes("production"))) ||
        (tab === "preview" && project.environments.some((e) => e.name.toLowerCase().includes("preview"))) ||
        (tab === "attention" && ["attention", "warning"].includes(project.status));
      const matchesFramework = frameworkFilter === "all" || project.framework === frameworkFilter;
      const matchesHealth =
        healthFilter === "all" ||
        (healthFilter === "healthy" && project.status === "healthy") ||
        (healthFilter === "warning" && project.status === "warning") ||
        (healthFilter === "attention" && project.status === "attention");
      return matchesQuery && matchesTab && matchesFramework && matchesHealth;
    });

    const sorted = [...rows];
    if (sortKey === "name") sorted.sort((a, b) => a.name.localeCompare(b.name));
    if (sortKey === "readiness") sorted.sort((a, b) => b.readiness - a.readiness);
    if (sortKey === "attention") sorted.sort((a, b) => statusRank(b.status) - statusRank(a.status));
    return sorted;
  }, [dashboardProjects, frameworkFilter, healthFilter, search, sortKey, tab]);

  const hasFilters = search.length > 0 || tab !== "all" || frameworkFilter !== "all" || healthFilter !== "all";
  const clearFilters = () => {
    setSearch("");
    setTab("all");
    setFrameworkFilter("all");
    setHealthFilter("all");
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="Projects"
        description="Manage applications, environments, deploys, and project health."
        actions={
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" disabled={!workspaceId} onClick={() => setImportOpen(true)}>
              <GitFork className="h-4 w-4" aria-hidden="true" />
              Import from GitHub
            </Button>
            <Button size="sm" disabled={!workspaceId} onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              New project
            </Button>
          </div>
        }
      />

      <NewProjectDialog workspaceId={workspaceId} open={createOpen} onOpenChange={setCreateOpen} />
      <ImportFromGitHubDialog workspaceId={workspaceId} open={importOpen} onOpenChange={setImportOpen} />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Total projects" value={String(dashboardProjects.length)} detail={liveCount > 0 ? "Live workspace records" : "Prototype set"} icon={Box} intent="info" accentBar />
        <StatCard label="Production apps" value={String(productionCount)} detail="With a production environment" icon={Globe2} intent="success" accentBar />
        <StatCard label="Preview environments" value={String(previewCount)} detail="Across your projects" icon={GitBranch} intent="info" accentBar />
        <StatCard label="Need attention" value={String(attentionCount)} detail="Warnings to resolve" icon={AlertTriangle} intent="warning" accentBar />
      </div>

      <div className="space-y-3">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex min-w-0 flex-wrap gap-1.5">
            {[
              { id: "all", label: "All" },
              { id: "production", label: "Production" },
              { id: "preview", label: "Preview" },
              { id: "attention", label: "Needs attention", count: attentionCount },
            ].map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => setTab(t.id)}
                className={cn(
                  "inline-flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm font-medium transition",
                  tab === t.id ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )}
              >
                {t.label}
                {t.count !== undefined && t.count > 0 && (
                  <span className="grid h-5 min-w-5 place-items-center rounded-full bg-destructive/10 px-1 text-[11px] text-destructive">
                    {t.count}
                  </span>
                )}
              </button>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex min-w-0 flex-1 items-center gap-2 rounded-md border border-border bg-card px-3 shadow-sm sm:w-60 sm:flex-none">
              <Search className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search projects"
                className="h-9 min-w-0 flex-1 border-0 bg-transparent p-0 text-sm text-foreground outline-none placeholder:text-muted-foreground"
              />
            </label>
            <div className="inline-flex rounded-md border border-border bg-card p-1 shadow-sm">
              <button
                type="button"
                onClick={() => setView("grid")}
                className={cn("grid h-7 w-7 place-items-center rounded transition", view === "grid" ? "bg-accent text-accent-foreground" : "text-muted-foreground")}
                aria-label="Grid view"
              >
                <Grid2X2 className="h-4 w-4" aria-hidden="true" />
              </button>
              <button
                type="button"
                onClick={() => setView("list")}
                className={cn("grid h-7 w-7 place-items-center rounded transition", view === "list" ? "bg-accent text-accent-foreground" : "text-muted-foreground")}
                aria-label="List view"
              >
                <List className="h-4 w-4" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="grid gap-2 sm:grid-cols-2 sm:min-w-[360px]">
            <Select value={frameworkFilter} onChange={(e) => setFrameworkFilter(e.target.value)} aria-label="Framework filter">
              {frameworks.map((framework) => (
                <option key={framework} value={framework}>
                  {framework === "all" ? "All frameworks" : framework}
                </option>
              ))}
            </Select>
            <Select value={sortKey} onChange={(e) => setSortKey(e.target.value)} aria-label="Sort projects">
              <option value="newest">Sort: Last deploy</option>
              <option value="readiness">Sort: Readiness</option>
              <option value="attention">Sort: Needs attention</option>
              <option value="name">Sort: Name</option>
            </Select>
          </div>
          <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
            <span>
              {filtered.length} {filtered.length === 1 ? "project" : "projects"}
            </span>
            {hasFilters && (
              <button type="button" onClick={clearFilters} className="font-medium text-info hover:text-info/80">
                Clear filters
              </button>
            )}
          </div>
        </div>
      </div>

      {query.isLoading && (
        <div className="grid gap-4 xl:grid-cols-2 2xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-56 w-full" />
          ))}
        </div>
      )}
      {error && <EmptyState title="Project API returned an error" body={error} />}
      {!query.isLoading && !error && filtered.length === 0 && (
        <EmptyState
          title={dashboardProjects.length === 0 ? "No projects yet" : "No projects match these filters"}
          body={
            dashboardProjects.length === 0
              ? "Create your first project to deploy an app, or import one from GitHub."
              : "Clear filters or switch tabs to see the full project list."
          }
        />
      )}
      {!query.isLoading && !error && filtered.length > 0 && view === "grid" && (
        <div className="grid gap-4 xl:grid-cols-2 2xl:grid-cols-3">
          {filtered.map((project) => (
            <ProjectCard key={project.id} project={project} />
          ))}
        </div>
      )}
      {!query.isLoading && !error && filtered.length > 0 && view === "list" && (
        <div className="space-y-2">
          {filtered.map((project) => (
            <ProjectListRow key={project.id} project={project} />
          ))}
        </div>
      )}
    </div>
  );
}
