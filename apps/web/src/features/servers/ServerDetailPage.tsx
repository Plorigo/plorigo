import type { ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Container, Cpu, HardDrive, MemoryStick, Server as ServerIcon } from "lucide-react";

import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Badge, EmptyState, Panel, PanelHeader, Skeleton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { errorMessage } from "@/lib/format";
import { useAgents, useServers } from "@/lib/queries";
import { intentSoft, statusIntent, statusTone, type Intent } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

import {
  diskIntent,
  dockerFactsLabel,
  gib,
  lastSeenLabel,
  memoryIntent,
  toServerRow,
  usedPercent,
} from "./serverData";

// ServerDetailPage is a single server's health view — the place a user decides whether it's
// safe to deploy onto, without opening an SSH session. It leads with the control plane's
// readiness verdict and its plain-English reason, then the resource meters (colored against
// the same thresholds readiness uses) and the raw compatibility facts (progressive disclosure).
export function ServerDetailPage() {
  const { serverId } = useParams({ strict: false }) as { serverId?: string };
  const id = serverId ?? "";
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);

  const error = errorMessage(servers.error) || errorMessage(agents.error);
  const loading = servers.isLoading || agents.isLoading;

  const server = (servers.data ?? []).find((s) => s.id === id);
  const agent = (agents.data ?? []).find((a) => a.serverId === id);

  if (loading && !server) {
    return (
      <div className="space-y-6">
        <BackLink />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState title="Couldn't load this server" body={error} />
      </div>
    );
  }

  if (!server) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState title="Server not found" body="It may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const row = toServerRow(server, agent);
  const verdict = row.readiness ?? row.status;

  return (
    <div className="space-y-6">
      <BackLink />

      <PageHeader
        title={row.name}
        description="Whether this server is ready to deploy onto — and the facts behind that verdict."
        actions={
          <Badge tone={statusTone(verdict)} className="shrink-0 capitalize">
            {verdict}
          </Badge>
        }
      />

      {row.readinessReason && (
        <p
          className={cn(
            "rounded-md border px-3 py-2.5 text-sm leading-6",
            intentSoft[statusIntent(verdict)],
          )}
        >
          {row.readinessReason}
        </p>
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Liveness"
          value={<span className="capitalize">{row.status}</span>}
          detail={lastSeenLabel(row.lastSeen)}
          icon={ServerIcon}
          intent={statusIntent(row.status)}
          accentBar
        />
        <StatCard
          label="CPU"
          value={row.extended && row.cpuCount ? `${row.cpuCount} vCPU` : "—"}
          detail={row.extended ? "Reported by the agent" : "Not reported"}
          icon={Cpu}
          intent={row.extended ? "info" : "neutral"}
        />
        <MeterStat
          label="Memory"
          icon={MemoryStick}
          total={row.memTotalBytes}
          available={row.memAvailableBytes}
          intent={
            row.memTotalBytes !== undefined && row.memAvailableBytes !== undefined
              ? memoryIntent(row.memAvailableBytes, row.memTotalBytes)
              : "neutral"
          }
        />
        <MeterStat
          label="Disk"
          icon={HardDrive}
          total={row.diskTotalBytes}
          available={row.diskFreeBytes}
          intent={
            row.diskTotalBytes !== undefined && row.diskFreeBytes !== undefined
              ? diskIntent(row.diskFreeBytes, row.diskTotalBytes)
              : "neutral"
          }
        />
      </div>

      <Panel>
        <PanelHeader
          title="Compatibility"
          description="The runtime facts the agent reports each heartbeat — the raw detail behind the readiness verdict."
        />
        <dl className="divide-y divide-border">
          <Fact label="Runtime">
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <Container className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              <span className="truncate">{dockerFactsLabel(row)}</span>
            </span>
          </Fact>
          <Fact label="Operating system">{row.os ? `${row.os}${row.arch ? ` · ${row.arch}` : ""}` : "Not reported"}</Fact>
          <Fact label="Agent version">{row.version || "—"}</Fact>
          <Fact label="Last heartbeat">{lastSeenLabel(row.lastSeen)}</Fact>
        </dl>
      </Panel>

      {!row.extended && row.status !== "no agent" && (
        <p className="text-xs leading-6 text-muted-foreground">
          This agent doesn&apos;t report extended host facts (CPU, memory, disk) yet. Update the agent to see full
          resource health here.
        </p>
      )}
    </div>
  );
}

// MeterStat is a resource StatCard with a usage bar underneath — used for memory and disk,
// where "used / total" and a colored fill convey pressure at a glance.
function MeterStat({
  label,
  icon,
  total,
  available,
  intent,
}: {
  label: string;
  icon: typeof HardDrive;
  total?: bigint;
  available?: bigint;
  intent: Intent;
}) {
  if (total === undefined || available === undefined || total <= 0n) {
    return <StatCard label={label} value="—" detail="Not reported" icon={icon} intent="neutral" />;
  }
  const used = total - available;
  const pct = usedPercent(used, total);
  return (
    <StatCard
      label={label}
      value={`${pct}%`}
      detail={`${gib(used)} of ${gib(total)} used`}
      icon={icon}
      intent={intent}
      accentBar
    />
  );
}

// Fact is one labeled row in the compatibility list.
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 py-3 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate text-right font-medium text-foreground">{children}</dd>
    </div>
  );
}

function BackLink() {
  return (
    <Link
      to="/servers"
      className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to servers
    </Link>
  );
}
