import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { authClient, projectClient, workspaceClient } from "../lib/clients";
import { useCurrentUser } from "../lib/auth";
import { useWorkspaceStore } from "../store";

export function ProjectsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: user } = useCurrentUser();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);

  const workspaces = useQuery({
    queryKey: ["workspaces"],
    queryFn: async () => (await workspaceClient.listMyWorkspaces({})).workspaces,
  });

  // Keep the selection valid: default to the first workspace, and reset whenever the
  // current id isn't one of the user's — e.g. a stale id left in the store by a previous
  // account in the same SPA session.
  useEffect(() => {
    if (workspaces.data && workspaces.data.length > 0) {
      if (!workspaces.data.some((w) => w.id === workspaceId)) {
        setWorkspaceId(workspaces.data[0].id);
      }
    }
  }, [workspaces.data, workspaceId, setWorkspaceId]);

  const projects = useQuery({
    queryKey: ["projects", workspaceId],
    queryFn: async () =>
      (await projectClient.listProjectsByWorkspace({ workspaceId })).projects,
    enabled: workspaceId.length > 0,
  });

  async function logout() {
    await authClient.logout({});
    setWorkspaceId(""); // don't let the next user inherit this selection
    await queryClient.invalidateQueries();
    await navigate({ to: "/login" });
  }

  return (
    <main className="mx-auto max-w-3xl p-8">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Projects</h1>
        <div className="flex items-center gap-3 text-sm">
          <span className="text-gray-500">{user?.email}</span>
          <button onClick={logout} className="rounded border border-gray-300 px-3 py-1">
            Log out
          </button>
        </div>
      </header>

      <label className="mt-6 block text-sm font-medium text-gray-700">Workspace</label>
      <select
        className="mt-1 w-full rounded border border-gray-300 px-3 py-2"
        value={workspaceId}
        onChange={(e) => setWorkspaceId(e.target.value)}
      >
        {workspaces.data?.map((w) => (
          <option key={w.id} value={w.id}>
            {w.name}
          </option>
        ))}
      </select>

      {projects.isLoading && <p className="mt-4 text-gray-500">Loading…</p>}
      {projects.error && <p className="mt-4 text-red-600">{projects.error.message}</p>}

      <ul className="mt-4 space-y-2">
        {projects.data?.map((p) => (
          <li key={p.id} className="rounded border border-gray-200 p-3">
            <div className="font-medium">{p.name}</div>
            <div className="text-sm text-gray-500">{p.slug}</div>
          </li>
        ))}
      </ul>
    </main>
  );
}
