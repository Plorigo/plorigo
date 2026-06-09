import { useMemo } from "react";

import { useDemoData } from "@/lib/demo";
import { formatDate } from "@/lib/format";
import { prototypeProjects, type DashboardProject } from "@/lib/mockDashboard";
import { useProjects } from "@/lib/queries";
import { type Tone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

// Tints a framework label so cards read at a glance.
export function frameworkTone(framework: string): Tone {
  if (["Next.js", "Express"].includes(framework)) return "neutral";
  if (["FastAPI", "Python"].includes(framework)) return "blue";
  if (framework === "Go") return "green";
  return "purple";
}

interface LiveProject {
  id: string;
  name: string;
  slug: string;
  createdAt: string;
}

// Live backend projects don't yet carry the rich UI fields (framework, services,
// readiness…), so we project them into the DashboardProject shape with sensible
// placeholders. When there are no live projects and demo mode is on, we show the
// prototype fixtures instead.
function toDashboardProject(project: LiveProject): DashboardProject {
  return {
    id: project.id,
    name: project.name,
    slug: project.slug,
    repo: "Backend project record",
    framework: "ConnectRPC",
    url: `${project.slug}.plorigo.local`,
    branch: "main",
    commit: project.id.slice(0, 7) || "live",
    status: "healthy",
    source: "live",
    updated: `created ${formatDate(project.createdAt)}`,
    environments: [{ name: "Production", tone: "green" }],
    services: ["Web", "DB"],
    owner: "Workspace",
    readiness: 91,
    collaborators: 1,
    sparkline: [20, 28, 34, 30, 42, 38, 46, 40, 48],
    kind: "web",
  };
}

export function useDashboardProjects() {
  const demo = useDemoData();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const projects = useProjects(workspaceId);

  const dashboardProjects = useMemo<DashboardProject[]>(() => {
    const live = projects.data ?? [];
    if (live.length > 0) {
      return live.map(toDashboardProject);
    }
    return demo ? prototypeProjects : [];
  }, [projects.data, demo]);

  return { query: projects, dashboardProjects, liveCount: projects.data?.length ?? 0 };
}
