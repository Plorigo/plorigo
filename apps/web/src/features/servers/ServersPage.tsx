import { useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Container, Cpu, Server } from "lucide-react";
import { toast } from "sonner";

import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Panel, Skeleton } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/cn";
import { useDemoData } from "@/lib/demo";
import { errorMessage, percentFromLabel } from "@/lib/format";
import { serverHealth } from "@/lib/mockDashboard";
import { agentClient, serverClient } from "@/lib/clients";
import { useAgents, useServers } from "@/lib/queries";
import { intentDot, intentSoft, statusIntent, statusTone, type Intent } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

import { ConnectServerDialog } from "./connect/ConnectServerDialog";
import { InstallCommandBlock } from "./connect/InstallCommand";
import { ServerCardActions } from "./ServerCardActions";
import { SetupStatusBadge } from "./SetupStatusBadge";

interface ServerRow {
  id?: string; // absent on demo fixtures; per-server actions render only when present
  name: string;
  region: string;
  cpu: string;
  memory: string;
  disk: string;
  status: string;
  // Deployment readiness derived by the control plane (ready | degraded | unavailable),
  // its plain-English reason, and the raw compatibility facts behind it.
  readiness?: string;
  readinessReason?: string;
  dockerVersion?: string;
  caddyVersion?: string;
  caddyRunning?: boolean; // undefined when the agent doesn't report extended facts
  os?: string;
  arch?: string;
  version?: string;
  lastSeen?: string;
}

interface InstallCommandResult {
  serverName: string;
  installCommand: string;
  expiresAt: string;
}

export function ServersPage() {
  const demo = useDemoData();
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);
  // Surface agent failures too: if only the agents query fails, we must not silently
  // render every server as "no agent" — that would misreport liveness.
  const error = errorMessage(servers.error) || errorMessage(agents.error);
  const loading = servers.isLoading || agents.isLoading;
  const [connectOpen, setConnectOpen] = useState(false);
  const [installResult, setInstallResult] = useState<InstallCommandResult | null>(null);
  const [mintingFor, setMintingFor] = useState("");
  // Managed setup runs started this session, by server id — drives the card's
  // "Setting up…" / "Setup failed" badge until the agent itself reports the server's state.
  const [managedRuns, setManagedRuns] = useState<Record<string, string>>({});

  const agentByServer = new Map((agents.data ?? []).map((a) => [a.serverId, a]));
  const liveRows: ServerRow[] = (servers.data ?? []).map((server) => {
    const agent = agentByServer.get(server.id);
    const base = { id: server.id, name: server.name, region: "Workspace server" };
    if (!agent) {
      return { ...base, cpu: "not reported", memory: "not reported", disk: "not reported", status: "no agent" };
    }
    // cpuCount > 0 marks an agent that reports the extended host facts (PLO-95); older
    // agents leave them zeroed, which we render as "not reported" rather than "0".
    const ext = agent.cpuCount > 0;
    return {
      ...base,
      cpu: ext ? `${agent.cpuCount} vCPU` : "not reported",
      memory:
        ext && agent.memTotalBytes > 0n
          ? fmtUsed(agent.memTotalBytes - agent.memAvailableBytes, agent.memTotalBytes)
          : "not reported",
      disk:
        ext && agent.diskTotalBytes > 0n
          ? fmtUsed(agent.diskTotalBytes - agent.diskFreeBytes, agent.diskTotalBytes)
          : "not reported",
      status: agent.status,
      readiness: agent.readiness,
      readinessReason: agent.readinessReason,
      dockerVersion: agent.dockerVersion,
      caddyVersion: ext ? agent.caddyVersion : undefined,
      caddyRunning: ext ? agent.caddyRunning : undefined,
      os: agent.os,
      arch: agent.arch,
      version: agent.agentVersion,
      lastSeen: agent.lastSeenAt,
    };
  });
  const rows: ServerRow[] = liveRows.length > 0 ? liveRows : demo ? serverHealth : [];

  // Mint a FRESH one-time token for an existing server and show its install command.
  // Tokens are single-use and shown once, so this is how you recover a command you
  // didn't copy; re-running install simply rotates the agent's credential.
  async function showInstallCommand(server: ServerRow) {
    if (!server.id) return;
    setMintingFor(server.id);
    try {
      const token = await agentClient.createRegistrationToken({ serverId: server.id });
      setInstallResult({
        serverName: server.name,
        installCommand: token.installCommand,
        expiresAt: token.expiresAt,
      });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not create an install command");
    } finally {
      setMintingFor("");
    }
  }

  async function deleteServer(server: ServerRow) {
    if (!server.id) return;
    try {
      await serverClient.deleteServer({ id: server.id });
      toast.success(`Server ${server.name} deleted`);
      await queryClient.invalidateQueries({ queryKey: ["servers", workspaceId] });
      await queryClient.invalidateQueries({ queryKey: ["agents", workspaceId] });
      // Its deployment history cascades away with it.
      await queryClient.invalidateQueries({ queryKey: ["deployments"] });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not delete the server");
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Servers"
        description="The machines you own and connect. Connect a server to install the agent — run a one-line command yourself, or let Plorigo prepare a fresh Ubuntu box over SSH. Each card shows whether it's ready to deploy."
        actions={
          <Button size="sm" disabled={!workspaceId} onClick={() => setConnectOpen(true)}>
            <Server className="h-4 w-4" aria-hidden="true" />
            Connect server
          </Button>
        }
      />

      <ConnectServerDialog
        workspaceId={workspaceId}
        open={connectOpen}
        onOpenChange={setConnectOpen}
        onManagedRun={(serverId, runId) => setManagedRuns((prev) => ({ ...prev, [serverId]: runId }))}
      />
      <InstallCommandDialog result={installResult} onClose={() => setInstallResult(null)} />

      {loading && (
        <div className="grid gap-4 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44 w-full" />
          ))}
        </div>
      )}

      {error && <EmptyState title="Couldn't load servers" body={error} />}

      {!loading && !error && rows.length === 0 && (
        <EmptyState
          title="No servers connected yet"
          body="Connect your first server to deploy apps. Run a one-line install command, or let Plorigo prepare a fresh server over SSH."
        />
      )}

      {!loading && !error && rows.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-3">
          {rows.map((server) => {
            const agentBadge = (
              <Badge tone={statusTone(server.readiness ?? server.status)} className="shrink-0 capitalize">
                {server.readiness ?? server.status}
              </Badge>
            );
            const trackedRun = server.id ? managedRuns[server.id] : undefined;
            return (
              <Panel key={server.id ?? server.name} className="p-4">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold text-foreground">{server.name}</p>
                    <p className="mt-1 truncate text-xs text-muted-foreground">{server.region}</p>
                  </div>
                  {trackedRun ? <SetupStatusBadge runId={trackedRun} fallback={agentBadge} /> : agentBadge}
                </div>
                {server.readinessReason && (
                  <p
                    className={cn(
                      "mt-3 rounded-md border px-3 py-2 text-xs leading-5",
                      intentSoft[statusIntent(server.readiness ?? "")],
                    )}
                  >
                    {server.readinessReason}
                  </p>
                )}
                <div className="mt-4 space-y-3">
                  <ResourceMeter label="CPU" value={server.cpu} intent="info" />
                  <ResourceMeter label="Memory" value={server.memory} intent="violet" />
                  <ResourceMeter label="Disk" value={server.disk} intent="success" />
                </div>
                {(server.os || server.dockerVersion) && (
                  <div className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Container className="h-4 w-4" aria-hidden="true" />
                    <span className="truncate">{dockerFactsLabel(server)}</span>
                  </div>
                )}
                <div className="mt-4 flex items-center justify-between border-t border-border pt-3">
                  <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Cpu className="h-4 w-4" aria-hidden="true" />
                    Agent {server.version ? server.version : "—"}
                  </span>
                  <span className="text-xs text-muted-foreground">{lastSeenLabel(server.lastSeen)}</span>
                </div>
                {server.id && (
                  <div className="mt-3">
                    <ServerCardActions
                      serverId={server.id}
                      serverName={server.name}
                      minting={mintingFor === server.id}
                      onInstallCommand={() => void showInstallCommand(server)}
                      onDelete={() => void deleteServer(server)}
                    />
                  </div>
                )}
              </Panel>
            );
          })}
        </div>
      )}
    </div>
  );
}

