import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Boxes, GitBranch, Globe, Lock, Rocket, Server as ServerIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, Input, Panel, Select } from "@/components/ui";
import {
  deploymentClient,
  environmentClient,
  envVarClient,
  projectClient,
  sourceClient,
} from "@/lib/clients";
import { useAgents, useBranches, useServers } from "@/lib/queries";
import { pickDefaultServer, serverStatusLabel } from "@/lib/serverSelection";
import type { DeployTemplate } from "@/lib/templates";

const ENV_TYPES = ["production", "staging", "preview", "custom"] as const;

// What the user picked on the launchpad: a connected (OAuth) repo, a public Git
// URL, or a curated template (which itself resolves to an image or a public repo).
export type DeploySelection =
  | { kind: "oauth"; owner: string; repo: string; fullName: string; defaultBranch: string; isPrivate: boolean }
  | { kind: "public"; repoUrl: string }
  | { kind: "template"; template: DeployTemplate };

// SimpleDeployWizard is the streamlined (Vercel-style) lane: minimal questions,
// then it creates the project + a first environment and either deploys (image /
// template-with-image) or connects the repository and lands on the project page.
// Real build-from-Git for repo sources is wired in a later slice; until then a
// repo import sets everything up so the first deploy is one click away.
export function SimpleDeployWizard({
  workspaceId,
  selection,
  onBack,
}: {
  workspaceId: string;
  selection: DeploySelection;
  onBack: () => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const source = useMemo(() => normalizeSource(selection), [selection]);
  const isImage = source.type === "image";
  // Only image sources can deploy immediately in this slice; repo sources connect
  // and land on the project. willDeployNow gates the server step and the copy.
  const willDeployNow = isImage;

  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);

  const oauthOwner = source.type === "repo" && source.access === "oauth" ? source.owner : "";
  const oauthRepo = source.type === "repo" && source.access === "oauth" ? source.repo : "";
  const branches = useBranches(workspaceId, oauthOwner, oauthRepo);

  const [name, setName] = useState(() => defaultName(selection));
  const [envName, setEnvName] = useState("Production");
  const [envType, setEnvType] = useState<string>("production");
  const [branch, setBranch] = useState(() => defaultBranch(selection));
  const [containerPort, setContainerPort] = useState(() => String(defaultPort(selection)));
  const [serverId, setServerId] = useState("");
  const [envVars, setEnvVars] = useState(() => initialEnvVars(selection));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Default an OAuth branch to the repo's default (or the first branch that loads).
  useEffect(() => {
    if (oauthOwner === "" || branch) return;
    const list = branches.data ?? [];
    if (list.length > 0) setBranch(list[0]);
  }, [oauthOwner, branch, branches.data]);

  // Default the server to a ready one (only relevant when we deploy now).
  useEffect(() => {
    if (!willDeployNow || serverId) return;
    const def = pickDefaultServer(servers.data, agents.data);
    if (def) setServerId(def.id);
  }, [willDeployNow, serverId, servers.data, agents.data]);

  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;
  const submitDisabled =
    busy || !name.trim() || !envName.trim() || (willDeployNow && (noServers || !serverId));

  function setEnvValue(key: string, value: string) {
    setEnvVars((prev) => prev.map((ev) => (ev.key === key ? { ...ev, value } : ev)));
  }

  function landOnProject(projectId: string) {
    setBusy(false);
    void navigate({ to: "/projects/$projectId", params: { projectId } });
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const projectName = name.trim();
    const environmentName = envName.trim();
    if (!projectName || !environmentName) return;

    let port = 0;
    if (isImage) {
      port = Number(containerPort);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        setError("Container port must be between 1 and 65535");
        return;
      }
    }
    if (willDeployNow && !serverId) {
      setError("Connect a server first, then pick it here");
      return;
    }

    setBusy(true);
    setError("");

    // 1) Create the project.
    let projectId = "";
    try {
      const { project } = await projectClient.createProject({ workspaceId, name: projectName });
      if (!project) throw new Error("the project was not created");
      projectId = project.id;
      await queryClient.invalidateQueries({ queryKey: ["projects", workspaceId] });
    } catch (err) {
      setError(connectMessage(err, "Could not create the project"));
      setBusy(false);
      return;
    }

    // 2) Create its first environment.
    let environmentId = "";
    try {
      const { environment } = await environmentClient.createEnvironment({
        projectId,
        name: environmentName,
        type: envType,
      });
      if (!environment) throw new Error("the environment was not created");
      environmentId = environment.id;
      await queryClient.invalidateQueries({ queryKey: ["environments", projectId] });
    } catch (err) {
      toast.error(
        `Project created, but its environment failed: ${connectMessage(err, "unknown error")}. Add one from the project page.`,
      );
      landOnProject(projectId);
      return;
    }

    // 3) Apply any suggested env vars (best-effort — never blocks the deploy).
    let appliedEnv = false;
    for (const ev of envVars) {
      const value = ev.value.trim();
      if (!value) continue;
      try {
        await envVarClient.setEnvVar({ environmentId, key: ev.key, value });
        appliedEnv = true;
      } catch {
        // The user can set it on the project page; don't fail the flow.
      }
    }
    if (appliedEnv) {
      await queryClient.invalidateQueries({ queryKey: ["envVars", environmentId] });
    }

    // 4a) Image source → deploy now.
    if (source.type === "image") {
      try {
        const { deployment } = await deploymentClient.createDeployment({
          environmentId,
          serverId,
          imageRef: source.imageRef,
          containerPort: port,
        });
        if (!deployment) throw new Error("the deployment was not created");
        await queryClient.invalidateQueries({ queryKey: ["deployments"] });
        toast.success(`Deploying ${projectName}`);
        void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
      } catch (err) {
        toast.error(
          `Project is ready, but the deploy failed: ${connectMessage(err, "unknown error")}. Deploy from the project page.`,
        );
        landOnProject(projectId);
      }
      return;
    }

    // 4b) Repo source → connect it. Build-and-deploy wiring lands in a later slice.
    try {
      if (source.access === "public") {
        await sourceClient.connectPublicRepository({
          projectId,
          repoUrl: source.repoUrl,
          branch: branch.trim(),
        });
      } else {
        await sourceClient.connectRepository({
          projectId,
          owner: source.owner,
          repo: source.repo,
          branch: branch.trim(),
        });
      }
      await queryClient.invalidateQueries({ queryKey: ["projectSource", projectId] });
      await queryClient.invalidateQueries({ queryKey: ["sources", workspaceId] });
      toast.success(`Imported ${projectName}`);
    } catch (err) {
      toast.error(
        `Project created, but connecting the repo failed: ${connectMessage(err, "unknown error")}. Connect it from the project page.`,
      );
    }
    landOnProject(projectId);
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Choose a different source
      </button>

      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">Configure your project</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          {willDeployNow
            ? "Review the details and deploy — Plorigo creates the project, its first environment, and runs it."
            : "Review the details — Plorigo creates the project, its first environment, and connects this repository."}
        </p>
      </div>

      <Panel className="p-5">
        <SourceSummary selection={selection} />

        <form onSubmit={onSubmit} className="mt-5 space-y-4">
          <Field label="Project name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" autoFocus required />
          </Field>

          {source.type === "repo" && source.access === "oauth" && (
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

          {source.type === "repo" && source.access === "public" && (
            <Field label="Branch (optional)">
              <Input
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="default branch"
                autoCapitalize="none"
                spellCheck={false}
              />
            </Field>
          )}

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

          {isImage && (
            <Field label="Container port">
              <Input
                value={containerPort}
                onChange={(e) => setContainerPort(e.target.value)}
                inputMode="numeric"
                placeholder="80"
                required
              />
            </Field>
          )}

          {envVars.length > 0 && (
            <div className="space-y-2">
              <p className="text-sm font-medium text-foreground">Environment variables</p>
              {envVars.map((ev) => (
                <div key={ev.key} className="grid grid-cols-[minmax(0,200px)_minmax(0,1fr)] items-center gap-2">
                  <code className="truncate rounded-md bg-muted px-2 py-1.5 text-xs text-muted-foreground">{ev.key}</code>
                  <Input
                    value={ev.value}
                    onChange={(e) => setEnvValue(ev.key, e.target.value)}
                    placeholder={ev.note ?? "value"}
                    spellCheck={false}
                  />
                </div>
              ))}
            </div>
          )}

          {willDeployNow && (
            <Field label="Deploy to server">
              {noServers ? (
                <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                  <p className="flex items-center gap-1.5 font-medium text-foreground">
                    <ServerIcon className="h-4 w-4" aria-hidden="true" />
                    No servers connected
                  </p>
                  <p className="mt-1">Plorigo runs your apps on a server you own. Connect one to deploy.</p>
                  <Link to="/servers" className="mt-2 inline-block font-medium text-primary hover:underline">
                    Connect a server →
                  </Link>
                </div>
              ) : (
                <Select value={serverId} onChange={(e) => setServerId(e.target.value)} disabled={!servers.data?.length}>
                  {(servers.data ?? []).map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name} — {serverStatusLabel(s.id, agents.data)}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}

          {source.type === "repo" && selection.kind === "oauth" && selection.isPrivate && (
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Lock className="h-3 w-3" aria-hidden="true" />
              Private repository — Plorigo accesses it through your GitHub connection.
            </p>
          )}

          {!willDeployNow && (
            <p className="text-xs text-muted-foreground">
              One-click building from Git is rolling out. For now Plorigo connects the repository so your first deploy is
              a click away on the project page.
            </p>
          )}

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <div className="flex justify-end">
            <Button type="submit" disabled={submitDisabled}>
              <Rocket className="h-4 w-4" aria-hidden="true" />
              {willDeployNow ? (busy ? "Deploying…" : "Deploy") : busy ? "Importing…" : "Import project"}
            </Button>
          </div>
        </form>
      </Panel>
    </div>
  );
}

function SourceSummary({ selection }: { selection: DeploySelection }) {
  if (selection.kind === "oauth") {
    return (
      <SummaryRow icon={<GitBranch className="h-4 w-4" aria-hidden="true" />} title={selection.fullName} subtitle="GitHub repository">
        {selection.isPrivate && <Badge tone="neutral">private</Badge>}
      </SummaryRow>
    );
  }
  if (selection.kind === "public") {
    return (
      <SummaryRow icon={<Globe className="h-4 w-4" aria-hidden="true" />} title={selection.repoUrl} subtitle="Public Git repository" />
    );
  }
  const template = selection.template;
  return (
    <SummaryRow
      icon={<Boxes className="h-4 w-4" aria-hidden="true" />}
      title={template.name}
      subtitle={template.imageRef ? `Template · image ${template.imageRef}` : `Template · ${template.repoUrl}`}
    >
      <Badge tone="blue">{template.category}</Badge>
    </SummaryRow>
  );
}

function SummaryRow({
  icon,
  title,
  subtitle,
  children,
}: {
  icon: ReactNode;
  title: string;
  subtitle: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-muted/30 px-3 py-2.5">
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-border bg-card text-muted-foreground">
          {icon}
        </span>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-foreground">{title}</p>
          <p className="truncate text-xs text-muted-foreground">{subtitle}</p>
        </div>
      </div>
      {children && <div className="flex shrink-0 items-center gap-1.5">{children}</div>}
    </div>
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

type WizardSource =
  | { type: "image"; imageRef: string }
  | { type: "repo"; access: "oauth"; owner: string; repo: string }
  | { type: "repo"; access: "public"; repoUrl: string };

function normalizeSource(selection: DeploySelection): WizardSource {
  if (selection.kind === "oauth") {
    return { type: "repo", access: "oauth", owner: selection.owner, repo: selection.repo };
  }
  if (selection.kind === "public") {
    return { type: "repo", access: "public", repoUrl: selection.repoUrl };
  }
  if (selection.template.imageRef) {
    return { type: "image", imageRef: selection.template.imageRef };
  }
  return { type: "repo", access: "public", repoUrl: selection.template.repoUrl ?? "" };
}

function defaultName(selection: DeploySelection): string {
  if (selection.kind === "oauth") return selection.repo;
  if (selection.kind === "public") return repoNameFromUrl(selection.repoUrl);
  return slugifyName(selection.template.name);
}

function defaultBranch(selection: DeploySelection): string {
  if (selection.kind === "oauth") return selection.defaultBranch ?? "";
  if (selection.kind === "template") return selection.template.defaultBranch ?? "";
  return "";
}

function defaultPort(selection: DeploySelection): number {
  return selection.kind === "template" ? selection.template.containerPort : 80;
}

function initialEnvVars(selection: DeploySelection): Array<{ key: string; value: string; note?: string }> {
  if (selection.kind !== "template" || !selection.template.suggestedEnv) return [];
  return selection.template.suggestedEnv.map((e) => ({ key: e.key, value: e.value ?? "", note: e.note }));
}

function connectMessage(err: unknown, fallback: string): string {
  return err instanceof ConnectError ? err.message : fallback;
}

function slugifyName(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// repoNameFromUrl pulls the repo segment out of a Git URL for a name default; the
// server is authoritative, so a best-effort parse is enough.
function repoNameFromUrl(raw: string): string {
  let s = raw.trim();
  if (!s) return "";
  if (s.startsWith("git@github.com:")) {
    s = s.slice("git@github.com:".length);
  } else {
    s = s.replace(/^https?:\/\//, "").replace(/^www\./, "").replace(/^github\.com\//, "");
  }
  const parts = s.replace(/^\/+/, "").split("/").filter(Boolean);
  if (parts.length < 2) return "";
  return parts[1].replace(/\.git$/, "");
}
