import { useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowLeft,
  Container,
  Copy,
  Database,
  ExternalLink,
  GitBranch,
  GitFork,
  Globe,
  Info,
  Lock,
  Plus,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Timeline } from "@/components/Timeline";
import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton, StatusDot } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ConfigType } from "@/gen/controlplane/v1/config_pb";
import type { Deployment } from "@/gen/controlplane/v1/deployments_pb";
import type { Domain } from "@/gen/controlplane/v1/domains_pb";
import type { Service } from "@/gen/controlplane/v1/services_pb";
import { deploymentClient, domainClient, serviceClient } from "@/lib/clients";
import {
  isTerminalDeploymentStatus,
  useAgents,
  useConfig,
  useDeploymentEvents,
  useDeploymentsByService,
  useDomainsByService,
  useServers,
  useService,
} from "@/lib/queries";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { pickDefaultServer, serverStatusLabel } from "@/lib/serverSelection";
import { statusTone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import { deploymentRefLabel, deploymentTimeline } from "@/features/deployments/timeline";
import { BackupsPanel } from "./BackupsPanel";
import { internalUrl, isPublic, sourceLabel } from "./serviceData";

// ServiceDetailPage is the single service: its identity and live URL, where it deploys from,
// and its deployment history. Its variables and secrets are managed on the Environment Variables
// page (scoped to this service). The Redeploy button triggers a fresh
// deployment of the service onto a chosen server (CreateDeploymentForService); the control
// plane re-resolves the service's source so a private URL can't be smuggled through.
export function ServiceDetailPage() {
  const { projectId, serviceId } = useParams({ strict: false }) as {
    projectId?: string;
    serviceId?: string;
  };
  const id = serviceId ?? "";
  const scopedProjectId = useEffectiveProjectId();
  const pid = projectId ?? scopedProjectId;
  const navigate = useNavigate();

  const svc = useService(id);
  const deployments = useDeploymentsByService(id);

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
  // The card and header reflect what's live: the running deployment if there is one, else the
  // newest attempt (rows are newest-first). restorable is the most recent previously-healthy
  // version — the target for an instant rollback.
  const active = rows.find((d) => d.status === "running") ?? rows[0];
  const restorable = rows.find((d) => d.status === "superseded");
  const publicSvc = isPublic(s);

  return (
    <div className="space-y-6">
      <BackLink projectId={pid} />

      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{s.name}</h1>
          <Badge tone={publicSvc ? "green" : "purple"}>
            {publicSvc ? <Globe className="mr-1 h-3 w-3" aria-hidden="true" /> : <Lock className="mr-1 h-3 w-3" aria-hidden="true" />}
            {publicSvc ? "public" : "private"}
          </Badge>
          {active && <StatusDot tone={statusTone(active.status)} label={active.status} />}
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

      <CurrentDeploymentCard service={s} active={active} restorable={restorable} projectId={pid} />

      {/* Connection + backups — managed databases (template services) expose how to connect and
          can be snapshotted with pg_dump. */}
      {s.sourceKind === "template" && <ConnectionPanel service={s} />}
      {s.sourceKind === "template" && <BackupsPanel serviceId={s.id} />}

      <DomainsPanel service={s} />

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
                  <TableHead className="text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((d) => (
                  <TableRow
                    key={d.id}
                    className="cursor-pointer"
                    onClick={() =>
                      navigate({
                        to: "/projects/$projectId/deployments/$deploymentId",
                        params: { projectId: pid, deploymentId: d.id },
                      })
                    }
                  >
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="truncate font-mono text-sm font-medium text-foreground">{deploymentRefLabel(d)}</span>
                        {d.id === active?.id && <Badge tone="green">Current</Badge>}
                      </div>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">{deploymentSubline(d)}</p>
                    </TableCell>
                    <TableCell>
                      <StatusDot tone={statusTone(d.status)} label={d.status} />
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">{timeAgo(d.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <ServiceSettingsPanel service={s} projectId={pid} />
    </div>
  );
}

// CurrentDeploymentCard is the page's hero — Plorigo's take on Vercel's "Production Deployment"
// card. The header carries the service's actions (open the repo, instant-rollback to the last
// healthy version, redeploy onto a chosen server, visit the live URL). The body shows the active
// deployment two ways: a live phase Timeline on the left (the closest analog to Vercel's preview),
// and its key facts — deployment, domains, status, created, source — on the right.
function CurrentDeploymentCard({
  service,
  active,
  restorable,
  projectId,
}: {
  service: Service;
  active?: Deployment;
  restorable?: Deployment;
  projectId: string;
}) {
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);
  const domains = useDomainsByService(service.id);
  const [serverOverride, setServerOverride] = useState("");
  const [redeploying, setRedeploying] = useState(false);
  const [rollingBack, setRollingBack] = useState(false);

  const isGit = service.sourceKind === "git";
  const publicSvc = isPublic(service);
  const serverId = serverOverride || pickDefaultServer(servers.data, agents.data)?.id || "";
  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;

  // The phase Timeline reads from the deployment's status events; poll only while it's in flight.
  const live = active ? !isTerminalDeploymentStatus(active.status) : false;
  const events = useDeploymentEvents(active?.id ?? "", live);
  const steps = active ? deploymentTimeline(events.data ?? [], active.status, active.sourceKind) : [];

  // The generated route plus any custom domains already serving — summarized here, fully managed
  // in the Domains panel below.
  const visitUrl = active?.routeUrl || service.routeUrl;
  const liveDomains = (domains.data ?? []).filter((d) => d.status === "active" || d.status === "verified");

  async function redeploy() {
    if (!serverId) {
      toast.error("No connected server to deploy onto. Connect one first.");
      return;
    }
    setRedeploying(true);
    try {
      const { deployment } = await deploymentClient.createDeploymentForService({ serviceId: service.id, serverId });
      if (!deployment) throw new Error("the deployment was not created");
      void navigate({
        to: "/projects/$projectId/deployments/$deploymentId",
        params: { projectId, deploymentId: deployment.id },
      });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not start the deployment");
      setRedeploying(false);
    }
  }

  // Rolling back reproduces the previous healthy version's build as a new deployment that goes
  // through the normal health-check/route-switch flow, so the current release stays up until it
  // passes. Navigate to the new deployment to watch it.
  async function rollback() {
    if (!restorable) return;
    setRollingBack(true);
    try {
      const { deployment } = await deploymentClient.rollbackDeployment({ targetDeploymentId: restorable.id });
      if (!deployment) throw new Error("the rollback deployment was not created");
      toast.success("Rolling back to the previous version");
      void navigate({
        to: "/projects/$projectId/deployments/$deploymentId",
        params: { projectId, deploymentId: deployment.id },
      });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not start the rollback");
      setRollingBack(false);
    }
  }

  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      {isGit && service.htmlUrl && (
        <a
          href={service.htmlUrl}
          target="_blank"
          rel="noreferrer"
          className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md border border-border bg-card px-2.5 text-xs font-medium text-foreground shadow-sm transition hover:bg-accent"
        >
          <GitFork className="h-4 w-4" aria-hidden="true" />
          Repository
        </a>
      )}
      {restorable && (
        <ConfirmDialog
          trigger={
            <Button size="sm" variant="secondary" disabled={rollingBack}>
              <RotateCcw className="h-4 w-4" aria-hidden="true" />
              {rollingBack ? "Rolling back…" : "Instant Rollback"}
            </Button>
          }
          title="Roll back to the previous version?"
          description="This redeploys the most recent healthy version's exact build as a new deployment. It goes live only after it passes its health check."
          recovery="Your current release stays up until the rollback is healthy, and every version remains in the deployment history."
          confirmLabel="Roll back"
          onConfirm={rollback}
        />
      )}
      {(servers.data?.length ?? 0) > 1 && (
        <Select
          value={serverId}
          onChange={(e) => setServerOverride(e.target.value)}
          aria-label="Server"
          className="sm:w-44"
        >
          {(servers.data ?? []).map((srv) => (
            <option key={srv.id} value={srv.id}>
              {srv.name} — {serverStatusLabel(srv.id, agents.data)}
            </option>
          ))}
        </Select>
      )}
      <Button size="sm" disabled={redeploying || noServers || !serverId} onClick={redeploy}>
        <RefreshCw className="h-4 w-4" aria-hidden="true" />
        {redeploying ? "Starting…" : "Redeploy"}
      </Button>
      {publicSvc && visitUrl && (
        <a
          href={visitUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md bg-secondary px-3 text-xs font-medium text-secondary-foreground transition-colors hover:bg-secondary/80"
        >
          <ExternalLink className="h-4 w-4" aria-hidden="true" />
          Visit
        </a>
      )}
    </div>
  );

  return (
    <Panel>
      <PanelHeader title="Current Deployment" action={actions} />
      {!active ? (
        <div className="p-4">
          <EmptyState
            title="No deployments yet"
            body="Click Redeploy to run this service on a connected server and watch it go live."
          />
        </div>
      ) : (
        <>
          <div className="grid gap-6 p-4 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
            <div className="rounded-lg border border-border bg-background p-4">
              <Timeline steps={steps} />
            </div>
            <div className="space-y-4 text-sm">
              <Field label="Deployment">
                <Link
                  to="/projects/$projectId/deployments/$deploymentId"
                  params={{ projectId, deploymentId: active.id }}
                  className="block truncate font-mono text-foreground hover:underline"
                >
                  {deploymentRefLabel(active)}
                </Link>
              </Field>
              <Field label="Domains">
                {publicSvc ? (
                  visitUrl ? (
                    <div className="space-y-1">
                      <DomainLink url={visitUrl} />
                      {liveDomains.map((d) => (
                        <DomainLink key={d.id} url={`https://${d.hostname}`} label={d.hostname} />
                      ))}
                    </div>
                  ) : (
                    <span className="text-muted-foreground">Assigned after the first deploy</span>
                  )
                ) : (
                  <span className="block truncate font-mono text-muted-foreground">{internalUrl(service)}</span>
                )}
              </Field>
              <div className="grid grid-cols-2 gap-4">
                <Field label="Status">
                  <StatusDot tone={statusTone(active.status)} label={active.status} />
                </Field>
                <Field label="Created">
                  <span className="text-muted-foreground">{timeAgo(active.createdAt)}</span>
                </Field>
              </div>
              <Field label="Source">
                {isGit ? (
                  <div className="space-y-1">
                    <span className="inline-flex items-center gap-1.5 font-mono text-foreground">
                      <GitBranch className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                      {active.gitRef || service.branch || service.defaultBranch || "default"}
                    </span>
                    {active.commitSha && (
                      <span className="block font-mono text-xs text-muted-foreground">{active.commitSha.slice(0, 7)}</span>
                    )}
                  </div>
                ) : service.sourceKind === "template" ? (
                  <span className="inline-flex items-center gap-1.5 text-foreground">
                    <Sparkles className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                    {sourceLabel(service)}
                  </span>
                ) : (
                  <span className="inline-flex min-w-0 items-center gap-1.5 font-mono text-foreground">
                    <Container className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                    <span className="truncate">{active.imageRef || sourceLabel(service)}</span>
                  </span>
                )}
              </Field>
            </div>
          </div>
          {isGit && (
            <div className="flex items-center gap-2 border-t border-border px-4 py-2.5 text-xs text-muted-foreground">
              <Info className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              Plorigo doesn&rsquo;t auto-deploy on push yet — click Redeploy to ship the latest commit.
            </div>
          )}
        </>
      )}
    </Panel>
  );
}

