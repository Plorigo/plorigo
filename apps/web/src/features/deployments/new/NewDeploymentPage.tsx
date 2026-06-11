import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowLeft,
  Container,
  FlaskConical,
  FolderGit2,
  GitBranch,
  GitFork,
  Globe,
  Layers,
  Lock,
  Network,
  Rocket,
  Search,
  Server,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton } from "@/components/ui";
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
import { deployTemplates, templateSourceKind, type DeployTemplate, type TemplateCategory } from "@/lib/templates";
import { type Intent, intentSoft } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

// Per-template icon + tint so the Templates gallery reads at a glance; falls back by
// category for any template that doesn't have its own entry. Visual only — the template
// catalog itself stays pure data in lib/templates.ts.
const TEMPLATE_ICONS: Record<string, { icon: LucideIcon; intent: Intent }> = {
  whoami: { icon: Network, intent: "violet" },
  "nginx-hello": { icon: Globe, intent: "info" },
  httpbin: { icon: FlaskConical, intent: "success" },
  "welcome-to-docker": { icon: Container, intent: "warning" },
};
const CATEGORY_ICONS: Record<TemplateCategory, { icon: LucideIcon; intent: Intent }> = {
  Starter: { icon: Sparkles, intent: "violet" },
  Web: { icon: Globe, intent: "info" },
  API: { icon: FlaskConical, intent: "success" },
};
function templateVisual(t: DeployTemplate) {
  return TEMPLATE_ICONS[t.id] ?? CATEGORY_ICONS[t.category];
}

