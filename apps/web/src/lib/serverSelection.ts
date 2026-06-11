import type { Agent } from "@/gen/controlplane/v1/agents_pb";
import type { Server } from "@/gen/controlplane/v1/servers_pb";

// Server-selection helpers shared by the deploy wizard and the deploy dialog. A
// server can run a deployment when its agent is online; readiness ("ready" |
// "degraded" | "unavailable") refines that with the reported Docker facts.

export function agentForServer(serverId: string, agents: Agent[] | undefined): Agent | undefined {
  return agents?.find((a) => a.serverId === serverId);
}

export function isServerOnline(serverId: string, agents: Agent[] | undefined): boolean {
  return agentForServer(serverId, agents)?.status === "online";
}

// Ready = online and not reporting a blocking readiness problem. An agent that
// predates readiness reporting (empty readiness) counts as ready when online,
// matching how the deploy dialog has always treated "online".
export function isServerReady(serverId: string, agents: Agent[] | undefined): boolean {
  const agent = agentForServer(serverId, agents);
  if (agent?.status !== "online") return false;
  return agent.readiness === "ready" || agent.readiness === "";
}

export function readyServers(servers: Server[] | undefined, agents: Agent[] | undefined): Server[] {
  return (servers ?? []).filter((s) => isServerReady(s.id, agents));
}

// Default to a ready server, then any online one, then the first connected server.
export function pickDefaultServer(
  servers: Server[] | undefined,
  agents: Agent[] | undefined,
): Server | undefined {
  if (!servers?.length) return undefined;
  return (
    readyServers(servers, agents)[0] ??
    servers.find((s) => isServerOnline(s.id, agents)) ??
    servers[0]
  );
}

// A short status label for a server option in a picker ("online", "ready",
// "degraded", "offline", or "no agent" when nothing has registered yet).
export function serverStatusLabel(serverId: string, agents: Agent[] | undefined): string {
  const agent = agentForServer(serverId, agents);
  if (!agent) return "no agent";
  if (agent.status === "online" && agent.readiness && agent.readiness !== "ready") {
    return agent.readiness;
  }
  return agent.status;
}
