import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { GitFork, Lock } from "lucide-react";
import { toast } from "sonner";

import { Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { environmentClient, projectClient, sourceClient } from "@/lib/clients";
import { useBranches, useGitHubConnection, useRepositories } from "@/lib/queries";

const ENV_TYPES = ["production", "staging", "preview", "custom"] as const;

// ImportFromGitHubDialog connects a project to a GitHub repository + branch. With no
// projectId it imports a new project (creates the project, connects the repo, and adds a
// first environment); with a projectId it connects the repo to that existing project.
export function ImportFromGitHubDialog({
  workspaceId,
  open,
  onOpenChange,
  projectId,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId?: string;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const connection = useGitHubConnection(workspaceId);
  const connected = connection.data?.connected ?? false;
  const configured = connection.data?.configured ?? false;

  const repos = useRepositories(workspaceId, open && connected);
  const [filter, setFilter] = useState("");
  const [repoFullName, setRepoFullName] = useState("");
  const selectedRepo = useMemo(
    () => repos.data?.find((r) => r.fullName === repoFullName),
    [repos.data, repoFullName],
  );

  const branches = useBranches(workspaceId, selectedRepo?.owner ?? "", selectedRepo?.name ?? "");
  const [branch, setBranch] = useState("");
  const [name, setName] = useState("");
  const [envName, setEnvName] = useState("Production");
  const [envType, setEnvType] = useState<string>("production");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const importMode = !projectId;

  function reset() {
    setFilter("");
    setRepoFullName("");
    setBranch("");
    setName("");
    setEnvName("Production");
    setEnvType("production");
    setBusy(false);
    setError("");
  }

  // When a repo is picked, default the branch to its default and prefill the name.
  useEffect(() => {
    if (!selectedRepo) return;
    setBranch(selectedRepo.defaultBranch || "");
    if (importMode) setName((prev) => prev || selectedRepo.name);
  }, [selectedRepo, importMode]);

  const filteredRepos = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const list = repos.data ?? [];
    if (!q) return list;
    return list.filter((r) => r.fullName.toLowerCase().includes(q));
  }, [repos.data, filter]);

  function startConnect() {
    window.location.assign(`/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}`);
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!selectedRepo || !branch) return;
    setBusy(true);
    setError("");

    // Connect-to-existing-project: a single connectRepository call.
    if (!importMode) {
      try {
        await sourceClient.connectRepository({ projectId, owner: selectedRepo.owner, repo: selectedRepo.name, branch });
        await queryClient.invalidateQueries({ queryKey: ["projectSource", projectId] });
        await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
        toast.success(`Connected ${selectedRepo.fullName}`);
        onOpenChange(false);
        reset();
      } catch (err) {
        setError(err instanceof ConnectError ? err.message : "Could not connect the repository");
        setBusy(false);
      }
      return;
    }

    // Import a new project: create it, connect the repo, then add a first environment.
    const projectName = name.trim();
    const environmentName = envName.trim();
    if (!projectName || !environmentName) {
      setBusy(false);
      return;
    }

    let newProjectId = "";
    try {
      const { project } = await projectClient.createProject({ workspaceId, name: projectName });
      if (!project) throw new Error("the project was not created");
      newProjectId = project.id;
      await queryClient.invalidateQueries({ queryKey: ["projects", workspaceId] });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not create the project");
      setBusy(false);
      return;
    }

    try {
      await sourceClient.connectRepository({ projectId: newProjectId, owner: selectedRepo.owner, repo: selectedRepo.name, branch });
      await queryClient.invalidateQueries({ queryKey: ["projectSource", newProjectId] });
      await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
    } catch (err) {
      const detail = err instanceof ConnectError ? err.message : "unknown error";
      toast.error(`Project created, but connecting the repo failed: ${detail}. Connect it from the project page.`);
      onOpenChange(false);
      reset();
      void navigate({ to: "/projects/$projectId", params: { projectId: newProjectId } });
      return;
    }

    try {
      await environmentClient.createEnvironment({ projectId: newProjectId, name: environmentName, type: envType });
      await queryClient.invalidateQueries({ queryKey: ["environments", newProjectId] });
    } catch (err) {
      const detail = err instanceof ConnectError ? err.message : "unknown error";
      toast.error(`Project created, but its environment failed: ${detail}. Add one from the project page.`);
      onOpenChange(false);
      reset();
      void navigate({ to: "/projects/$projectId", params: { projectId: newProjectId } });
      return;
    }

    toast.success(`Imported ${selectedRepo.fullName}`);
    onOpenChange(false);
    reset();
    void navigate({ to: "/projects/$projectId", params: { projectId: newProjectId } });
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) reset();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{importMode ? "Import from GitHub" : "Connect a repository"}</DialogTitle>
          <DialogDescription>
            {importMode
              ? "Connect a GitHub repository and branch as the source for a new project."
              : "Connect a GitHub repository and branch to this project."}
          </DialogDescription>
        </DialogHeader>

        {connection.isLoading ? (
          <p className="text-sm text-muted-foreground">Checking GitHub connection…</p>
        ) : !configured ? (
          <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
            GitHub OAuth isn't configured on this server. Set <code>GITHUB_OAUTH_CLIENT_ID</code> and{" "}
            <code>GITHUB_OAUTH_CLIENT_SECRET</code> to enable importing from GitHub.
          </div>
        ) : !connected ? (
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Connect your GitHub account to choose a repository. You'll be redirected to GitHub to authorize, then
              back here.
            </p>
            <Button type="button" onClick={startConnect} disabled={!workspaceId}>
              <GitFork className="h-4 w-4" aria-hidden="true" />
              Connect GitHub
            </Button>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="space-y-4">
            <Field label="Repository">
              <div className="space-y-2">
                <Input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter repositories…"
                  autoFocus
                />
                <Select
                  value={repoFullName}
                  onChange={(e) => setRepoFullName(e.target.value)}
                  aria-label="Repository"
                  required
                >
                  <option value="" disabled>
                    {repos.isLoading ? "Loading repositories…" : "Select a repository"}
                  </option>
                  {filteredRepos.map((r) => (
                    <option key={r.fullName} value={r.fullName}>
                      {r.fullName}
                      {r.isPrivate ? " (private)" : ""}
                    </option>
                  ))}
                </Select>
                {(repos.data?.length ?? 0) >= 100 && (
                  <p className="text-xs text-muted-foreground">
                    Showing the 100 most recently updated repositories. Use the filter to find others.
                  </p>
                )}
              </div>
            </Field>

            {selectedRepo && (
              <Field label="Branch">
                <Select value={branch} onChange={(e) => setBranch(e.target.value)} aria-label="Branch" required>
                  {branches.isLoading && <option value="">Loading branches…</option>}
                  {(branches.data ?? []).map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </Select>
              </Field>
            )}

            {importMode && selectedRepo && (
              <>
                <Field label="Project name">
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" required />
                </Field>
                <div className="grid grid-cols-[minmax(0,1fr)_140px] gap-3">
                  <Field label="First environment">
                    <Input value={envName} onChange={(e) => setEnvName(e.target.value)} required />
                  </Field>
                  <Field label="Type">
                    <Select value={envType} onChange={(e) => setEnvType(e.target.value)}>
                      {ENV_TYPES.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </div>
              </>
            )}

            {selectedRepo?.isPrivate && (
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Lock className="h-3 w-3" aria-hidden="true" />
                Private repository — Plorigo will access it through your GitHub connection.
              </p>
            )}

            {error && (
              <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {error}
              </p>
            )}

            <DialogFooter>
              <Button type="submit" disabled={busy || !selectedRepo || !branch || (importMode && !name.trim())}>
                {busy ? "Connecting..." : importMode ? "Import project" : "Connect repository"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm font-medium text-foreground">{label}</span>
      {children}
    </label>
  );
}