// NewDeploymentPage is the full-page "what do you want to deploy?" picker (Vercel's New
// Project, adapted for BYOS). A "Deploy to" bar fixes the target (project + environment +
// server); below it a quick image/URL box and two columns — Import Git Repository (the
// connected account's repos) and Templates — each item carrying its own Deploy/Connect
// action. Image sources run now; Git sources connect as the project's source (build & deploy
// from Git is a later slice). An optional ?project= preselects the target and scopes "Back".
export function NewDeploymentPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const storeProjectId = useWorkspaceStore((s) => s.projectId);
  const search = useSearch({ strict: false }) as { project?: string };
  // Where to preselect/scope: an explicit ?project= wins, then the active dashboard filter,
  // then the first project. Only ?project= drives the back link (deterministic on refresh).
  const preferredProject = search.project ?? (storeProjectId || undefined);

  const projects = useProjects(workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);
  const connection = useGitHubConnection(workspaceId);
  const configured = connection.data?.configured ?? false;
  const connected = connection.data?.connected ?? false;
  const githubLogin = connection.data?.connection?.githubLogin ?? "";

  // Target.
  const [projectId, setProjectId] = useState("");
  const [environmentId, setEnvironmentId] = useState("");
  const [serverId, setServerId] = useState("");
  // Quick deploy (a public image, or a public Git URL).
  const [quickValue, setQuickValue] = useState("");
  const [quickPort, setQuickPort] = useState("80");
  // Import-a-repo (the connected account).
  const [repoFilter, setRepoFilter] = useState("");
  const [selectedRepoFullName, setSelectedRepoFullName] = useState("");
  const [repoBranch, setRepoBranch] = useState("");
  // Templates.
  const [templateFilter, setTemplateFilter] = useState("");
  // Shared action state — busyKey names the item being acted on so only its button spins.
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [error, setError] = useState("");
  const busy = busyKey !== null;

  const environments = useEnvironments(projectId);
  const repos = useRepositories(workspaceId, connected);
  const selectedRepo = useMemo(
    () => repos.data?.find((r) => r.fullName === selectedRepoFullName),
    [repos.data, selectedRepoFullName],
  );
  const branches = useBranches(workspaceId, selectedRepo?.owner ?? "", selectedRepo?.name ?? "");

  // Surface the GitHub OAuth outcome (?github=connected|error) on return, refresh the
  // connection so the repo list appears, then strip the params so it doesn't repeat.
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

  // Default the project: a valid current selection wins, then ?project= / the active filter,
  // then the first project (guarding against a stale or foreign ?project= id).
  useEffect(() => {
    const list = projects.data;
    if (!list?.length) return;
    setProjectId((cur) => {
      if (cur && list.some((p) => p.id === cur)) return cur;
      if (preferredProject && list.some((p) => p.id === preferredProject)) return preferredProject;
      return list[0].id;
    });
  }, [projects.data, preferredProject]);

  // Keep the environment valid for the chosen project (default to its first).
  useEffect(() => {
    const envs = environments.data;
    if (!envs?.length) {
      setEnvironmentId("");
      return;
    }
    setEnvironmentId((cur) => (cur && envs.some((e) => e.id === cur) ? cur : envs[0].id));
  }, [environments.data]);

  // Default the server to a ready one (then any online, then the first server).
  useEffect(() => {
    if (serverId) return;
    const def = pickDefaultServer(servers.data, agents.data);
    if (def) setServerId(def.id);
  }, [servers.data, agents.data, serverId]);

  // When a repo is expanded, default its branch to the repo's default.
  useEffect(() => {
    if (!selectedRepo) return;
    setRepoBranch(selectedRepo.defaultBranch || "");
  }, [selectedRepo]);

  const filteredRepos = useMemo(() => {
    const q = repoFilter.trim().toLowerCase();
    const list = repos.data ?? [];
    if (!q) return list;
    return list.filter((r) => r.fullName.toLowerCase().includes(q));
  }, [repos.data, repoFilter]);

  const filteredTemplates = useMemo(() => {
    const q = templateFilter.trim().toLowerCase();
    if (!q) return deployTemplates;
    return deployTemplates.filter((t) =>
      `${t.name} ${t.description} ${t.category}`.toLowerCase().includes(q),
    );
  }, [templateFilter]);

  const noProjects = !projects.isLoading && (projects.data?.length ?? 0) === 0;
  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;
  const noEnvironments = projectId.length > 0 && !environments.isLoading && (environments.data?.length ?? 0) === 0;
  const projectName = (projects.data ?? []).find((p) => p.id === projectId)?.name ?? "this project";
  // An image deploy needs a full target; connecting a repo just needs a project.
  const canDeploy = Boolean(projectId) && Boolean(environmentId) && Boolean(serverId) && !noServers;
  const canConnect = Boolean(projectId);

  function startConnect() {
    // Return to this import flow after GitHub authorizes — the OAuth handler defaults to
    // /projects otherwise. The server's safeReturnPath strips any query, so we send a clean
    // path; the page re-resolves the target project from the active scope on return.
    const returnTo = encodeURIComponent("/deployments/new");
    window.location.assign(
      `/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}&return_to=${returnTo}`,
    );
  }

  async function invalidateSource() {
    await queryClient.invalidateQueries({ queryKey: ["projectSource", projectId] });
    await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
  }

  // Run a pre-built image as a container under the chosen environment + server.
  async function deployImage(imageRef: string, port: number, key: string) {
    setError("");
    if (!projectId) return setError("Pick a project to deploy to");
    if (!environmentId) return setError("Pick an environment (add one on the project first)");
    if (!serverId) return setError("Pick a connected server");
    const ref = imageRef.trim();
    if (!ref) return setError("Enter an image reference, e.g. traefik/whoami");
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return setError("Container port must be between 1 and 65535");
    }
    setBusyKey(key);
    try {
      const { deployment } = await deploymentClient.createDeployment({
        environmentId,
        serverId,
        imageRef: ref,
        containerPort: port,
      });
      if (!deployment) throw new Error("the deployment was not created");
      await queryClient.invalidateQueries({ queryKey: ["deployments"] });
      void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not start the deployment");
      setBusyKey(null);
    }
  }

  // Connect a repository as the project's source. Build-from-Git is a later slice — for now
  // this sets the project up and you deploy an image to run something.
  async function connectOAuthRepo(owner: string, repo: string, branch: string, key: string) {
    setError("");
    if (!projectId) return setError("Pick a project to connect to");
    setBusyKey(key);
    try {
      await sourceClient.connectRepository({ projectId, owner, repo, branch });
      await invalidateSource();
      toast.success("Repository connected");
      void navigate({ to: "/projects/$projectId", params: { projectId } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not connect the repository");
      setBusyKey(null);
    }
  }

  async function connectPublicUrl(repoUrl: string, branch: string, key: string) {
    setError("");
    if (!projectId) return setError("Pick a project to connect to");
    const url = repoUrl.trim();
    if (!url) return setError("Enter a repository URL");
    setBusyKey(key);
    try {
      await sourceClient.connectPublicRepository({ projectId, repoUrl: url, branch: branch.trim() });
      await invalidateSource();
      toast.success("Repository connected");
      void navigate({ to: "/projects/$projectId", params: { projectId } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not connect the repository");
      setBusyKey(null);
    }
  }

  // The quick box deploys an image unless it clearly looks like a Git URL.
  const quickIsRepo = looksLikeGitUrl(quickValue);
  const quickReady =
    quickValue.trim().length > 0 && (quickIsRepo ? canConnect : canDeploy && Number(quickPort) >= 1);
  function runQuick() {
    if (quickIsRepo) void connectPublicUrl(quickValue, "", "quick");
    else void deployImage(quickValue, Number(quickPort), "quick");
  }

  function runTemplate(t: DeployTemplate) {
    if (templateSourceKind(t) === "image") void deployImage(t.imageRef ?? "", t.containerPort, `tpl:${t.id}`);
    else void connectPublicUrl(t.repoUrl ?? "", t.defaultBranch ?? "", `tpl:${t.id}`);
  }

  return (
    <div className="mx-auto w-full max-w-6xl space-y-6">
      {search.project ? (
        <Link to="/projects/$projectId" params={{ projectId: search.project }} className={BACK_LINK_CLS}>
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Back to project
        </Link>
      ) : (
        <Link to="/deployments" className={BACK_LINK_CLS}>
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Back to deployments
        </Link>
      )}

      <div className="flex items-start gap-3">
        <span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-border bg-card text-foreground shadow-sm">
          <Rocket className="h-6 w-6" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">Deploy something new</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Run a public image now, or connect a Git repository as the project's source. Pick where it lands, then
            choose what to deploy.
          </p>
        </div>
      </div>

      {!workspaceId ? (
        <EmptyState
          title="Select a workspace first"
          body="Choose a workspace to deploy under, then come back to start a deployment."
        />
      ) : (
        <div className="space-y-6">
          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          {/* Deploy target — where everything below lands. */}
          <Panel>
            <PanelHeader
              title="Deploy to"
              description="Environment and server apply to image deploys; connecting a repo just needs a project."
            />
            <div className="grid gap-4 p-4 sm:grid-cols-3">
              <Field label="Project" icon={FolderGit2}>
                <Select value={projectId} onChange={(e) => setProjectId(e.target.value)}>
                  {(projects.data ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                  {noProjects && <option value="">No projects</option>}
                </Select>
                {noProjects && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    No projects yet —{" "}
                    <Link to="/projects/new" className="font-medium text-foreground hover:underline">
                      create one
                    </Link>{" "}
                    first.
                  </p>
                )}
              </Field>
              <Field label="Environment" icon={Layers}>
                <Select
                  value={environmentId}
                  onChange={(e) => setEnvironmentId(e.target.value)}
                  disabled={!environments.data?.length}
                >
                  {(environments.data ?? []).map((env) => (
                    <option key={env.id} value={env.id}>
                      {env.name} ({env.type})
                    </option>
                  ))}
                </Select>
                {noEnvironments && (
                  <p className="mt-1 text-xs text-muted-foreground">No environments yet — add one on the project page.</p>
                )}
              </Field>
              <Field label="Server" icon={Server}>
                <Select value={serverId} onChange={(e) => setServerId(e.target.value)} disabled={!servers.data?.length}>
                  {(servers.data ?? []).map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name} — {serverStatusLabel(s.id, agents.data)}
                    </option>
                  ))}
                </Select>
                {noServers && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    No servers —{" "}
                    <Link to="/servers" className="font-medium text-foreground hover:underline">
                      connect one
                    </Link>
                    .
                  </p>
                )}
              </Field>
            </div>
          </Panel>

          {/* Quick deploy — a public image (with port), or paste a public Git URL. */}
          <Panel className="p-4 sm:p-5">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
              <div className="min-w-0 flex-1">
                <span className="mb-1.5 block text-sm font-medium text-foreground">Public image or Git URL</span>
                <div className="relative">
                  {quickIsRepo ? (
                    <GitFork className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
                  ) : (
                    <Container className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
                  )}
                  <Input
                    value={quickValue}
                    onChange={(e) => setQuickValue(e.target.value)}
                    placeholder="traefik/whoami  ·  or  https://github.com/owner/repo"
                    className="pl-9"
                    autoCapitalize="none"
                    spellCheck={false}
                    autoFocus
                  />
                </div>
              </div>
              {!quickIsRepo && (
                <div className="w-full sm:w-28">
                  <span className="mb-1.5 block text-sm font-medium text-foreground">Port</span>
                  <Input
                    value={quickPort}
                    onChange={(e) => setQuickPort(e.target.value)}
                    inputMode="numeric"
                    placeholder="80"
                  />
                </div>
              )}
              <Button onClick={runQuick} disabled={busy || !quickReady}>
                {quickIsRepo ? <GitFork className="h-4 w-4" aria-hidden="true" /> : <Rocket className="h-4 w-4" aria-hidden="true" />}
                {busyKey === "quick" ? "Working…" : quickIsRepo ? "Connect" : "Deploy"}
              </Button>
            </div>
            {quickValue.trim() && (
              <p className="mt-2 text-xs text-muted-foreground">
                {quickIsRepo
                  ? `Looks like a Git repository — connects it to ${projectName}.`
                  : `Deploys this image to ${projectName} on port ${quickPort || "…"}.`}
              </p>
            )}
          </Panel>

          <div className="grid gap-6 lg:grid-cols-2">
            {/* Import Git Repository — the connected account's repos. */}
            <Panel className="flex flex-col overflow-hidden">
              <SectionHeader
                icon={GitFork}
                title="Import Git Repository"
                subtitle={connected && githubLogin ? `Connected as ${githubLogin}` : "Connect GitHub to import your repos."}
                count={connected ? filteredRepos.length : undefined}
              >
                {connected && (
                  <SearchInput value={repoFilter} onChange={setRepoFilter} placeholder="Search repositories…" />
                )}
              </SectionHeader>

              {!configured && !connection.isLoading ? (
                <div className="p-4 text-sm text-muted-foreground">
                  GitHub OAuth isn't configured on this server. Set <code>GITHUB_OAUTH_CLIENT_ID</code> and{" "}
                  <code>GITHUB_OAUTH_CLIENT_SECRET</code> to import account repos, or paste a public repository URL in
                  the box above.
                </div>
              ) : connection.isLoading ? (
                <ListSkeleton />
              ) : !connected ? (
                <div className="space-y-3 p-4">
                  <p className="text-sm text-muted-foreground">
                    Connect your GitHub account to import a repository. You'll be redirected to GitHub to authorize,
                    then back here.
                  </p>
                  <Button type="button" onClick={startConnect} disabled={!workspaceId}>
                    <GitFork className="h-4 w-4" aria-hidden="true" />
                    Connect GitHub
                  </Button>
                </div>
              ) : repos.isLoading ? (
                <ListSkeleton />
              ) : filteredRepos.length === 0 ? (
                <p className="p-4 text-sm text-muted-foreground">
                  {repos.data?.length ? "No repositories match your search." : "No repositories found on this account."}
                </p>
              ) : (
                <div className="max-h-[24rem] divide-y divide-border overflow-y-auto">
                  {filteredRepos.map((r) => {
                    const isSel = selectedRepoFullName === r.fullName;
                    return (
                      <div key={r.fullName} className={cn("px-4 py-2.5 transition-colors", isSel ? "bg-accent/50" : "hover:bg-accent/30")}>
                        <div className="flex items-center justify-between gap-3">
                          <button
                            type="button"
                            onClick={() => {
                              setError("");
                              setSelectedRepoFullName((cur) => (cur === r.fullName ? "" : r.fullName));
                            }}
                            className="flex min-w-0 items-center gap-2.5 text-left"
                          >
                            <span className="grid h-7 w-7 shrink-0 place-items-center rounded-md border border-border bg-muted text-muted-foreground">
                              <GitFork className="h-3.5 w-3.5" aria-hidden="true" />
                            </span>
                            <span className="min-w-0">
                              <span className="flex items-center gap-1.5">
                                <span className="truncate text-sm font-medium text-foreground">{r.fullName}</span>
                                {r.isPrivate && <Lock className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden="true" />}
                              </span>
                              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                                <GitBranch className="h-3 w-3" aria-hidden="true" />
                                {r.defaultBranch || "default"}
                              </span>
                            </span>
                          </button>
                          {!isSel && (
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={busy || !canConnect}
                              onClick={() => {
                                setError("");
                                setSelectedRepoFullName(r.fullName);
                              }}
                            >
                              Import
                            </Button>
                          )}
                        </div>
                        {isSel && (
                          <div className="mt-3 flex items-end gap-2">
                            <div className="min-w-0 flex-1">
                              <span className="mb-1.5 block text-xs font-medium text-foreground">Branch</span>
                              <Select value={repoBranch} onChange={(e) => setRepoBranch(e.target.value)} aria-label="Branch">
                                {branches.isLoading && <option value="">Loading branches…</option>}
                                {(branches.data ?? []).map((b) => (
                                  <option key={b} value={b}>
                                    {b}
                                  </option>
                                ))}
                              </Select>
                            </div>
                            <Button
                              size="sm"
                              disabled={busy || !canConnect || !repoBranch}
                              onClick={() => connectOAuthRepo(r.owner, r.name, repoBranch, `repo:${r.fullName}`)}
                            >
                              {busyKey === `repo:${r.fullName}` ? "Connecting…" : "Connect"}
                            </Button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
              {connected && (repos.data?.length ?? 0) >= 100 && (
                <p className="border-t border-border p-3 text-xs text-muted-foreground">
                  Showing the 100 most recently updated repositories. Use search to find others.
                </p>
              )}
            </Panel>

            {/* Templates — start from a ready-made image or repo. */}
            <Panel className="flex flex-col overflow-hidden">
              <SectionHeader
                icon={Sparkles}
                title="Templates"
                subtitle="Start from a ready-made image or repo."
                count={filteredTemplates.length}
              >
                <SearchInput value={templateFilter} onChange={setTemplateFilter} placeholder="Search templates…" />
              </SectionHeader>
              {filteredTemplates.length === 0 ? (
                <p className="p-4 text-sm text-muted-foreground">No templates match your search.</p>
              ) : (
                <div className="max-h-[24rem] overflow-y-auto">
                  <div className="grid gap-3 p-4 sm:grid-cols-2">
                    {filteredTemplates.map((t) => {
                      const isImg = templateSourceKind(t) === "image";
                      const key = `tpl:${t.id}`;
                      const { icon: Icon, intent } = templateVisual(t);
                      const detail = isImg
                        ? `${t.imageRef} · :${t.containerPort}`
                        : `${shortRepo(t.repoUrl ?? "")} · ${t.defaultBranch || "default"}`;
                      return (
                        <div
                          key={t.id}
                          className="flex flex-col rounded-lg border border-border p-3 transition hover:border-ring/40 hover:shadow-card-hover"
                        >
                          <div className="flex items-start gap-2.5">
                            <span className={cn("grid h-9 w-9 shrink-0 place-items-center rounded-lg border", intentSoft[intent])}>
                              <Icon className="h-5 w-5" aria-hidden="true" />
                            </span>
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="truncate text-sm font-medium text-foreground">{t.name}</span>
                                <Badge tone={isImg ? "green" : "blue"}>{isImg ? "image" : "git"}</Badge>
                              </div>
                              <p className="text-[11px] uppercase tracking-wide text-muted-foreground">{t.category}</p>
                            </div>
                          </div>
                          <p className="mt-2 line-clamp-2 flex-1 text-xs leading-5 text-muted-foreground">{t.description}</p>
                          <p className="mt-2 truncate font-mono text-[11px] text-muted-foreground">{detail}</p>
                          <Button
                            size="sm"
                            variant="secondary"
                            className="mt-3 w-full"
                            disabled={busy || (isImg ? !canDeploy : !canConnect)}
                            onClick={() => runTemplate(t)}
                          >
                            {busyKey === key ? (isImg ? "Starting…" : "Connecting…") : isImg ? "Deploy" : "Use template"}
                          </Button>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </Panel>
          </div>
        </div>
      )}
    </div>
  );
}

const BACK_LINK_CLS =
  "inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground";

// looksLikeGitUrl decides whether the quick box should connect a repo (a clear Git URL)
// rather than deploy an image. A bare "owner/name" stays an image (the common quick path) —
// only an explicit URL/SSH/github.com reference is treated as a repository.
function looksLikeGitUrl(raw: string): boolean {
  const s = raw.trim().toLowerCase();
  if (!s) return false;
  return s.startsWith("http://") || s.startsWith("https://") || s.startsWith("git@") || s.includes("github.com");
}

// shortRepo strips protocol/host noise from a repo URL for a compact "owner/repo" label.
function shortRepo(url: string): string {
  return url
    .trim()
    .replace(/^https?:\/\//, "")
    .replace(/^www\./, "")
    .replace(/^github\.com\//, "")
    .replace(/\.git$/, "")
    .replace(/\/+$/, "");
}

// SectionHeader is the column header for the two pickers: an icon tile, a title with an
// optional count, a subtitle, and a right-aligned slot for the search box.
function SectionHeader({
  icon: Icon,
  title,
  subtitle,
  count,
  children,
}: {
  icon: LucideIcon;
  title: string;
  subtitle?: string;
  count?: number;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 border-b border-border px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-lg border border-border bg-muted text-muted-foreground">
          <Icon className="h-4 w-4" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-foreground">{title}</h2>
            {typeof count === "number" && (
              <span className="rounded-full bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">{count}</span>
            )}
          </div>
          {subtitle && <p className="truncate text-xs text-muted-foreground">{subtitle}</p>}
        </div>
      </div>
      {children && <div className="sm:w-56 sm:shrink-0">{children}</div>}
    </div>
  );
}

function SearchInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div className="relative">
      <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
      <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className="h-9 pl-8" />
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="space-y-2.5 p-4">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-11 w-full" />
      ))}
    </div>
  );
}

function Field({ label, icon: Icon, children }: { label: string; icon?: LucideIcon; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-foreground">
        {Icon && <Icon className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />}
        {label}
      </span>
      {children}
    </label>
  );
}
