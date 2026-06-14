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
// action. Public images and public Git repos build-and-deploy now; a connected-account
// (OAuth) repo connects as the project's source (building it needs the GitHub App, later).
// An optional ?project= preselects the target and scopes "Back".
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

  // Target. Selections use an override-or-default shape: an empty override means "follow the
  // computed default" (derived below from freshly loaded data); the Select onChange records an
  // explicit user choice. Deriving keeps defaults correct without syncing through effects.
  const [projectOverride, setProjectOverride] = useState("");
  const [environmentOverride, setEnvironmentOverride] = useState("");
  const [serverOverride, setServerOverride] = useState("");
  // Quick deploy (a public image, or a public Git URL).
  const [quickValue, setQuickValue] = useState("");
  const [quickPort, setQuickPort] = useState("80");
  // Feedback for the Git-URL port auto-detection (checking / detected / not found).
  const [portHint, setPortHint] = useState("");
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

  // Default the project: a valid current selection wins, then ?project= / the active filter,
  // then the first project (guarding against a stale or foreign id).
  const projectList = projects.data ?? [];
  const projectId =
    projectOverride && projectList.some((p) => p.id === projectOverride)
      ? projectOverride
      : preferredProject && projectList.some((p) => p.id === preferredProject)
        ? preferredProject
        : (projectList[0]?.id ?? "");

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

  // Keep the environment valid for the chosen project (default to its first).
  const envs = environments.data;
  const environmentId =
    envs?.length
      ? (environmentOverride && envs.some((e) => e.id === environmentOverride) ? environmentOverride : envs[0].id)
      : "";

  // Default the server to a ready one (then any online, then the first server).
  const serverId = serverOverride || pickDefaultServer(servers.data, agents.data)?.id || "";

  // When a repo is expanded, default its branch to the repo's default. Tracking the previous
  // repo and adjusting during render keeps this out of an effect.
  const [prevSelectedRepo, setPrevSelectedRepo] = useState(selectedRepo);
  if (selectedRepo !== prevSelectedRepo) {
    setPrevSelectedRepo(selectedRepo);
    if (selectedRepo) setRepoBranch(selectedRepo.defaultBranch || "");
  }

  const quickIsRepo = looksLikeGitUrl(quickValue);

  // An image deploy needs an explicit port, so default it to 80 when the input becomes an
  // image (only on the kind flip, so typing an image ref keeps a custom port). Clearing the
  // port + hint when the URL changes is likewise adjusted during render, leaving the effect
  // below purely for the async Dockerfile read.
  const [prevQuickIsRepo, setPrevQuickIsRepo] = useState(quickIsRepo);
  if (quickIsRepo !== prevQuickIsRepo) {
    setPrevQuickIsRepo(quickIsRepo);
    if (!quickIsRepo) setQuickPort("80");
  }
  const [prevQuickValue, setPrevQuickValue] = useState(quickValue);
  if (quickValue !== prevQuickValue) {
    setPrevQuickValue(quickValue);
    if (quickIsRepo) {
      setQuickPort("");
      setPortHint(
        parseGitHubRepo(quickValue)
          ? "Checking the repo's Dockerfile…"
          : "Public Git repo — the port is auto-detected from the Dockerfile when it builds.",
      );
    }
  }

  // For a Git URL, read the repo's Dockerfile up front and PREFILL the port (from its EXPOSE)
  // so the user sees it instead of a mysterious blank. Best-effort + debounced; if it can't be
  // found the field stays blank and the agent still auto-detects from the built image. Public,
  // unauthenticated GitHub read — re-runs when the URL changes, so changing repos re-detects;
  // a manual edit on the SAME URL sticks (this effect won't re-fire).
  useEffect(() => {
    if (!quickIsRepo) return;
    const gh = parseGitHubRepo(quickValue);
    if (!gh) return;
    const ctrl = new AbortController();
    const timer = setTimeout(async () => {
      const port = await detectDockerfilePort(gh.owner, gh.repo, ctrl.signal);
      if (ctrl.signal.aborted) return;
      if (port) {
        setQuickPort(String(port));
        setPortHint(`Detected port ${port} from the repo's Dockerfile (edit if needed).`);
      } else {
        setPortHint("No EXPOSE found — the port is auto-detected from the built image, or set one.");
      }
    }, 600);
    return () => {
      ctrl.abort();
      clearTimeout(timer);
    };
  }, [quickValue, quickIsRepo]);

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

  // Connect a connected-account (OAuth) repository as the project's source. Building from a
  // private/OAuth repo isn't supported yet (the agent gets no credential this slice), so this
  // stays connect-only; public repos build-and-deploy via deployPublicSource below.
  async function connectOAuthRepo(owner: string, repo: string, branch: string, key: string) {
    setError("");
    if (!projectId) return setError("Pick a project to connect to");
    setBusyKey(key);
    try {
      await sourceClient.connectRepository({ projectId, owner, repo, branch });
      await invalidateSource();
      toast.success("Repository connected as the project's source");
      void navigate({ to: "/projects/$projectId", params: { projectId } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not connect the repository");
      setBusyKey(null);
    }
  }

  // Build-and-deploy a PUBLIC repository: connect it as the project's source, then trigger a
  // source deployment. The agent clones the repo, builds its Dockerfile, and runs it. No
  // credential is involved (public repos only); private repos use the connect-only path above.
  // port 0 means "auto-detect from the Dockerfile's EXPOSE on the agent".
  async function deployPublicSource(repoUrl: string, branch: string, port: number, key: string) {
    setError("");
    if (!projectId) return setError("Pick a project to deploy to");
    if (!environmentId) return setError("Pick an environment (add one on the project first)");
    if (!serverId) return setError("Pick a connected server");
    const url = repoUrl.trim();
    if (!url) return setError("Enter a repository URL");
    if (port !== 0 && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      return setError("Container port must be between 1 and 65535, or blank to auto-detect");
    }
    setBusyKey(key);
    try {
      await sourceClient.connectPublicRepository({ projectId, repoUrl: url, branch: branch.trim() });
      await invalidateSource();
      const { deployment } = await deploymentClient.createDeploymentFromSource({
        environmentId,
        serverId,
        containerPort: port,
        gitRef: branch.trim(),
      });
      if (!deployment) throw new Error("the deployment was not created");
      await queryClient.invalidateQueries({ queryKey: ["deployments"] });
      void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not build and deploy the repository");
      setBusyKey(null);
    }
  }

  // The quick box deploys an image unless it clearly looks like a Git URL — in which case it
  // builds and deploys a PUBLIC repo. Both need a full target (project + env + server). An
  // image needs a port; a repo auto-detects it from the Dockerfile when the field is blank
  // (quickIsRepo is derived above, where it also drives the port-field default).
  const quickReady =
    quickValue.trim().length > 0 && canDeploy && (quickIsRepo || Number(quickPort) >= 1);
  function runQuick() {
    if (quickIsRepo) void deployPublicSource(quickValue, "", quickPort.trim() ? Number(quickPort) : 0, "quick");
    else void deployImage(quickValue, Number(quickPort), "quick");
  }

  function runTemplate(t: DeployTemplate) {
    if (templateSourceKind(t) === "image") void deployImage(t.imageRef ?? "", t.containerPort, `tpl:${t.id}`);
    else void deployPublicSource(t.repoUrl ?? "", t.defaultBranch ?? "", t.containerPort, `tpl:${t.id}`);
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
            Run a public image, or build and deploy a public Git repo (its Dockerfile builds on your server). Pick
            where it lands, then choose what to deploy.
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
              description="Where everything below lands. Public images and public Git repos deploy here; importing a connected-account repo just sets it as the project's source."
            />
            <div className="grid gap-4 p-4 sm:grid-cols-3">
              <Field label="Project" icon={FolderGit2}>
                <Select value={projectId} onChange={(e) => setProjectOverride(e.target.value)}>
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
                  <p className="mt-1 text-xs text-muted-foreground">No environments yet — add one on the project page.</p>
                )}
              </Field>
              <Field label="Server" icon={Server}>
                <Select value={serverId} onChange={(e) => setServerOverride(e.target.value)} disabled={!servers.data?.length}>
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
              <div className="w-full sm:w-28">
                <span className="mb-1.5 block text-sm font-medium text-foreground">Port</span>
                <Input
                  value={quickPort}
                  onChange={(e) => setQuickPort(e.target.value)}
                  inputMode="numeric"
                  placeholder={quickIsRepo ? "Auto" : "80"}
                />
              </div>
              <Button onClick={runQuick} disabled={busy || !quickReady}>
                {quickIsRepo ? <GitFork className="h-4 w-4" aria-hidden="true" /> : <Rocket className="h-4 w-4" aria-hidden="true" />}
                {busyKey === "quick" ? "Working…" : "Deploy"}
              </Button>
            </div>
            {quickValue.trim() && (
              <p className="mt-2 text-xs text-muted-foreground">
                {quickIsRepo
                  ? `Looks like a public Git repository — builds its Dockerfile and deploys to ${projectName}.`
                  : `Deploys this image to ${projectName} on port ${quickPort || "…"}.`}
              </p>
            )}
            {quickIsRepo && quickValue.trim() && portHint && (
              <p className="mt-1 text-xs text-muted-foreground">{portHint}</p>
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
                            disabled={busy || !canDeploy}
                            onClick={() => runTemplate(t)}
                          >
                            {busyKey === key ? (isImg ? "Starting…" : "Building…") : "Deploy"}
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
// rather than deploy an image. A bare "owner/name" stays an image (the common quick path),
// and so do registry refs like ghcr.io/org/img; only an explicit scheme or a GitHub host
// counts as a repo. The GitHub match is anchored to the start with a trailing slash so a
// lookalike host (github.com.evil.com) or a smuggled mid-string "github.com" can't slip
// through — this is a routing hint, not a security gate, but the substring form is fragile.
function looksLikeGitUrl(raw: string): boolean {
  const s = raw.trim().toLowerCase();
  if (!s) return false;
  if (/^(https?:\/\/|git@|ssh:\/\/)/.test(s)) return true;
  return /^(www\.)?github\.com\//.test(s);
}

// parseGitHubRepo pulls {owner, repo} from a GitHub URL (https, git@, or bare github.com/…).
// Returns null for non-GitHub URLs (we only auto-read Dockerfiles from GitHub for now).
function parseGitHubRepo(input: string): { owner: string; repo: string } | null {
  const s = input.trim().replace(/^git@github\.com:/i, "https://github.com/");
  const m = /(?:https?:\/\/)?(?:www\.)?github\.com\/([^/\s]+)\/([^/\s#?]+)/i.exec(s);
  if (!m) return null;
  return { owner: m[1], repo: m[2].replace(/\.git$/i, "") };
}

// detectDockerfilePort reads a PUBLIC repo's root Dockerfile (default branch, unauthenticated)
// and returns its first EXPOSE port. Returns null when there's no Dockerfile/EXPOSE or the
// lookup fails — purely a UX preview; the agent still detects from the built image at deploy.
async function detectDockerfilePort(owner: string, repo: string, signal: AbortSignal): Promise<number | null> {
  try {
    const res = await fetch(`https://api.github.com/repos/${owner}/${repo}/contents/Dockerfile`, {
      headers: { Accept: "application/vnd.github.raw" },
      signal,
    });
    if (!res.ok) return null;
    const m = /^\s*EXPOSE\s+(\d{1,5})/im.exec(await res.text());
    if (!m) return null;
    const port = Number(m[1]);
    return port >= 1 && port <= 65535 ? port : null;
  } catch {
    return null; // network/abort/CORS — silently fall back to build-time detection
  }
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