// Field is one labeled metadata block in the current-deployment card's right column.
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <span className="block text-xs font-medium text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

// DomainLink is a compact, external-opening link to one of the service's live domains.
function DomainLink({ url, label }: { url: string; label?: string }) {
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="flex items-center gap-1.5 truncate font-mono text-xs text-blue-400 hover:text-blue-300 hover:underline"
    >
      <span className="truncate">{label ?? url}</span>
      <ExternalLink className="h-3 w-3 shrink-0" aria-hidden="true" />
    </a>
  );
}

function DomainsPanel({ service }: { service: Service }) {
  const queryClient = useQueryClient();
  const domains = useDomainsByService(service.id);
  const [open, setOpen] = useState(false);
  const [hostname, setHostname] = useState("");
  const [busy, setBusy] = useState(false);
  const [verifying, setVerifying] = useState("");
  const publicSvc = isPublic(service);
  const rows = domains.data ?? [];
  const generatedStatus = !publicSvc ? "Private service" : service.routeUrl ? "Active" : "Waiting for deployment";
  const generatedTone = !publicSvc ? "purple" : service.routeUrl ? "green" : "amber";

  async function invalidate() {
    await queryClient.invalidateQueries({ queryKey: ["domains", "service", service.id] });
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`Copied ${label}`);
    } catch {
      toast.error(`Could not copy ${label}`);
    }
  }

  async function onAdd(e: FormEvent) {
    e.preventDefault();
    const host = hostname.trim();
    if (!host || busy) return;
    setBusy(true);
    try {
      await domainClient.createDomain({ serviceId: service.id, hostname: host });
      await invalidate();
      setHostname("");
      setOpen(false);
      toast.success("Domain added");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not add the domain");
    } finally {
      setBusy(false);
    }
  }

  async function onVerify(domain: Domain) {
    setVerifying(domain.id);
    try {
      await domainClient.verifyDomain({ id: domain.id });
      await invalidate();
      toast.success("Domain checked");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not verify the domain");
    } finally {
      setVerifying("");
    }
  }

  async function onDelete(domain: Domain) {
    try {
      await domainClient.deleteDomain({ id: domain.id });
      await invalidate();
      toast.success(`Removed ${domain.hostname}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not remove the domain");
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Domains"
        description="Generated and custom hostnames for this service."
        action={
          <Button size="sm" onClick={() => setOpen(true)}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add domain
          </Button>
        }
      />
      <div className="space-y-4 p-4">
        <div className="rounded-lg border border-border bg-background px-3 py-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <Globe className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                <p className="text-sm font-medium text-foreground">Generated domain</p>
                <Badge tone={generatedTone}>{generatedStatus}</Badge>
              </div>
              {service.routeUrl ? (
                <a
                  href={service.routeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-1 block truncate font-mono text-xs text-blue-400 hover:text-blue-300 hover:underline"
                >
                  {service.routeUrl}
                </a>
              ) : (
                <p className="mt-1 text-xs text-muted-foreground">
                  {publicSvc ? "Deploy this service to create its generated domain." : "Private services do not receive public domains."}
                </p>
              )}
            </div>
            {service.routeUrl && (
              <div className="flex gap-2">
                <Button size="sm" variant="secondary" onClick={() => copy(service.routeUrl, "generated domain")}>
                  <Copy className="h-4 w-4" aria-hidden="true" />
                  Copy
                </Button>
                <a
                  href={service.routeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md border border-border bg-card px-2.5 text-xs font-medium text-foreground shadow-sm transition hover:bg-accent"
                >
                  <ExternalLink className="h-4 w-4" aria-hidden="true" />
                  Open
                </a>
              </div>
            )}
          </div>
        </div>

        {domains.isLoading ? (
          <Skeleton className="h-36 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No custom domains"
            body="Add a hostname, copy the DNS record to your provider, then verify it here."
          />
        ) : (
          <div className="overflow-x-auto">
            <Table className="min-w-[960px] table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[34%]">Domain</TableHead>
                  <TableHead className="w-[140px]">Status</TableHead>
                  <TableHead className="w-[260px]">DNS record</TableHead>
                  <TableHead className="w-[132px]">Last checked</TableHead>
                  <TableHead className="w-[172px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((domain) => (
                  <TableRow key={domain.id}>
                    <TableCell className="whitespace-normal align-top">
                      <p className="truncate font-mono text-sm font-medium text-foreground">{domain.hostname}</p>
                      {domain.statusMessage && (
                        <p className="mt-1 max-w-full break-words text-xs leading-5 text-muted-foreground">{domain.statusMessage}</p>
                      )}
                    </TableCell>
                    <TableCell className="align-top">
                      <StatusDot tone={domainTone(domain.status)} label={formatDomainStatus(domain.status)} />
                    </TableCell>
                    <TableCell className="align-top">
                      {domain.dnsRecordType && domain.dnsRecordValue ? (
                        <div className="space-y-1 font-mono text-xs">
                          <div className="flex items-center gap-2">
                            <Badge tone="blue">{domain.dnsRecordType}</Badge>
                            <span className="min-w-0 truncate text-muted-foreground">{domain.dnsRecordName}</span>
                          </div>
                          <button
                            type="button"
                            onClick={() => copy(domain.dnsRecordValue, "DNS value")}
                            className="flex w-full items-center gap-1 text-left text-foreground hover:underline"
                          >
                            <span className="min-w-0 truncate">{domain.dnsRecordValue}</span>
                            <Copy className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                          </button>
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">Waiting for a generated domain</span>
                      )}
                    </TableCell>
                    <TableCell className="align-top text-muted-foreground">
                      {domain.lastCheckedAt ? timeAgo(domain.lastCheckedAt) : "—"}
                    </TableCell>
                    <TableCell className="align-top">
                      <div className="flex justify-end gap-2">
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={verifying === domain.id}
                          onClick={() => onVerify(domain)}
                        >
                          <RefreshCw className="h-4 w-4" aria-hidden="true" />
                          {verifying === domain.id ? "Checking…" : "Verify"}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => onDelete(domain)} aria-label={`Remove ${domain.hostname}`}>
                          <Trash2 className="h-4 w-4" aria-hidden="true" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add custom domain</DialogTitle>
            <DialogDescription>Enter the hostname you want to route to this service.</DialogDescription>
          </DialogHeader>
          <form onSubmit={onAdd} className="space-y-4">
            <div>
              <span className="mb-1.5 block text-xs font-medium text-foreground">Domain</span>
              <Input
                value={hostname}
                onChange={(e) => setHostname(e.target.value)}
                placeholder="app.example.com"
                autoCapitalize="none"
                autoComplete="off"
                spellCheck={false}
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy || !hostname.trim()}>
                {busy ? "Adding…" : "Add domain"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </Panel>
  );
}

// ConnectionPanel shows how to connect to a managed database (a template service). It rebuilds
// the connection string from the service's config variables — POSTGRES_USER/PASSWORD/DB, written
// at provision time — plus the service's internal host and container port; siblings reach it at
// that host on the per-environment network. Each field has a copy control. Data is NOT yet
// persisted across redeploys (volumes are a later slice), so the panel flags it as ephemeral.
function ConnectionPanel({ service }: { service: Service }) {
  const config = useConfig(service.id);
  const entries = config.data ?? [];
  const get = (key: string) =>
    entries.find((e) => e.key === key && e.type === ConfigType.VARIABLE)?.value ?? "";
  const user = get("POSTGRES_USER");
  const password = get("POSTGRES_PASSWORD");
  const database = get("POSTGRES_DB");
  const host = service.internalHost;
  const port = service.containerPort;
  const ready = Boolean(user && password && database && host && port > 0);
  const connectionString = ready
    ? `postgresql://${user}:${password}@${host}:${port}/${database}`
    : "";

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`Copied ${label}`);
    } catch {
      toast.error(`Could not copy ${label}`);
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Connection"
        description="How sibling services connect to this managed database."
      />
      <div className="space-y-4 p-4">
        <div className="flex items-start gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2.5 text-xs leading-5 text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <p>
            Data is <span className="font-medium">not yet persisted across redeploys</span> — a redeploy starts a fresh,
            empty database. Persistent volumes are coming in a later release; treat this database as ephemeral for now.
          </p>
        </div>

        {config.isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : !ready ? (
          <EmptyState
            title="Connection details unavailable"
            body="Provision details appear once the database has its generated credentials. Try again after the first deployment."
          />
        ) : (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <ConnField label="Host" value={host} onCopy={() => copy(host, "host")} />
              <ConnField label="Port" value={String(port)} onCopy={() => copy(String(port), "port")} />
              <ConnField label="User" value={user} onCopy={() => copy(user, "user")} />
              <ConnField label="Database" value={database} onCopy={() => copy(database, "database")} />
            </div>
            <div>
              <span className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-foreground">
                <Database className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                Connection string
              </span>
              <button
                type="button"
                onClick={() => copy(connectionString, "connection string")}
                className="flex w-full items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-left font-mono text-xs text-foreground transition hover:bg-accent"
              >
                <span className="min-w-0 flex-1 truncate">{connectionString}</span>
                <Copy className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
              </button>
            </div>
          </>
        )}
      </div>
    </Panel>
  );
}

