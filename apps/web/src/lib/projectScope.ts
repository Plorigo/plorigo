import { useEffect } from "react";
import { useParams } from "@tanstack/react-router";

import { useWorkspaceStore } from "@/store";

// useEffectiveProjectId keeps project-scoped routes refresh-safe. If the URL contains
// /projects/$projectId, that route param is the source of truth and is synced back into
// the sidebar selector. Otherwise pages fall back to the in-memory project filter.
export function useEffectiveProjectId(): string {
  const { projectId: routeProjectId } = useParams({ strict: false }) as { projectId?: string };
  const storeProjectId = useWorkspaceStore((s) => s.projectId);
  const setProjectId = useWorkspaceStore((s) => s.setProjectId);

  useEffect(() => {
    if (routeProjectId && routeProjectId !== storeProjectId) setProjectId(routeProjectId);
  }, [routeProjectId, storeProjectId, setProjectId]);

  return routeProjectId || storeProjectId;
}
