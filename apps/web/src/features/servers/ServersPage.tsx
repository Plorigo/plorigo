import { useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Container, Cpu, Server, TriangleAlert } from "lucide-react";
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
import { dockerFactsLabel, fleetSummary, lastSeenLabel, toServerRow, type ServerRow } from "./serverData";
import { SetupStatusBadge } from "./SetupStatusBadge";

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
  // The existing server whose SSH connection is being (re-)set up, or null when that dialog
  // is closed. Drives a second ConnectServerDialog in existing-server mode.
  const [setupServer, setSetupServer] = useState<{ id: string; name: string } | null>(null);
  const [installResult, setInstallResult] = useState<InstallCommandResult | null>(null);
  const [mintingFor, setMintingFor] = useState("");
  // Managed setup runs started this session, by server id — drives the card's
  // "Setting up…" / "Setup failed" badge until the agent itself reports the server's state.
  const [managedRuns, setManagedRuns] = useState<Record<string, string>>({});

  const agentByServer = new Map((agents.data ?? []).map((a) => [a.serverId, a]));
  const liveRows: ServerRow[] = (servers.data ?? []).map((server) =>
    toServerRow(server, agentByServer.get(server.id)),
  );
  const rows: ServerRow[] = liveRows.length > 0 ? liveRows : demo ? serverHealth : [];
  const summary = fleetSummary(rows);

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
      {/* Re-run / start SSH setup on an existing server. Keyed by server id so it remounts with a
          fresh form per server; shares the managed-run tracking so the card reflects progress. */}
      <ConnectServerDialog
        key={setupServer?.id ?? "none"}
        workspaceId={workspaceId}
        existingServer={setupServer ?? undefined}
        open={setupServer !== null}
        onOpenChange={(next) => !next && setSetupServer(null)}
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
        <FleetBanner summary={summary} />
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
              <Panel
                key={server.id ?? server.name}
                className={cn(
                  "relative p-4",
                  server.id &&
                    "cursor-pointer transition hover:-translate-y-0.5 hover:shadow-card-hover",
                )}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    {server.id ? (
                      <Link
                        to="/servers/$serverId"
                        params={{ serverId: server.id }}
                        className="truncate text-sm font-semibold text-foreground hover:underline before:absolute before:inset-0 before:rounded-xl focus-visible:outline-none focus-visible:before:ring-2 focus-visible:before:ring-ring"
                      >
                        {server.name}
                      </Link>
                    ) : (
                      <p className="truncate text-sm font-semibold text-foreground">{server.name}</p>
                    )}
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
                  <div className="relative z-10 mt-3">
                    <ServerCardActions
                      serverId={server.id}
                      serverName={server.name}
                      minting={mintingFor === server.id}
                      onSetup={() => setSetupServer({ id: server.id!, name: server.name })}
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

// FleetBanner leads the list with a one-glance health verdict so warnings are impossible to
// miss: green when every server is ready, amber when one or more need attention. The raw
// per-server detail stays in the cards (and the server detail page) below.
function FleetBanner({ summary }: { summary: ReturnType<typeof fleetSummary> }) {
  const ok = summary.attention === 0;
  const plural = (n: number) => (n === 1 ? "server" : "servers");
  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-md border px-3 py-2.5 text-sm leading-6",
        intentSoft[ok ? "success" : "warning"],
      )}
    >
      {ok ? (
        <CircleCheck className="h-4 w-4 shrink-0" aria-hidden="true" />
      ) : (
        <TriangleAlert className="h-4 w-4 shrink-0" aria-hidden="true" />
      )}
      <span>
        {ok
          ? `All ${summary.total} ${plural(summary.total)} ready to deploy${
              summary.online < summary.total ? ` · ${summary.online} online` : ""
            }.`
          : `${summary.attention} of ${summary.total} ${plural(summary.total)} need attention — open a server to see why.`}
      </span>
    </div>
  );
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
