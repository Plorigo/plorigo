import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { GitFork, Globe, Lock } from "lucide-react";
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
import { cn } from "@/lib/cn";
import { environmentClient, projectClient, sourceClient } from "@/lib/clients";
import { useBranches, useGitHubConnection, useRepositories } from "@/lib/queries";

const ENV_TYPES = ["production", "staging", "preview", "custom"] as const;

type Method = "oauth" | "public";

// ImportFromGitHubDialog connects a project to a Git repository + branch. The source can
// come from the workspace's connected GitHub account (OAuth) or from a public repository
// URL (no connection or credential). With no projectId it imports a new project (creates
// the project, connects the repo, and adds a first environment); with a projectId it
// connects the repo to that existing project.
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

  // An empty override means "follow the default": OAuth normally, but the public-URL method
  // once we know OAuth isn't configured. The method buttons record an explicit choice.
  const [methodOverride, setMethodOverride] = useState<Method | null>(null);
  const method: Method = methodOverride ?? (!connection.isLoading && !configured ? "public" : "oauth");

  // OAuth picker state.
  const repos = useRepositories(workspaceId, open && method === "oauth" && connected);
  const [filter, setFilter] = useState("");
  const [repoFullName, setRepoFullName] = useState("");
  const selectedRepo = useMemo(
    () => repos.data?.find((r) => r.fullName === repoFullName),
    [repos.data, repoFullName],
  );
  const branches = useBranches(workspaceId, selectedRepo?.owner ?? "", selectedRepo?.name ?? "");

  // Public-URL state.
  const [repoUrl, setRepoUrl] = useState("");

  // Shared.
  const [branch, setBranch] = useState("");
  const [name, setName] = useState("");
  const [envName, setEnvName] = useState("Production");
  const [envType, setEnvType] = useState<string>("production");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const importMode = !projectId;

  function reset() {
    setMethodOverride(null);
    setFilter("");
    setRepoFullName("");
    setRepoUrl("");
    setBranch("");
    setName("");
    setEnvName("Production");
    setEnvType("production");
    setBusy(false);
    setError("");
  }

  // Prefill defaults when the trigger changes, adjusting state during render (tracking the
  // previous value) instead of syncing through effects. OAuth: when a repo is picked,
  // default the branch and prefill the project name. Public: prefill the project name from
  // the URL's repo segment (best-effort; the server stays authoritative for the branch).
  const [prevSelectedRepo, setPrevSelectedRepo] = useState(selectedRepo);
  if (selectedRepo !== prevSelectedRepo) {
    setPrevSelectedRepo(selectedRepo);
    if (method === "oauth" && selectedRepo) {
      setBranch(selectedRepo.defaultBranch || "");
      if (importMode) setName((prev) => prev || selectedRepo.name);
    }
  }
  const [prevRepoUrl, setPrevRepoUrl] = useState(repoUrl);
  if (repoUrl !== prevRepoUrl) {
    setPrevRepoUrl(repoUrl);
    if (method === "public" && importMode) {
      const suggested = repoNameFromUrl(repoUrl);
      if (suggested) setName((prev) => prev || suggested);
    }
  }

  const filteredRepos = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const list = repos.data ?? [];
    if (!q) return list;
    return list.filter((r) => r.fullName.toLowerCase().includes(q));
  }, [repos.data, filter]);

  function startConnect() {
    window.location.assign(`/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}`);
  }

  // A friendly label for toasts: "owner/repo" for OAuth, the parsed pair for a public URL.
  const displayName =
    method === "public" ? ownerRepoFromUrl(repoUrl) || repoUrl.trim() : (selectedRepo?.fullName ?? "");

  // Run the right connect RPC for the chosen method.
  async function connectStep(targetProjectId: string) {
    if (method === "public") {
      await sourceClient.connectPublicRepository({
        projectId: targetProjectId,
        repoUrl: repoUrl.trim(),
        branch: branch.trim(),
      });
    } else {
      if (!selectedRepo) throw new Error("select a repository");
      await sourceClient.connectRepository({
        projectId: targetProjectId,
        owner: selectedRepo.owner,
        repo: selectedRepo.name,
        branch,
      });
    }
  }

  async function invalidateSource(targetProjectId: string) {
    await queryClient.invalidateQueries({ queryKey: ["projectSource", targetProjectId] });
    await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
  }

  const canConnect =
    method === "public" ? repoUrl.trim().length > 0 : Boolean(selectedRepo && branch);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!canConnect) return;
    setBusy(true);
    setError("");

    // Connect-to-existing-project: a single connect call.
    if (!importMode) {
      try {
        await connectStep(projectId);
        await invalidateSource(projectId);
        toast.success(`Connected ${displayName}`);
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

    let newProjectId: string;
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
      await connectStep(newProjectId);
      await invalidateSource(newProjectId);
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

    toast.success(`Imported ${displayName || projectName}`);
    onOpenChange(false);
    reset();
    void navigate({ to: "/projects/$projectId", params: { projectId: newProjectId } });
  }

  const submitDisabled = busy || !canConnect || (importMode && (!name.trim() || !workspaceId));
  const submitLabel = busy ? "Connecting..." : importMode ? "Import project" : "Connect repository";

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

        <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
          {(["oauth", "public"] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => {
                setMethodOverride(m);
                setError("");
              }}
              className={cn(
                "rounded px-3 py-1 text-sm transition-colors",
                method === m
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {m === "oauth" ? "GitHub account" : "Public URL"}
            </button>
          ))}
        </div>

        {method === "oauth" ? (
          connection.isLoading ? (
            <p className="text-sm text-muted-foreground">Checking GitHub connection…</p>
          ) : !configured ? (
            <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
              GitHub OAuth isn't configured on this server. Set <code>GITHUB_OAUTH_CLIENT_ID</code> and{" "}
              <code>GITHUB_OAUTH_CLIENT_SECRET</code> to enable it, or connect a public repository by URL above.
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
                <ImportFields
                  name={name}
                  setName={setName}
                  envName={envName}
                  setEnvName={setEnvName}
                  envType={envType}
                  setEnvType={setEnvType}
                />
              )}

              {selectedRepo?.isPrivate && (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Lock className="h-3 w-3" aria-hidden="true" />
                  Private repository — Plorigo will access it through your GitHub connection.
                </p>
              )}

              {error && <ErrorBox>{error}</ErrorBox>}

              <DialogFooter>
                <Button type="submit" disabled={submitDisabled}>
                  {submitLabel}
                </Button>
              </DialogFooter>
            </form>
          )
        ) : (
          <form onSubmit={onSubmit} className="space-y-4">
            <Field label="Public repository URL">
              <Input
                value={repoUrl}
                onChange={(e) => setRepoUrl(e.target.value)}
                placeholder="https://github.com/owner/repo"
                autoFocus
                inputMode="url"
                autoCapitalize="none"
                spellCheck={false}
                required
              />
            </Field>

            <Field label="Branch (optional)">
              <Input
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="default branch"
                autoCapitalize="none"
                spellCheck={false}
              />
            </Field>

            {importMode && repoUrl.trim() && (
              <ImportFields
                name={name}
                setName={setName}
                envName={envName}
                setEnvName={setEnvName}
                envType={envType}
                setEnvType={setEnvType}
              />
            )}

            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Globe className="h-3 w-3" aria-hidden="true" />
              The repository must be public — it's read without any credentials.
            </p>

            {error && <ErrorBox>{error}</ErrorBox>}

            <DialogFooter>
              <Button type="submit" disabled={submitDisabled}>
                {submitLabel}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ImportFields({
  name,
  setName,
  envName,
  setEnvName,
  envType,
  setEnvType,
}: {
  name: string;
  setName: (v: string) => void;
  envName: string;
  setEnvName: (v: string) => void;
  envType: string;
  setEnvType: (v: string) => void;
}) {
  return (
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

function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {children}
    </p>
  );
}

// ownerRepoFromUrl extracts "owner/repo" from a public GitHub reference for display; it
// mirrors the server's parse loosely and returns "" when the input isn't yet a repo.
function ownerRepoFromUrl(raw: string): string {
  let s = raw.trim();
  if (!s) return "";
  if (s.startsWith("git@github.com:")) {
    s = s.slice("git@github.com:".length);
  } else {
    s = s.replace(/^https?:\/\//, "").replace(/^www\./, "").replace(/^github\.com\//, "");
  }
  const parts = s.replace(/^\/+/, "").split("/").filter(Boolean);
  if (parts.length < 2) return "";
  return `${parts[0]}/${parts[1].replace(/\.git$/, "")}`;
}

function repoNameFromUrl(raw: string): string {
  const pair = ownerRepoFromUrl(raw);
  return pair ? pair.split("/")[1] : "";
}
