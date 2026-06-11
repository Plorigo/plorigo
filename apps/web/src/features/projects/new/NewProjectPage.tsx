import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, FolderPlus } from "lucide-react";
import { toast } from "sonner";

import { Button, Input, Panel } from "@/components/ui";
import { environmentClient, projectClient } from "@/lib/clients";
import { useWorkspaceStore } from "@/store";

// The environments every new project starts with. A project is just a named container —
// it's created empty (no deployment); you deploy under it afterward from the project page.
const DEFAULT_ENVIRONMENTS = [
  { name: "Production", type: "production" },
  { name: "Preview", type: "preview" },
];

// NewProjectPage creates a project by name only, seeds its default environments, and lands
// on the project page — where deployments are added. No deploy, repo, or server is required
// at creation, so projects stay easy to create and organize.
export function NewProjectPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const projectName = name.trim();
    if (!projectName) return setError("Enter a project name");
    if (!workspaceId) return setError("Select a workspace first");
    setBusy(true);
    setError("");

    let projectId = "";
    try {
      const { project } = await projectClient.createProject({ workspaceId, name: projectName });
      if (!project) throw new Error("the project was not created");
      projectId = project.id;
      await queryClient.invalidateQueries({ queryKey: ["projects", workspaceId] });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not create the project");
      setBusy(false);
      return;
    }

    // Seed the default environments. If one fails, the project still exists — land on it and
    // say what happened (an environment can be added from the project page).
    try {
      for (const env of DEFAULT_ENVIRONMENTS) {
        await environmentClient.createEnvironment({ projectId, name: env.name, type: env.type });
      }
      await queryClient.invalidateQueries({ queryKey: ["environments", projectId] });
    } catch (err) {
      const detail = err instanceof ConnectError ? err.message : "unknown error";
      toast.error(`Project created, but a default environment failed: ${detail}. Add one from the project page.`);
      void navigate({ to: "/projects/$projectId", params: { projectId } });
      return;
    }

    toast.success(`Project ${projectName} created`);
    void navigate({ to: "/projects/$projectId", params: { projectId } });
  }

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <Link to="/projects" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to projects
      </Link>

      <Panel className="p-6">
        <div className="mb-5 flex items-start gap-3">
          <span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-border bg-card text-foreground shadow-sm">
            <FolderPlus className="h-6 w-6" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h1 className="text-lg font-semibold text-foreground">Create a project</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              A project is a named container for your app. It starts empty with Production and
              Preview environments — deploy whatever you want under it afterward.
            </p>
          </div>
        </div>

        <form onSubmit={onSubmit} className="space-y-4">
          <label className="block">
            <span className="mb-1.5 block text-sm font-medium text-foreground">Project name</span>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="plorigo"
              autoFocus
              required
            />
          </label>

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => navigate({ to: "/projects" })}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || !workspaceId}>
              {busy ? "Creating…" : "Create project"}
            </Button>
          </div>
        </form>
      </Panel>
    </div>
  );
}