// ConnField is one labeled, copyable connection field (host/port/user/database).
function ConnField({ label, value, onCopy }: { label: string; value: string; onCopy: () => void }) {
  return (
    <div>
      <span className="mb-1.5 block text-xs font-medium text-foreground">{label}</span>
      <button
        type="button"
        onClick={onCopy}
        className="flex w-full items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-left font-mono text-xs text-foreground transition hover:bg-accent"
      >
        <span className="min-w-0 flex-1 truncate">{value}</span>
        <Copy className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      </button>
    </div>
  );
}

// ServiceSettingsPanel exposes the service-level controls that, until now, had RPCs but no UI:
// flip visibility between public and private, and delete the service. Both are reversible-aware —
// the visibility change is one click back, and the delete confirms first (it is not).
function ServiceSettingsPanel({ service, projectId }: { service: Service; projectId: string }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const publicSvc = isPublic(service);
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);

  async function setVisibility(next: "public" | "private") {
    setBusy(true);
    try {
      await serviceClient.updateServiceVisibility({ id: service.id, visibility: next });
      await queryClient.invalidateQueries({ queryKey: ["service", service.id] });
      toast.success(next === "public" ? "Service is now public" : "Service is now private");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not change the visibility");
    } finally {
      setBusy(false);
    }
  }

  async function onDelete() {
    setDeleting(true);
    try {
      await serviceClient.deleteService({ id: service.id });
      toast.success(`Deleted ${service.name}`);
      // Leave before the open service query refetches and 404s the page.
      if (projectId) {
        void navigate({ to: "/projects/$projectId", params: { projectId } });
        await queryClient.invalidateQueries({ queryKey: ["services", "project", projectId] });
      } else {
        void navigate({ to: "/projects" });
      }
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not delete the service");
      setDeleting(false);
    }
  }

  return (
    <Panel>
      <PanelHeader title="Settings" description="Control how this service is exposed, or remove it." />
      <div className="divide-y divide-border">
        <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">Visibility</p>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              {publicSvc
                ? "Public — served on its generated domain and any custom domains."
                : "Private — internal only, reachable by sibling services."}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Badge tone={publicSvc ? "green" : "purple"}>{publicSvc ? "public" : "private"}</Badge>
            {publicSvc ? (
              <ConfirmDialog
                trigger={
                  <Button size="sm" variant="secondary" disabled={busy}>
                    {busy ? "Saving…" : "Make private"}
                  </Button>
                }
                title="Make this service private?"
                description="It stops serving on its public URL and becomes reachable only by sibling services on the internal network."
                recovery="You can switch it back to public at any time."
                confirmLabel="Make private"
                onConfirm={() => setVisibility("private")}
              />
            ) : (
              <Button size="sm" variant="secondary" disabled={busy} onClick={() => setVisibility("public")}>
                {busy ? "Saving…" : "Make public"}
              </Button>
            )}
          </div>
        </div>
        <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">Delete service</p>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              Permanently remove this service and its deployment history. This cannot be undone.
            </p>
          </div>
          <ConfirmDialog
            trigger={
              <Button size="sm" variant="danger" disabled={deleting} className="shrink-0">
                <Trash2 className="h-4 w-4" aria-hidden="true" />
                {deleting ? "Deleting…" : "Delete service"}
              </Button>
            }
            title={`Delete ${service.name}?`}
            description="This permanently removes the service and its deployment history. Its running container is torn down on the next reconcile."
            confirmLabel="Delete service"
            onConfirm={onDelete}
          />
        </div>
      </div>
    </Panel>
  );
}

// deploymentSubline is the deployments-list secondary line: branch · short commit for a git
// deployment, falling back to the short id (also used for image deployments).
function deploymentSubline(d: Deployment): string {
  if (d.sourceKind === "git") {
    const parts = [d.gitRef, d.commitSha ? d.commitSha.slice(0, 7) : ""].filter(Boolean);
    if (parts.length > 0) return parts.join(" · ");
  }
  return d.id.slice(0, 8);
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

function domainTone(status: string) {
  if (status === "pending_dns" || status === "verified") return "blue";
  if (status === "blocked") return "amber";
  return statusTone(status);
}

function formatDomainStatus(status: string): string {
  return status.replaceAll("_", " ");
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
