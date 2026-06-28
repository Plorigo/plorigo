import type { Agent } from "@/gen/controlplane/v1/agents_pb";
import type { Server } from "@/gen/controlplane/v1/servers_pb";
import { statusIntent, type Intent } from "@/lib/status";

// Shared server-health derivation used by both the Servers list and a single server's
// detail page, so the two surfaces agree on what "ready", "needs attention", and the raw
// facts mean. The control plane already derives readiness (ready | degraded | blocked |
// unknown) + a plain-English reason on each Agent; the dashboard only presents it.

export interface ServerRow {
  id?: string; // absent on demo fixtures; per-server actions render only when present
  name: string;
  region: string;
  cpu: string;
  memory: string;
  disk: string;
  status: string;
  // Deployment readiness derived by the control plane (ready | degraded | blocked |
  // unknown), its plain-English reason, and the raw compatibility facts behind it.
  readiness?: string;
  readinessReason?: string;
  dockerVersion?: string;
  caddyVersion?: string;
  caddyRunning?: boolean; // undefined when the agent doesn't report extended facts
  os?: string;
  arch?: string;
  version?: string;
  lastSeen?: string;
  // Raw extended host facts (present only when the agent reports them, i.e. cpuCount > 0).
  // Carried alongside the formatted strings so the detail page can color meters against the
  // same thresholds the control plane uses, without re-fetching.
  extended?: boolean;
  cpuCount?: number;
  memTotalBytes?: bigint;
  memAvailableBytes?: bigint;
  diskTotalBytes?: bigint;
  diskFreeBytes?: bigint;
}

// toServerRow folds a connected Server and its Agent heartbeat (if any) into the row shape the
// dashboard renders. A server with no agent yet reads as "no agent" with unreported resources.
export function toServerRow(server: Server, agent: Agent | undefined): ServerRow {
  const base = { id: server.id, name: server.name, region: "Workspace server" };
  if (!agent) {
    return { ...base, cpu: "not reported", memory: "not reported", disk: "not reported", status: "no agent" };
  }
  // cpuCount > 0 marks an agent that reports the extended host facts (PLO-95); older agents
  // leave them zeroed, which we render as "not reported" rather than "0".
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
    extended: ext,
    cpuCount: ext ? agent.cpuCount : undefined,
    memTotalBytes: ext ? agent.memTotalBytes : undefined,
    memAvailableBytes: ext ? agent.memAvailableBytes : undefined,
    diskTotalBytes: ext ? agent.diskTotalBytes : undefined,
    diskFreeBytes: ext ? agent.diskFreeBytes : undefined,
  };
}

// dockerFactsLabel renders the raw compatibility facts behind a server's readiness, so the
// detail is one glance away (progressive disclosure). Shown only for health-reporting agents
// (those that set os); "Docker unavailable" when the daemon isn't reachable, plus the Caddy
// reverse-proxy state when the agent reports it (PLO-95).
export function dockerFactsLabel(server: ServerRow): string {
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
export function fmtUsed(usedBytes: bigint, totalBytes: bigint): string {
  const gib = (b: bigint) => (Number(b) / 1024 ** 3).toFixed(1);
  return `${gib(usedBytes)} / ${gib(totalBytes)} GiB`;
}

// gib renders a single byte count in GiB to one decimal — for the detail page's facts.
export function gib(bytes: bigint): string {
  return `${(Number(bytes) / 1024 ** 3).toFixed(1)} GiB`;
}

// lastSeenLabel renders a short relative time for an agent's last heartbeat.
export function lastSeenLabel(iso?: string): string {
  if (!iso) return "never seen";
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}

// The same disk/memory thresholds the control plane derives readiness from
// (internal/agents/agents.go), so a meter turns amber/red exactly when readiness does.
const diskCriticalFreeBytes = 1n * 1024n ** 3n; // 1 GiB
const diskLowFreeBytes = 5n * 1024n ** 3n; // 5 GiB
const memLowAvailableBytes = 256n * 1024n ** 2n; // 256 MiB

// diskIntent / memoryIntent color a usage meter against those thresholds: red when a hard
// blocker is near, amber for a low-resource warning, green otherwise.
export function diskIntent(freeBytes: bigint, totalBytes: bigint): Intent {
  if (totalBytes <= 0n) return "neutral";
  if (freeBytes < diskCriticalFreeBytes) return "danger";
  if (freeBytes < diskLowFreeBytes) return "warning";
  return "success";
}

export function memoryIntent(availableBytes: bigint, totalBytes: bigint): Intent {
  if (totalBytes <= 0n) return "neutral";
  if (availableBytes < memLowAvailableBytes) return "warning";
  return "success";
}

// usedPercent is the integer used-fraction of a resource, for a meter's fill width.
export function usedPercent(usedBytes: bigint, totalBytes: bigint): number {
  if (totalBytes <= 0n) return 0;
  return Math.min(100, Math.round((Number(usedBytes) / Number(totalBytes)) * 100));
}

export interface FleetSummary {
  total: number;
  online: number;
  attention: number; // servers whose readiness/status is a warning or danger
}

// fleetSummary counts the workspace's servers by health so the list can lead with a one-glance
// banner ("N online · M need attention"). A row "needs attention" when its readiness (or, for
// agentless rows, its status) maps to a warning or danger intent.
export function fleetSummary(rows: ServerRow[]): FleetSummary {
  let online = 0;
  let attention = 0;
  for (const row of rows) {
    if (row.status === "online") online += 1;
    const intent = statusIntent(row.readiness ?? row.status);
    if (intent === "warning" || intent === "danger") attention += 1;
  }
  return { total: rows.length, online, attention };
}
