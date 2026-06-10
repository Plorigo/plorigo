import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Container, Cpu, Server, TerminalSquare, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Input, Panel, Skeleton } from "@/components/ui";
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

  const agentByServer = new Map((agents.data ?? []).map((a) => [a.serverId, a]));
  const liveRows: ServerRow[] = (servers.data ?? []).map((server) => {
    const agent = agentByServer.get(server.id);
    return {
      id: server.id,
      name: server.name,
      region: "Workspace server",
      cpu: "not reported",
      memory: "not reported",
      disk: "not reported",
      status: agent ? agent.status : "no agent",
      readiness: agent?.readiness,
      readinessReason: agent?.readinessReason,
      dockerVersion: agent?.dockerVersion,
      os: agent?.os,
      arch: agent?.arch,
      version: agent?.agentVersion,
      lastSeen: agent?.lastSeenAt,
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
        description="The machines you own and connect. Connect a server to install the agent; each card shows whether it's ready to deploy, with Docker and host details. Runtime metrics are on the way."
        actions={
          <Button size="sm" disabled={!workspaceId} onClick={() => setConnectOpen(true)}>
            <Server className="h-4 w-4" aria-hidden="true" />
            Connect server
          </Button>
        }
      />

      <ConnectServerDialog workspaceId={workspaceId} open={connectOpen} onOpenChange={setConnectOpen} />
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
          body="Connect your first server to deploy apps. You'll get a one-line install command for the agent."
        />
      )}

      {!loading && !error && rows.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-3">
          {rows.map((server) => (
            <Panel key={server.id ?? server.name} className="p-4">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{server.name}</p>
                  <p className="mt-1 truncate text-xs text-muted-foreground">{server.region}</p>
                </div>
                <Badge tone={statusTone(server.readiness ?? server.status)} className="shrink-0 capitalize">
                  {server.readiness ?? server.status}
                </Badge>
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
                <div className="mt-3 flex items-center justify-between gap-2 border-t border-border pt-3">
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={mintingFor === server.id}
                    onClick={() => void showInstallCommand(server)}
                  >
                    <TerminalSquare className="h-4 w-4" aria-hidden="true" />
                    {mintingFor === server.id ? "Generating..." : "Install command"}
                  </Button>
                  <ConfirmDialog
                    trigger={
                      <Button size="icon" variant="ghost" aria-label={`Delete server ${server.name}`}>
                        <Trash2 className="h-4 w-4" aria-hidden="true" />
                      </Button>
                    }
                    title={`Delete server ${server.name}?`}
                    description="This disconnects its agent and permanently removes the server, its agent registration, and its deployment history. Containers already running on the machine are not touched."
                    recovery="You can connect the machine again at any time — create a new server and run a fresh install command on it."
                    confirmLabel="Delete server"
                    onConfirm={() => void deleteServer(server)}
                  />
                </div>
              )}
            </Panel>
          ))}
        </div>
      )}
    </div>
  );
}

// InstallCommandBlock renders the one-time install command responsively: the command
// WRAPS (break-all) instead of stretching the dialog past the viewport on small
// screens, with the copy button as the primary affordance.
function InstallCommandBlock({ installCommand, expiresAt }: { installCommand: string; expiresAt: string }) {
  return (
    <div className="min-w-0 space-y-4">
      <pre className="min-w-0 max-w-full overflow-x-auto whitespace-pre-wrap break-all rounded-md border border-border bg-muted p-3 text-xs text-foreground">
        {installCommand}
      </pre>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="text-xs text-muted-foreground">
          One-time token; expires {new Date(expiresAt).toLocaleString()}.
        </span>
        <Button size="sm" variant="secondary" onClick={() => copy(installCommand)}>
          Copy command
        </Button>
      </div>
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

function ConnectServerDialog({
  workspaceId,
  open,
  onOpenChange,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{ installCommand: string; expiresAt: string } | null>(null);

  function reset() {
    setName("");
    setBusy(false);
    setError("");
    setResult(null);
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setBusy(true);
    setError("");
    try {
      // Create the server record, then mint a one-time registration token for it. The
      // install command embeds that token; the agent redeems it on first run.
      const { server } = await serverClient.createServer({ workspaceId, name: trimmed });
      if (!server) throw new Error("the server was not created");
      const token = await agentClient.createRegistrationToken({ serverId: server.id });
      setResult({ installCommand: token.installCommand, expiresAt: token.expiresAt });
      await queryClient.invalidateQueries({ queryKey: ["servers", workspaceId] });
      await queryClient.invalidateQueries({ queryKey: ["agents", workspaceId] });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not connect the server");
    } finally {
      setBusy(false);
    }
  }

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
          <DialogTitle>Connect a server</DialogTitle>
          <DialogDescription>
            {result
              ? "Run this command on the machine you're connecting. The server appears online here once the agent connects. If you lose the command, mint a new one from the server card."
              : "Name the machine you want to deploy onto. We'll generate an install command for the agent."}
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-4">
            <InstallCommandBlock installCommand={result.installCommand} expiresAt={result.expiresAt} />
            <DialogFooter>
              <Button onClick={() => onOpenChange(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="space-y-4">
            <label className="block">
              <span className="mb-1.5 block text-sm font-medium text-foreground">Server name</span>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="prod-1"
                autoFocus
                required
              />
            </label>
            {error && (
              <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button type="submit" disabled={busy || !name.trim()}>
                {busy ? "Generating..." : "Generate install command"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function copy(text: string) {
  void navigator.clipboard.writeText(text).then(
    () => toast.success("Install command copied"),
    () => toast.error("Could not copy to clipboard"),
  );
}

// dockerFactsLabel renders the raw compatibility facts behind a server's readiness, so the
// detail is one glance away (progressive disclosure). Shown only for health-reporting
// agents (those that set os); "Docker unavailable" when the daemon isn't reachable.
function dockerFactsLabel(server: ServerRow): string {
  const docker = server.dockerVersion ? `Docker ${server.dockerVersion}` : "Docker unavailable";
  const host = server.os ? `${server.os}${server.arch ? `/${server.arch}` : ""}` : "";
  return host ? `${docker} · ${host}` : docker;
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
