import { useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowLeft,
  Container,
  ExternalLink,
  GitBranch,
  GitFork,
  Globe,
  Lock,
  Plus,
  RefreshCw,
  Sparkles,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton, StatusDot } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { deploymentClient, envVarClient } from "@/lib/clients";
import {
  useAgents,
  useDeploymentsByService,
  useEnvVars,
  useServers,
  useService,
} from "@/lib/queries";
import { pickDefaultServer, serverStatusLabel } from "@/lib/serverSelection";
import { statusTone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import { deploymentRefLabel } from "@/features/deployments/timeline";
import { internalUrl, isPublic, sourceLabel } from "./serviceData";

// ServiceDetailPage is the single service: its identity and live URL, where it deploys from,
// its non-secret env vars, and its deployment history. The Redeploy button triggers a fresh
// deployment of the service onto a chosen server (CreateDeploymentForService); the control
// plane re-resolves the service's source so a private URL can't be smuggled through.
export function ServiceDetailPage() {
  const { projectId, serviceId } = useParams({ strict: false }) as {
    projectId?: string;
    serviceId?: string;
  };
  const id = serviceId ?? "";
  const pid = projectId ?? "";
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);

  const svc = useService(id);
  const deployments = useDeploymentsByService(id);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);

  const [serverOverride, setServerOverride] = useState("");
  const [redeploying, setRedeploying] = useState(false);

  if (svc.isLoading && !svc.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={pid} />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!svc.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={pid} />
        <EmptyState title="Service not found" body="It may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const s = svc.data;
  const rows = deployments.data ?? [];
  const latest = rows[0];
  const isGit = s.sourceKind === "git";
  const publicSvc = isPublic(s);
  const serverId = serverOverride || pickDefaultServer(servers.data, agents.data)?.id || "";
  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;

  async function redeploy() {
    if (!serverId) {
      toast.error("No connected server to deploy onto. Connect one first.");
      return;
    }
    setRedeploying(true);
    try {
      const { deployment } = await deploymentClient.createDeploymentForService({ serviceId: id, serverId });
      if (!deployment) throw new Error("the deployment was not created");
      void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not start the deployment");
      setRedeploying(false);
    }
  }

  return (
    <div className="space-y-6">
      <BackLink projectId={pid} />

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{s.name}</h1>
            <Badge tone={publicSvc ? "green" : "purple"}>
              {publicSvc ? <Globe className="mr-1 h-3 w-3" aria-hidden="true" /> : <Lock className="mr-1 h-3 w-3" aria-hidden="true" />}
              {publicSvc ? "public" : "private"}
            </Badge>
            {latest && <StatusDot tone={statusTone(latest.status)} label={latest.status} />}
          </div>
          {publicSvc ? (
            s.routeUrl ? (
              <a
                href={s.routeUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-1.5 flex max-w-full items-center gap-1.5 font-mono text-sm text-blue-400 hover:text-blue-300 hover:underline"
              >
                <span className="truncate">{s.routeUrl}</span>
                <ExternalLink className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              </a>
            ) : (
              <p className="mt-1.5 text-sm text-muted-foreground">Deploy this service to get a live URL.</p>
            )
          ) : (
            <p className="mt-1.5 font-mono text-sm text-muted-foreground">
              {internalUrl(s)} <span className="font-sans">— internal only, reachable by sibling services</span>
            </p>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-stretch gap-2 sm:flex-row sm:items-center">
          {(servers.data?.length ?? 0) > 1 && (
            <Select
              value={serverId}
              onChange={(e) => setServerOverride(e.target.value)}
              aria-label="Server"
              className="sm:w-52"
            >
              {(servers.data ?? []).map((srv) => (
                <option key={srv.id} value={srv.id}>
                  {srv.name} — {serverStatusLabel(srv.id, agents.data)}
                </option>
              ))}
            </Select>
          )}
          {publicSvc && s.routeUrl && (
            <a
              href={s.routeUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md bg-secondary px-3 text-sm font-medium text-secondary-foreground transition-colors hover:bg-secondary/80"
            >
              <ExternalLink className="h-4 w-4" aria-hidden="true" />
              Visit
            </a>
          )}
          <Button size="sm" disabled={redeploying || noServers || !serverId} onClick={redeploy}>
            <RefreshCw className="h-4 w-4" aria-hidden="true" />
            {redeploying ? "Starting…" : "Redeploy"}
          </Button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Source — where this service deploys from. */}
        <Panel>
          <PanelHeader title="Source" description="Where this service deploys from." />
          <div className="space-y-3 p-4 text-sm">
            <Row
              label="Kind"
              value={
                <span className="inline-flex items-center gap-1.5 text-foreground">
                  {isGit ? (
                    <GitFork className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                  ) : s.sourceKind === "template" ? (
                    <Sparkles className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                  ) : (
                    <Container className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                  )}
                  {s.sourceKind || "image"}
                </span>
              }
            />
            {isGit ? (
              <>
                <Row
                  label="Repository"
                  value={
                    s.htmlUrl ? (
                      <a
                        href={s.htmlUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 truncate font-mono text-foreground hover:underline"
                      >
                        {sourceLabel(s)}
                        <ExternalLink className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden="true" />
                      </a>
                    ) : (
                      <span className="truncate font-mono text-foreground">{sourceLabel(s)}</span>
                    )
                  }
                />
                <Row
                  label="Branch"
                  value={
                    <span className="inline-flex items-center gap-1 font-mono text-foreground">
                      <GitBranch className="h-3 w-3 text-muted-foreground" aria-hidden="true" />
                      {s.branch || s.defaultBranch || "default"}
                    </span>
                  }
                />
                {s.githubLogin && <Row label="Connected as" value={<span className="text-foreground">{s.githubLogin}</span>} />}
                {s.sourceAccess === "public" && (
                  <Row label="Access" value={<span className="text-muted-foreground">public repo</span>} />
                )}
              </>
            ) : (
              <Row label="Image" value={<span className="truncate font-mono text-foreground">{sourceLabel(s)}</span>} />
            )}
            <Row label="Port" value={<span className="font-mono text-foreground">{s.containerPort > 0 ? `:${s.containerPort}` : "auto"}</span>} />
            <Row
              label="Visibility"
              value={
                <Badge tone={publicSvc ? "green" : "purple"}>{publicSvc ? "public" : "private"}</Badge>
              }
            />
          </div>
        </Panel>

        {/* Env vars — non-secret per-service configuration. */}
        <EnvVarsPanel serviceId={id} />
      </div>

      <Panel>
        <PanelHeader title="Deployments" description="This service's deployment history." />
        {deployments.isLoading ? (
          <div className="p-4">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : rows.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="No deployments yet"
              body="Click Redeploy to run this service on a connected server and watch it go live."
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Deployment</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Host port</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((d) => (
                  <TableRow
                    key={d.id}
                    className="cursor-pointer"
                    onClick={() => navigate({ to: "/deployments/$deploymentId", params: { deploymentId: d.id } })}
                  >
                    <TableCell>
                      <p className="truncate font-mono text-sm font-medium text-foreground">{deploymentRefLabel(d)}</p>
                      <p className="text-xs text-muted-foreground">{d.id.slice(0, 8)}</p>
                    </TableCell>
                    <TableCell>
                      <StatusDot tone={statusTone(d.status)} label={d.status} />
                    </TableCell>
                    <TableCell className="font-mono text-muted-foreground">{d.hostPort > 0 ? `:${d.hostPort}` : "—"}</TableCell>
                    <TableCell className="text-muted-foreground">{timeAgo(d.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>
    </div>
  );
}

// EnvVarsPanel lists a service's non-secret env vars and lets you add/update/delete them
// (SetEnvVar upserts by key). Values are stored in plaintext and shown back.
function EnvVarsPanel({ serviceId }: { serviceId: string }) {
  const queryClient = useQueryClient();
  const envVars = useEnvVars(serviceId);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const rows = envVars.data ?? [];

  async function invalidate() {
    await queryClient.invalidateQueries({ queryKey: ["envVars", serviceId] });
  }

  async function onAdd(e: FormEvent) {
    e.preventDefault();
    const k = key.trim();
    if (!k || busy) return;
    setBusy(true);
    try {
      await envVarClient.setEnvVar({ serviceId, key: k, value });
      await invalidate();
      setKey("");
      setValue("");
      toast.success(`Saved ${k}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not save the variable");
    } finally {
      setBusy(false);
    }
  }

  async function onDelete(k: string) {
    try {
      await envVarClient.deleteEnvVar({ serviceId, key: k });
      await invalidate();
      toast.success(`Removed ${k}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not remove the variable");
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Environment variables"
        description="Non-secret configuration injected at run time. Applied on the next deploy."
      />
      <div className="space-y-4 p-4">
        <form onSubmit={onAdd} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1">
            <span className="mb-1.5 block text-xs font-medium text-foreground">Key</span>
            <Input value={key} onChange={(e) => setKey(e.target.value)} placeholder="PORT" autoCapitalize="none" spellCheck={false} />
          </div>
          <div className="min-w-0 flex-1">
            <span className="mb-1.5 block text-xs font-medium text-foreground">Value</span>
            <Input value={value} onChange={(e) => setValue(e.target.value)} placeholder="8080" autoCapitalize="none" spellCheck={false} />
          </div>
          <Button type="submit" size="sm" disabled={busy || !key.trim()}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            {busy ? "Saving…" : "Set"}
          </Button>
        </form>

        {envVars.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState title="No variables yet" body="Add a key/value above to configure this service." />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Key</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((v) => (
                  <TableRow key={v.id || v.key}>
                    <TableCell className="font-medium text-foreground">{v.key}</TableCell>
                    <TableCell className="max-w-[260px] truncate font-mono text-xs text-muted-foreground">{v.value}</TableCell>
                    <TableCell>
                      <Button size="sm" variant="ghost" aria-label={`Remove ${v.key}`} onClick={() => onDelete(v.key)}>
                        <Trash2 className="h-4 w-4" aria-hidden="true" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </Panel>
  );
}

// timeAgo renders a short relative time for an RFC 3339 timestamp.
function timeAgo(iso: string): string {
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right">{value}</span>
    </div>
  );
}

function BackLink({ projectId }: { projectId: string }) {
  if (projectId) {
    return (
      <Link
        to="/projects/$projectId"
        params={{ projectId }}
        className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to project
      </Link>
    );
  }
  return (
    <Link to="/projects" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to projects
    </Link>
  );
}
