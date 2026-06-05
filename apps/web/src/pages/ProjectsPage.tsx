import { useQuery } from "@tanstack/react-query";
import { createClient } from "@connectrpc/connect";

import { ProjectService } from "../gen/controlplane/v1/projects_pb";
import { transport } from "../lib/connect";
import { useWorkspaceStore } from "../store";

const client = createClient(ProjectService, transport);

export function ProjectsPage() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);

  const { data, isLoading, error } = useQuery({
    queryKey: ["projects", workspaceId],
    queryFn: () => client.listProjectsByWorkspace({ workspaceId }),
    enabled: workspaceId.length > 0,
  });

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="text-2xl font-semibold">Projects</h1>
      <input
        className="mt-4 w-full rounded border border-gray-300 px-3 py-2"
        placeholder="Enter a workspace id to list its projects"
        value={workspaceId}
        onChange={(e) => setWorkspaceId(e.target.value)}
      />

      {isLoading && <p className="mt-4 text-gray-500">Loading…</p>}
      {error && <p className="mt-4 text-red-600">{error.message}</p>}

      <ul className="mt-4 space-y-2">
        {data?.projects.map((p) => (
          <li key={p.id} className="rounded border border-gray-200 p-3">
            <div className="font-medium">{p.name}</div>
            <div className="text-sm text-gray-500">{p.slug}</div>
          </li>
        ))}
      </ul>
    </main>
  );
}
