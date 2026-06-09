import { Cpu, Server } from "lucide-react";

import { PageHeader } from "@/components/PageHeader";
import { Button, EmptyState, Panel, Skeleton, StatusDot } from "@/components/ui";
import { cn } from "@/lib/cn";
import { useDemoData } from "@/lib/demo";
import { errorMessage, percentFromLabel } from "@/lib/format";
import { serverHealth } from "@/lib/mockDashboard";
import { useServers } from "@/lib/queries";
import { intentDot, statusTone, type Intent } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

interface ServerRow {
  name: string;
  region: string;
  cpu: string;
  memory: string;
  disk: string;
  status: string;
}

export function ServersPage() {
  const demo = useDemoData();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const servers = useServers(workspaceId);
  const error = errorMessage(servers.error);

  const liveRows: ServerRow[] = (servers.data ?? []).map((server) => ({
    name: server.name,
    region: "Workspace server",
    cpu: "not reported",
    memory: "not reported",
    disk: "not reported",
    status: "online",
  }));
  const rows: ServerRow[] = liveRows.length > 0 ? liveRows : demo ? serverHealth : [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Servers"
        description="The machines you own and connect. Names load from the backend; runtime metrics are on the way."
        actions={
          <Button size="sm" disabled>
            <Server className="h-4 w-4" aria-hidden="true" />
            Connect server
          </Button>
        }
      />

      {servers.isLoading && (
        <div className="grid gap-4 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44 w-full" />
          ))}
        </div>
      )}

      {error && <EmptyState title="Server API returned an error" body={error} />}

      {!servers.isLoading && !error && rows.length === 0 && (
        <EmptyState
          title="No servers connected yet"
          body="Connect your first server to deploy apps. You'll get a one-line install command for the agent."
        />
      )}

      {!servers.isLoading && !error && rows.length > 0 && (
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
                <span className="text-xs text-muted-foreground">Agent</span>
                <Cpu className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
              </div>
            </Panel>
          ))}
        </div>
      )}
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
