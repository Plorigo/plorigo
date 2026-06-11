import { useEffect } from "react";
import { useParams } from "@tanstack/react-router";

import { useWorkspaceStore } from "@/store";
import { ProjectOverview } from "./ProjectOverview";

// The /projects/$projectId route. It renders the shared ProjectOverview and syncs the
// deep-linked project into the global filter so the sidebar switcher reflects it.
export function ProjectDetailPage() {
  const { projectId } = useParams({ strict: false }) as { projectId?: string };
  const storeProjectId = useWorkspaceStore((s) => s.projectId);
  const setProjectId = useWorkspaceStore((s) => s.setProjectId);

  // Sync the URL param into the store. Keyed on the param and idempotent, and it never
  // navigates — so there's no route<->store loop. It must depend on the param (not just
  // mount) because /projects/$a -> /projects/$b reuses this route without remounting.
  useEffect(() => {
    if (projectId && projectId !== storeProjectId) setProjectId(projectId);
  }, [projectId, storeProjectId, setProjectId]);

  return <ProjectOverview projectId={projectId ?? ""} embedded={false} />;
}
