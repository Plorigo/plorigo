import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { GitFork, Globe, Lock } from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/cn";
import { deploymentClient, sourceClient } from "@/lib/clients";
import {
  useAgents,
  useBranches,
  useEnvironments,
  useGitHubConnection,
  useProjects,
  useRepositories,
  useServers,
} from "@/lib/queries";
import { pickDefaultServer, serverStatusLabel } from "@/lib/serverSelection";
import { deployTemplates, templateSourceKind } from "@/lib/templates";

// The three ways to start a deployment under a project. Image and image-based templates
// run immediately (the engine runs pre-built images). GitHub repos and repo-based
// templates connect the repository as the project's source — build & deploy from Git is a
// later slice, so until then connecting sets the project up and you deploy an image to run
// something now.
type Mode = "image" | "template" | "github";
type RepoMethod = "oauth" | "public";

const MODES: Array<{ value: Mode; label: string }> = [
  { value: "image", label: "Public image" },
  { value: "template", label: "Template" },
  { value: "github", label: "GitHub" },
];

// NewDeploymentDialog deploys under a project. Pick a source — a public image, a curated
// template, or a GitHub repository — then an environment and server (for image sources) or
// a branch (for repo sources). Mutations follow the imperative try/await/invalidate style
// the other dialogs use.
export function NewDeploymentDialog({
  workspaceId,
  open,
  onOpenChange,
  defaultProjectId,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProjectId?: string;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const projects = useProjects(workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);
  const connection = useGitHubConnection(workspaceId);
  const configured = connection.data?.configured ?? false;
  const connected = connection.data?.connected ?? false;

  // Selections use an override-or-default shape: an empty override means "follow the
  // computed default" (derived below from freshly loaded data), and the Select onChange
  // records an explicit user choice. This keeps defaults correct without syncing state in
  // through effects.
  const [mode, setMode] = useState<Mode>("image");
  const [projectOverride, setProjectOverride] = useState("");
  const [environmentOverride, setEnvironmentOverride] = useState("");
  const [serverOverride, setServerOverride] = useState("");
  // Image source.
  const [imageRef, setImageRef] = useState("traefik/whoami");
  const [containerPort, setContainerPort] = useState("80");
  // Template source.
  const [templateId, setTemplateId] = useState("");
  // GitHub source.
  const [repoMethodOverride, setRepoMethodOverride] = useState<RepoMethod | null>(null);
  const [repoFilter, setRepoFilter] = useState("");
  const [repoFullName, setRepoFullName] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [branch, setBranch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Default the project (preselected project wins, then the first available project).
  const projectId = projectOverride || defaultProjectId || (projects.data?.[0]?.id ?? "");

  // When OAuth isn't configured, fall back to the public-URL method.
  const repoMethod: RepoMethod =
    repoMethodOverride ?? (!connection.isLoading && !configured ? "public" : "oauth");

  const environments = useEnvironments(projectId);
  const repos = useRepositories(workspaceId, open && mode === "github" && repoMethod === "oauth" && connected);
  const selectedRepo = useMemo(
    () => repos.data?.find((r) => r.fullName === repoFullName),
    [repos.data, repoFullName],
  );
  const oauthBranches = useBranches(workspaceId, selectedRepo?.owner ?? "", selectedRepo?.name ?? "");

  const template = deployTemplates.find((t) => t.id === templateId);
  const templateIsImage = Boolean(template && templateSourceKind(template) === "image");
  // Image sources deploy now; repo sources connect (build-from-Git is a later slice).
  const isDeploy = mode === "image" || (mode === "template" && templateIsImage);

  // Keep the environment valid for the chosen project (default to its first).
  const envs = environments.data;
  const environmentId =
    envs?.length
      ? (environmentOverride && envs.some((e) => e.id === environmentOverride) ? environmentOverride : envs[0].id)
      : "";

  // Default the server to a ready one (then any online, then the first server).
  const serverId = serverOverride || pickDefaultServer(servers.data, agents.data)?.id || "";

  // OAuth: when a repo is picked, default the branch to its default. Tracking the previous
  // repo and adjusting during render keeps this out of an effect.
  const [prevSelectedRepo, setPrevSelectedRepo] = useState(selectedRepo);
  if (selectedRepo !== prevSelectedRepo) {
    setPrevSelectedRepo(selectedRepo);
    if (mode === "github" && repoMethod === "oauth" && selectedRepo) {
      setBranch(selectedRepo.defaultBranch || "");
    }
  }

  const filteredRepos = useMemo(() => {
    const q = repoFilter.trim().toLowerCase();
    const list = repos.data ?? [];
    if (!q) return list;
    return list.filter((r) => r.fullName.toLowerCase().includes(q));
  }, [repos.data, repoFilter]);

  function reset() {
    setBusy(false);
    setError("");
    // Clear the project override so reopening re-applies the preselected/default project.
    setProjectOverride("");
    setImageRef("traefik/whoami");
    setContainerPort("80");
    setTemplateId("");
    setRepoFilter("");
    setRepoFullName("");
    setRepoUrl("");
    setBranch("");
  }

  function startConnect() {
    window.location.assign(`/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}`);
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (!projectId) return setError("Pick a project");

    if (isDeploy) {
      const image = mode === "image" ? imageRef.trim() : (template?.imageRef ?? "");
      const port = mode === "image" ? Number(containerPort) : (template?.containerPort ?? 0);
      if (!environmentId) return setError("Pick an environment (add one on the project first)");
      if (!serverId) return setError("Pick a connected server");
      if (!image) return setError("Enter an image reference, e.g. traefik/whoami");
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        return setError("Container port must be between 1 and 65535");
      }
      setBusy(true);
      try {
        const { deployment } = await deploymentClient.createDeployment({
          environmentId,
          serverId,
          imageRef: image,
          containerPort: port,
        });
        if (!deployment) throw new Error("the deployment was not created");
        await queryClient.invalidateQueries({ queryKey: ["deployments"] });
        onOpenChange(false);
        reset();
        void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
      } catch (err) {
        setError(err instanceof ConnectError ? err.message : "Could not start the deployment");
        setBusy(false);
      }
      return;
    }

    // Connect a repository as the project's source (GitHub account, public URL, or a
    // repo-based template).
    setBusy(true);
    try {
      if (mode === "github" && repoMethod === "oauth") {
        if (!selectedRepo) throw new Error("Select a repository");
        await sourceClient.connectRepository({
          projectId,
          owner: selectedRepo.owner,
          repo: selectedRepo.name,
          branch,
        });
      } else {
        const url = mode === "github" ? repoUrl.trim() : (template?.repoUrl ?? "");
        if (!url) throw new Error("Enter a repository URL");
        await sourceClient.connectPublicRepository({ projectId, repoUrl: url, branch: branch.trim() });
      }
      await queryClient.invalidateQueries({ queryKey: ["projectSource", projectId] });
      await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
      onOpenChange(false);
      reset();
      toast.success("Repository connected");
      void navigate({ to: "/projects/$projectId", params: { projectId } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not connect the repository");
      setBusy(false);
    }
  }

  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;
  const noEnvironments = projectId.length > 0 && !environments.isLoading && (environments.data?.length ?? 0) === 0;

  // Whether the submit button is actionable.
  let canSubmit = Boolean(projectId) && !busy;
  if (mode === "template" && !template) canSubmit = false;
  if (isDeploy) {
    canSubmit = canSubmit && Boolean(environmentId) && Boolean(serverId) && !noServers;
  } else if (mode === "github" && repoMethod === "oauth") {
    canSubmit = canSubmit && Boolean(selectedRepo) && branch.length > 0;
  } else if (mode === "github") {
    canSubmit = canSubmit && repoUrl.trim().length > 0;
  }
  const submitLabel = isDeploy
    ? busy
      ? "Starting…"
      : "Deploy"
    : busy
      ? "Connecting…"
      : "Connect repository";

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
          <DialogTitle>New deployment</DialogTitle>
          <DialogDescription>
            {isDeploy
              ? "Run a container under this project. The server's agent pulls, starts, and health-checks it, then publishes it on a host port."
              : "Connect a Git repository as this project's source. Build & deploy from Git is coming — until then, connect here and deploy an image or template to run something now."}
          </DialogDescription>
        </DialogHeader>

        {/* Source selector */}
        <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
          {MODES.map((m) => (
            <button
              key={m.value}
              type="button"
              onClick={() => {
                setMode(m.value);
                setError("");
              }}
              className={cn(
                "rounded px-3 py-1 text-sm transition-colors",
                mode === m.value
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {m.label}
            </button>
          ))}
        </div>

        <form onSubmit={onSubmit} className="space-y-4">
          <Field label="Project">
            <Select value={projectId} onChange={(e) => setProjectOverride(e.target.value)}>
              {(projects.data ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
              {!projects.data?.length && <option value="">No projects</option>}
            </Select>
          </Field>

          {mode === "template" && (
            <Field label="Template">
              <div className="grid gap-2">
                {deployTemplates.map((t) => {
                  const isImg = templateSourceKind(t) === "image";
                  return (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => {
                        setTemplateId(t.id);
                        setBranch("");
                        setError("");
                      }}
                      className={cn(
                        "flex items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition",
                        templateId === t.id
                          ? "border-ring/60 bg-accent/50"
                          : "border-border hover:border-ring/40 hover:bg-accent/30",
                      )}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium text-foreground">{t.name}</span>
                          <Badge tone={isImg ? "green" : "blue"}>{isImg ? "image" : "git"}</Badge>
                        </div>
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">{t.description}</p>
                      </div>
                    </button>
                  );
                })}
              </div>
            </Field>
          )}

          {/* Deploy-source detail */}
          {mode === "image" && (
            <div className="grid grid-cols-[minmax(0,1fr)_120px] gap-3">
              <Field label="Image">
                <Input
                  value={imageRef}
                  onChange={(e) => setImageRef(e.target.value)}
                  placeholder="traefik/whoami"
                  spellCheck={false}
                  required
                />
              </Field>
              <Field label="Container port">
                <Input
                  value={containerPort}
                  onChange={(e) => setContainerPort(e.target.value)}
                  inputMode="numeric"
                  placeholder="80"
                  required
                />
              </Field>
            </div>
          )}

          {mode === "template" && template && templateIsImage && (
            <p className="text-xs text-muted-foreground">
              Deploys <code className="font-mono">{template.imageRef}</code> on port {template.containerPort}.
            </p>
          )}

          {/* Environment + server are needed only to actually run something. */}
          {isDeploy && (
            <>
              <Field label="Environment">
                <Select
                  value={environmentId}
                  onChange={(e) => setEnvironmentOverride(e.target.value)}
                  disabled={!environments.data?.length}
                >
                  {(environments.data ?? []).map((env) => (
                    <option key={env.id} value={env.id}>
                      {env.name} ({env.type})
                    </option>
                  ))}
                </Select>
                {noEnvironments && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    This project has no environments yet — add one on the project page first.
                  </p>
                )}
              </Field>

              <Field label="Server">
                <Select value={serverId} onChange={(e) => setServerOverride(e.target.value)} disabled={!servers.data?.length}>
                  {(servers.data ?? []).map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name} — {serverStatusLabel(s.id, agents.data)}
                    </option>
                  ))}
                </Select>
                {noServers && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    No servers connected — connect one on the Servers page first.
                  </p>
                )}
              </Field>
            </>
          )}

          {/* Repo-based template: just a branch (it connects, not deploys). */}
          {mode === "template" && template && !templateIsImage && (
            <Field label="Branch (optional)">
              <Input
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder={template.defaultBranch || "default branch"}
                autoCapitalize="none"
                spellCheck={false}
              />
            </Field>
          )}

          {/* GitHub source: a connected account repo, or a public URL. */}
          {mode === "github" && (
            <>
              <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
                {(["oauth", "public"] as const).map((rm) => (
                  <button
                    key={rm}
                    type="button"
                    onClick={() => {
                      setRepoMethodOverride(rm);
                      setError("");
                    }}
                    className={cn(
                      "rounded px-3 py-1 text-sm transition-colors",
                      repoMethod === rm
                        ? "bg-background font-medium text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {rm === "oauth" ? "GitHub account" : "Public URL"}
                  </button>
                ))}
              </div>

              {repoMethod === "oauth" ? (
                connection.isLoading ? (
                  <p className="text-sm text-muted-foreground">Checking GitHub connection…</p>
                ) : !configured ? (
                  <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                    GitHub OAuth isn't configured on this server. Set <code>GITHUB_OAUTH_CLIENT_ID</code> and{" "}
                    <code>GITHUB_OAUTH_CLIENT_SECRET</code> to enable it, or connect a public repository by URL.
                  </div>
                ) : !connected ? (
                  <div className="space-y-3">
                    <p className="text-sm text-muted-foreground">
                      Connect your GitHub account to choose a repository. You'll be redirected to GitHub to authorize,
                      then back here.
                    </p>
                    <Button type="button" onClick={startConnect} disabled={!workspaceId}>
                      <GitFork className="h-4 w-4" aria-hidden="true" />
                      Connect GitHub
                    </Button>
                  </div>
                ) : (
                  <>
                    <Field label="Repository">
                      <div className="space-y-2">
                        <Input
                          value={repoFilter}
                          onChange={(e) => setRepoFilter(e.target.value)}
                          placeholder="Filter repositories…"
                        />
                        <Select value={repoFullName} onChange={(e) => setRepoFullName(e.target.value)} aria-label="Repository">
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
                      </div>
                    </Field>
                    {selectedRepo && (
                      <Field label="Branch">
                        <Select value={branch} onChange={(e) => setBranch(e.target.value)} aria-label="Branch">
                          {oauthBranches.isLoading && <option value="">Loading branches…</option>}
                          {(oauthBranches.data ?? []).map((b) => (
                            <option key={b} value={b}>
                              {b}
                            </option>
                          ))}
                        </Select>
                      </Field>
                    )}
                    {selectedRepo?.isPrivate && (
                      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Lock className="h-3 w-3" aria-hidden="true" />
                        Private repository — Plorigo accesses it through your GitHub connection.
                      </p>
                    )}
                  </>
                )
              ) : (
                <>
                  <Field label="Public repository URL">
                    <Input
                      value={repoUrl}
                      onChange={(e) => setRepoUrl(e.target.value)}
                      placeholder="https://github.com/owner/repo"
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
                  <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Globe className="h-3 w-3" aria-hidden="true" />
                    The repository must be public — it's read without any credentials.
                  </p>
                </>
              )}
            </>
          )}

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="submit" disabled={!canSubmit}>
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
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
