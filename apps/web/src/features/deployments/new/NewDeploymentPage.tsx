import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowLeft,
  Boxes,
  ChevronDown,
  Container,
  FlaskConical,
  FolderGit2,
  GitBranch,
  GitFork,
  Globe,
  Layers,
  Lock,
  Network,
  Search,
  Server,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton } from "@/components/ui";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { slugify } from "@/features/services/serviceData";
import { cn } from "@/lib/cn";
import { serviceClient } from "@/lib/clients";
import {
  useAgents,
  useBranches,
  useEnvironments,
  useFrameworkDetection,
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

type Visibility = "public" | "private";

// NewDeploymentPage is the full-page "add a service" picker (Vercel's New Project, adapted
// for BYOS). A "Deploy to" bar fixes the target (project + environment + server) plus the
// new service's name and visibility; below it a quick image/URL box and two columns —
// Import Git Repository (the connected account's repos) and Templates — each item carrying
// its own action. Every lane creates a SERVICE via ServiceService.createService with
// deployNow: a public image / public Git repo / template builds-and-deploys now and lands
// on the deployment; a connected-account (OAuth) repo creates the service but can't build
// yet (no agent credential this slice), so it lands on the service detail page.
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
  // Environments are MULTI-select: the same service config can be created in several
  // environments at once (e.g. staging + production). An empty selection follows the default
  // (the project's first environment); toggling records an explicit choice.
  const [selectedEnvIds, setSelectedEnvIds] = useState<string[]>([]);
  const [serverOverride, setServerOverride] = useState("");
  // The new service's identity. An empty name auto-derives from the source (image/repo) per
  // lane; visibility defaults to public (reachable on the internet via Caddy).
  const [name, setName] = useState("");
  const [visibility, setVisibility] = useState<Visibility>("public");
  // Quick deploy (a public image, or a public Git URL).
  const [quickValue, setQuickValue] = useState("");
  const [quickPort, setQuickPort] = useState("80");
  // The debounced repo URL fed to framework detection (empty disables the preview).
  const [detectRepo, setDetectRepo] = useState("");
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
  // Preview how the quick-deploy Git URL would build (server-side framework detection — the
  // same internal/builder logic the agent runs, so the preview matches what gets built).
  const detection = useFrameworkDetection(detectRepo, "");

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

  // Keep the environment selection valid for the chosen project. Drop ids that aren't in the
  // current project's environments; when nothing valid is left, default to the first one so
  // there's always exactly one target until the user picks more. Derived during render (no
  // effect) so a project switch re-defaults cleanly.
  const envs = environments.data;
  const envList = envs ?? [];
  const validSelected = selectedEnvIds.filter((id) => envList.some((e) => e.id === id));
  const environmentIds = validSelected.length ? validSelected : envList[0] ? [envList[0].id] : [];
  const envName = (id: string) => envList.find((e) => e.id === id)?.name ?? id;

  // Toggle an environment in/out of the target set, keeping at least one selected (so a
  // deselect of the last one is a no-op rather than snapping to a different environment).
  function toggleEnv(id: string) {
    setError("");
    const current = environmentIds;
    if (current.includes(id)) {
      if (current.length === 1) return;
      setSelectedEnvIds(current.filter((x) => x !== id));
    } else {
      setSelectedEnvIds([...current, id]);
    }
  }

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
    // Clearing on a repo URL change lets detection prefill the port; a later manual edit then
    // sticks (the prefill effect below only fills an empty field).
    if (quickIsRepo) setQuickPort("");
  }

  // Debounce the repo URL, then preview how it builds. Only GitHub repos can be previewed; a
  // repo on another host still deploys (the agent detects on the clone) — we just don't preview.
  // The state update lives in the timer callback (an async context) so it doesn't trigger a
  // cascading render from the effect body.
  useEffect(() => {
    const repo = quickIsRepo && parseGitHubRepo(quickValue) ? quickValue.trim() : "";
    const timer = setTimeout(() => setDetectRepo(repo), repo ? 600 : 0);
    return () => clearTimeout(timer);
  }, [quickValue, quickIsRepo]);

  // Prefill the port when detection resolves (adjusted during render, not in an effect). It
  // only fills an empty field — the URL-change handler clears it — so a manual edit sticks.
  const detectedPort = detection.data?.containerPort ?? 0;
  const [prevDetectedPort, setPrevDetectedPort] = useState(detectedPort);
  if (detectedPort !== prevDetectedPort) {
    setPrevDetectedPort(detectedPort);
    if (detectedPort > 0 && quickPort === "") setQuickPort(String(detectedPort));
  }

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
  // Creating a service needs a full target (project + at least one environment + server).
  const canDeploy = Boolean(projectId) && environmentIds.length > 0 && Boolean(serverId) && !noServers;
  const slugPreview = slugify(name);

  function startConnect() {
    // Return to this add-service flow after GitHub authorizes — the OAuth handler defaults to
    // /projects otherwise. The server's safeReturnPath strips any query, so we send a clean
    // path; the page re-resolves the target project from the active scope on return.
    const returnTo = encodeURIComponent("/deployments/new");
    window.location.assign(
      `/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}&return_to=${returnTo}`,
    );
  }

  // After creating a service: a build that enqueued lands on the deployment; otherwise (an
  // OAuth/private git service that can't build yet) lands on the new service detail page.
  function onCreated(serviceId: string, deploymentId: string) {
    void queryClient.invalidateQueries({ queryKey: ["services"] });
    void queryClient.invalidateQueries({ queryKey: ["deployments"] });
    if (deploymentId) {
      void navigate({
        to: "/projects/$projectId/deployments/$deploymentId",
        params: { projectId, deploymentId },
      });
    } else {
      void navigate({
        to: "/projects/$projectId/services/$serviceId",
        params: { projectId, serviceId },
      });
    }
  }

  // Format a createService failure for one environment: a duplicate name there
  // (AlreadyExists) gets a precise message; everything else uses the connect message.
  function formatErr(err: unknown, envLabel: string, serviceName: string, fallback: string): string {
    if (err instanceof ConnectError && err.code === Code.AlreadyExists) {
      return `${envLabel}: a service named "${serviceName}" already exists there`;
    }
    if (err instanceof ConnectError) return `${envLabel}: ${err.message}`;
    return `${envLabel}: ${fallback}`;
  }

  // Shared target guard for every lane.
  function guardTarget(): boolean {
    setError("");
    if (!projectId) return Boolean(setError("Pick a project to deploy to"));
    if (environmentIds.length === 0) return Boolean(setError("Pick at least one environment (add one on the project first)"));
    if (!serverId) return Boolean(setError("Pick a connected server"));
    return true;
  }

  // The createService source shape shared by every lane (everything except the per-call
  // environment + the shared visibility/server/deployNow).
  type ServiceSource = Pick<
    Parameters<typeof serviceClient.createService>[0],
    "name" | "sourceKind" | "imageRef" | "repoUrl" | "owner" | "repo" | "branch" | "containerPort"
  >;

  // runCreate creates the SAME service in EVERY selected environment (each environment gets
  // its own service + first deployment). One environment behaves like before — it lands on the
  // deployment, or the service detail page for an OAuth source that can't build yet. Several
  // environments report the spread and return to the project, where all the new services are
  // listed. Partial failure (e.g. a duplicate name in one environment) is surfaced without
  // discarding the environments that succeeded.
  async function runCreate(source: ServiceSource, key: string, fallback: string) {
    if (!guardTarget()) return;
    setBusyKey(key);
    setError("");
    const serviceName = source.name ?? "service";
    const created: { serviceId: string; deploymentId: string }[] = [];
    const failures: string[] = [];
    for (const envId of environmentIds) {
      try {
        const { service, deploymentId } = await serviceClient.createService({
          ...source,
          environmentId: envId,
          visibility,
          serverId,
          deployNow: true,
        });
        if (service) created.push({ serviceId: service.id, deploymentId });
        else failures.push(formatErr(new Error(fallback), envName(envId), serviceName, fallback));
      } catch (err) {
        failures.push(formatErr(err, envName(envId), serviceName, fallback));
      }
    }
    setBusyKey(null);
    if (created.length === 0) {
      setError(failures.join(" · ") || fallback);
      return;
    }
    void queryClient.invalidateQueries({ queryKey: ["services"] });
    void queryClient.invalidateQueries({ queryKey: ["deployments"] });
    if (failures.length) toast.error(failures.join(" · "));
    if (created.length === 1) {
      onCreated(created[0].serviceId, created[0].deploymentId);
      return;
    }
    toast.success(`Created "${serviceName}" in ${created.length} environments`);
    void navigate({ to: "/projects/$projectId", params: { projectId } });
  }

  // Create a service from a pre-built image and deploy it now.
  function createImageService(imageRef: string, port: number, key: string, fallbackName: string) {
    setError("");
    const ref = imageRef.trim();
    if (!ref) return setError("Enter an image reference, e.g. traefik/whoami");
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return setError("Container port must be between 1 and 65535");
    }
    void runCreate(
      { name: name.trim() || fallbackName, sourceKind: "image", imageRef: ref, containerPort: port },
      key,
      "Could not create the service",
    );
  }

  // Create a service from a PUBLIC git repo (URL) and deploy it now. The agent clones the
  // repo, builds its Dockerfile, and runs it. port 0 means "auto-detect from EXPOSE".
  function createPublicGitService(repoUrl: string, branch: string, port: number, key: string, fallbackName: string) {
    setError("");
    const url = repoUrl.trim();
    if (!url) return setError("Enter a repository URL");
    if (port !== 0 && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      return setError("Container port must be between 1 and 65535, or blank to auto-detect");
    }
    void runCreate(
      { name: name.trim() || fallbackName, sourceKind: "git", repoUrl: url, branch: branch.trim(), containerPort: port },
      key,
      "Could not create and deploy the repository",
    );
  }

  // Create a service from a connected-account (OAuth) repository. Building an OAuth repo isn't
  // supported yet (the agent gets no credential this slice), so deployNow can't enqueue a
  // build — each service is created and (for one environment) the user lands on its detail page.
  function createOAuthGitService(owner: string, repo: string, branch: string, key: string) {
    void runCreate(
      { name: name.trim() || repo, sourceKind: "git", owner, repo, branch: branch.trim() },
      key,
      "Could not create the service",
    );
  }

  // The quick box creates an image service unless it clearly looks like a Git URL — in which
  // case it builds and deploys a PUBLIC repo service. Both need a full target. An image needs
  // a port; a repo auto-detects from the Dockerfile when blank (quickIsRepo is derived above).
  const quickReady =
    quickValue.trim().length > 0 && canDeploy && (quickIsRepo || Number(quickPort) >= 1);
  function runQuick() {
    if (quickIsRepo) {
      const gh = parseGitHubRepo(quickValue);
      void createPublicGitService(quickValue, "", quickPort.trim() ? Number(quickPort) : 0, "quick", gh?.repo ?? "service");
    } else {
      void createImageService(quickValue, Number(quickPort), "quick", imageNameHint(quickValue));
    }
  }

  function runTemplate(t: DeployTemplate) {
    if (templateSourceKind(t) === "image") {
      void createImageService(t.imageRef ?? "", t.containerPort, `tpl:${t.id}`, t.id);
    } else {
      void createPublicGitService(t.repoUrl ?? "", t.defaultBranch ?? "", t.containerPort, `tpl:${t.id}`, t.id);
    }
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
          <Boxes className="h-6 w-6" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">Add a service</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            A service is a deployable component — a public image, a public Git repo (its Dockerfile builds on your
            server), or a connected repository. Name it, choose where it lands, then pick what to deploy.
          </p>
        </div>
      </div>

      {!workspaceId ? (
        <EmptyState
          title="Select a workspace first"
          body="Choose a workspace to deploy under, then come back to add a service."
        />
      ) : (
        <div className="space-y-6">
          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          {/* Deploy target — where everything below lands, plus the new service's identity. */}
          <Panel>
            <PanelHeader
              title="Deploy to"
              description="Where the service lands, what it's called, and whether it's reachable on the internet."
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
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      type="button"
                      disabled={!envList.length}
                      className="flex h-9 w-full items-center justify-between gap-2 rounded-md border border-input bg-card px-3 text-sm text-foreground shadow-sm outline-none transition focus:border-ring focus:ring-2 focus:ring-ring/25 disabled:cursor-not-allowed disabled:opacity-50 data-[state=open]:border-ring data-[state=open]:ring-2 data-[state=open]:ring-ring/25"
                    >
                      <span className="truncate text-left">
                        {environmentIds.length === 0
                          ? "Select environments"
                          : environmentIds.length === 1
                            ? envName(environmentIds[0])
                            : `${environmentIds.length} environments`}
                      </span>
                      <ChevronDown className="h-4 w-4 shrink-0 opacity-60" aria-hidden="true" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="max-h-64 w-[var(--radix-dropdown-menu-trigger-width)] min-w-48 overflow-y-auto">
                    {envList.map((env) => (
                      <DropdownMenuCheckboxItem
                        key={env.id}
                        checked={environmentIds.includes(env.id)}
                        onCheckedChange={() => toggleEnv(env.id)}
                        onSelect={(e) => e.preventDefault()}
                      >
                        {env.name}
                        <span className="ml-1.5 text-xs text-muted-foreground">{env.type}</span>
                      </DropdownMenuCheckboxItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                {noEnvironments ? (
                  <p className="mt-1 text-xs text-muted-foreground">No environments yet — add one on the project page.</p>
                ) : (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {environmentIds.length > 1
                      ? `Same service created in all ${environmentIds.length} selected environments.`
                      : "Pick one or more — the same service is created in each."}
                  </p>
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
              <Field label="Service name" icon={Boxes}>
                <Input
                  value={name}
                  onChange={(e) => {
                    setError("");
                    setName(e.target.value);
                  }}
                  placeholder="Leave blank to derive from the source"
                  autoCapitalize="none"
                  spellCheck={false}
                />
                <p className="mt-1 truncate text-xs text-muted-foreground">
                  {slugPreview ? (
                    <>
                      URL slug: <span className="font-mono text-foreground">{slugPreview}</span>
                    </>
                  ) : (
                    "A short name, unique within the environment."
                  )}
                </p>
              </Field>
              <Field label="Visibility" icon={visibility === "public" ? Globe : Lock}>
                <div className="inline-flex rounded-md border border-input bg-muted/30 p-0.5">
                  {(["public", "private"] as const).map((v) => (
                    <button
                      key={v}
                      type="button"
                      onClick={() => setVisibility(v)}
                      className={cn(
                        "rounded px-3 py-1.5 text-sm transition-colors",
                        visibility === v
                          ? "bg-background font-medium text-foreground shadow-sm"
                          : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      {v === "public" ? "Public" : "Private"}
                    </button>
                  ))}
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {visibility === "public"
                    ? "Routed on the internet via Caddy with a public URL."
                    : "Reachable only by sibling services at its internal host."}
                </p>
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
                {quickIsRepo ? <GitFork className="h-4 w-4" aria-hidden="true" /> : <Boxes className="h-4 w-4" aria-hidden="true" />}
                {busyKey === "quick" ? "Working…" : "Add service"}
              </Button>
            </div>
            {quickValue.trim() && !quickIsRepo && (
              <p className="mt-2 text-xs text-muted-foreground">
                {`Adds an image service to ${projectName} on port ${quickPort || "…"}.`}
              </p>
            )}
            {quickIsRepo && quickValue.trim() && (
              <DetectionHint detection={detection} isGitHub={!!parseGitHubRepo(quickValue)} projectName={projectName} />
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
                              disabled={busy || !canDeploy}
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
                              disabled={busy || !canDeploy || !repoBranch}
                              onClick={() => createOAuthGitService(r.owner, r.name, repoBranch, `repo:${r.fullName}`)}
                            >
                              {busyKey === `repo:${r.fullName}` ? "Adding…" : "Add service"}
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
                            {busyKey === key ? (isImg ? "Starting…" : "Building…") : "Add service"}
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

// imageNameHint derives a friendly default service name from an image ref: the final path
// segment, minus any tag/digest (e.g. ghcr.io/org/api:1.2 → "api").
function imageNameHint(imageRef: string): string {
  const path = imageRef.trim().split("/").pop() ?? "";
  const name = path.split("@")[0].split(":")[0];
  return name || "service";
}

// looksLikeGitUrl decides whether the quick box should create a git service (a clear Git
// URL) rather than an image service. A bare "owner/name" stays an image (the common quick
// path), and so do registry refs like ghcr.io/org/img; only an explicit scheme or a GitHub
// host counts as a repo. The GitHub match is anchored to the start with a trailing slash so
// a lookalike host (github.com.evil.com) or a smuggled mid-string "github.com" can't slip
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

// DetectionHint previews how a quick-deploy Git URL would build. It shows the detected runtime,
// suggested commands, port, and the exact generated Dockerfile — or, for an unsupported repo,
// the plain-English next steps. It degrades gracefully: a failed/absent preview just notes that
// the agent detects at build time, so the user can still deploy. Detection is GitHub-only for
// now; a repo on another host still builds (the agent detects on the clone).
function DetectionHint({
  detection,
  isGitHub,
  projectName,
}: {
  detection: ReturnType<typeof useFrameworkDetection>;
  isGitHub: boolean;
  projectName: string;
}) {
  if (!isGitHub) {
    return (
      <p className="mt-2 text-xs text-muted-foreground">
        Public Git repo — Plorigo detects the framework and builds it when you deploy to {projectName}.
      </p>
    );
  }
  if (detection.isFetching) {
    return <p className="mt-2 text-xs text-muted-foreground">Inspecting the repository…</p>;
  }
  const d = detection.data;
  if (!d || detection.isError) {
    return (
      <p className="mt-2 text-xs text-muted-foreground">
        Plorigo detects the framework and builds it when you deploy.
      </p>
    );
  }
  if (d.status === "unsupported") {
    return (
      <div className="mt-2 rounded-md border border-border bg-muted/40 p-2.5 text-xs text-muted-foreground">
        <p className="font-medium text-foreground">Couldn&apos;t detect a supported app</p>
        <p className="mt-0.5">{d.nextSteps}</p>
      </div>
    );
  }
  if (d.status === "dockerfile") {
    return <p className="mt-2 text-xs text-muted-foreground">Found a Dockerfile — Plorigo builds it as-is.</p>;
  }
  return (
    <div className="mt-2 space-y-1.5 text-xs text-muted-foreground">
      <p>
        Detected <span className="font-medium text-foreground">{d.runtimeLabel}</span> · {d.packageManager} · node{" "}
        {d.nodeVersion} · port {d.containerPort}
      </p>
      <div className="font-mono text-[11px] leading-relaxed">
        {d.buildCommand && <div>build: {d.buildCommand}</div>}
        <div>start: {d.startCommand}</div>
      </div>
      <details className="group">
        <summary className="cursor-pointer select-none text-foreground/80 hover:text-foreground">
          Generated Dockerfile
        </summary>
        <pre className="mt-1 max-h-60 overflow-auto rounded-md border border-border bg-muted/40 p-2 text-[11px] leading-relaxed">
          {d.dockerfile}
        </pre>
      </details>
    </div>
  );
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