// InstallCommandDialog shows a freshly minted install command for an EXISTING server
// (the create flow has its own dialog). Closing it discards the shown token, but a new
// one can always be minted from the server card.
function InstallCommandDialog({
  result,
  onClose,
}: {
  result: InstallCommandResult | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={result !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Connect {result?.serverName}</DialogTitle>
          <DialogDescription>
            Run this command on the server to install (or re-connect) the Plorigo agent.
            Re-running install is safe: it rotates the agent&apos;s credential.
          </DialogDescription>
        </DialogHeader>
        {result && <InstallCommandBlock installCommand={result.installCommand} expiresAt={result.expiresAt} />}
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// dockerFactsLabel renders the raw compatibility facts behind a server's readiness, so the
// detail is one glance away (progressive disclosure). Shown only for health-reporting
// agents (those that set os); "Docker unavailable" when the daemon isn't reachable, plus the
// Caddy reverse-proxy state when the agent reports it (PLO-95).
function dockerFactsLabel(server: ServerRow): string {
  const docker = server.dockerVersion ? `Docker ${server.dockerVersion}` : "Docker unavailable";
  let caddy = "";
  if (server.caddyVersion) {
    caddy = `Caddy ${server.caddyVersion}${server.caddyRunning === false ? " (not running)" : ""}`;
  } else if (server.caddyRunning === false) {
    caddy = "Caddy not running";
  }
  const host = server.os ? `${server.os}${server.arch ? `/${server.arch}` : ""}` : "";
  return [docker, caddy, host].filter(Boolean).join(" · ");
}

// fmtUsed renders a "used / total GiB" label whose embedded fraction drives the meter bar
// (see percentFromLabel) — a fuller bar means more of the resource is in use.
function fmtUsed(usedBytes: bigint, totalBytes: bigint): string {
  const gib = (b: bigint) => (Number(b) / 1024 ** 3).toFixed(1);
  return `${gib(usedBytes)} / ${gib(totalBytes)} GiB`;
}

// lastSeenLabel renders a short relative time for an agent's last heartbeat.
function lastSeenLabel(iso?: string): string {
  if (!iso) return "never seen";
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}

function ResourceMeter({ label, value, intent }: { label: string; value: string; intent: Intent }) {
  const width = percentFromLabel(value);

  return (
    <div className="text-xs text-muted-foreground">
      <div className="mb-1 flex items-center justify-between gap-3">
        <span>{label}</span>
        <span className="truncate font-medium text-foreground">{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full", intentDot[intent])} style={{ width: `${width}%` }} />
      </div>
    </div>
  );
}
