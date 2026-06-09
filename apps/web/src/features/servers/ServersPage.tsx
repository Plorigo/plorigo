import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Cpu, Server } from "lucide-react";
import { toast } from "sonner";

import { PageHeader } from "@/components/PageHeader";
import { Button, EmptyState, Input, Panel, Skeleton, StatusDot } from "@/components/ui";
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
import { intentDot, statusTone, type Intent } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

interface ServerRow {
  name: string;
  region: string;
  cpu: string;
  memory: string;
  disk: string;
  status: string;
  version?: string;
  lastSeen?: string;
}

export function ServersPage() {
  const demo = useDemoData();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);
  // Surface agent failures too: if only the agents query fails, we must not silently
  // render every server as "no agent" — that would misreport liveness.
  const error = errorMessage(servers.error) || errorMessage(agents.error);
  const loading = servers.isLoading || agents.isLoading;
  const [connectOpen, setConnectOpen] = useState(false);

  const agentByServer = new Map((agents.data ?? []).map((a) => [a.serverId, a]));
  const liveRows: ServerRow[] = (servers.data ?? []).map((server) => {
    const agent = agentByServer.get(server.id);
    return {
      name: server.name,
      region: "Workspace server",
      cpu: "not reported",
      memory: "not reported",
      disk: "not reported",
      status: agent ? agent.status : "no agent",
      version: agent?.agentVersion,
      lastSeen: agent?.lastSeenAt,
    };
  });
  const rows: ServerRow[] = liveRows.length > 0 ? liveRows : demo ? serverHealth : [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Servers"
        description="The machines you own and connect. Connect a server to install the agent; it reports online once it's running. Runtime metrics are on the way."
        actions={
          <Button size="sm" disabled={!workspaceId} onClick={() => setConnectOpen(true)}>
            <Server className="h-4 w-4" aria-hidden="true" />
            Connect server
          </Button>
        }
      />

      <ConnectServerDialog workspaceId={workspaceId} open={connectOpen} onOpenChange={setConnectOpen} />

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
            <Panel key={server.name} className="p-4">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{server.name}</p>
                  <p className="mt-1 truncate text-xs text-muted-foreground">{server.region}</p>
                </div>
                <StatusDot tone={statusTone(server.status)} label={server.status} />
              </div>
              <div className="mt-4 space-y-3">
                <ResourceMeter label="CPU" value={server.cpu} intent="info" />
                <ResourceMeter label="Memory" value={server.memory} intent="violet" />
                <ResourceMeter label="Disk" value={server.disk} intent="success" />
              </div>
              <div className="mt-4 flex items-center justify-between border-t border-border pt-3">
                <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Cpu className="h-4 w-4" aria-hidden="true" />
                  Agent {server.version ? server.version : "—"}
                </span>
                <span className="text-xs text-muted-foreground">{lastSeenLabel(server.lastSeen)}</span>
              </div>
            </Panel>
          ))}
        </div>
      )}
    </div>
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
              ? "Run this one-line command on your server (a Linux host with Docker). It installs the Plorigo agent and registers it. The server appears online here once the agent connects."
              : "Name the machine you want to deploy onto. We'll generate a one-line install command for the agent."}
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-4">
            <pre className="overflow-x-auto rounded-md border border-border bg-muted p-3 text-xs text-foreground">
              {result.installCommand}
            </pre>
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">
                One-time token; expires {new Date(result.expiresAt).toLocaleString()}.
              </span>
              <Button size="sm" variant="secondary" onClick={() => copy(result.installCommand)}>
                Copy command
              </Button>
            </div>
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
