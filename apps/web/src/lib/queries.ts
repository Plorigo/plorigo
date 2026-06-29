import { useQuery } from "@tanstack/react-query";

import {
  agentClient,
  authClient,
  configClient,
  backupClient,
  deploymentClient,
  domainClient,
  environmentClient,
  projectClient,
  readinessClient,
  serverClient,
  setupClient,
  serviceClient,
  sourceClient,
  workspaceClient,
} from "./clients";
import { isPrototypeId } from "./mockDashboard";

// Centralized TanStack Query hooks so every feature page reads workspace-scoped
// data the same way (and shares the cache by query key). Server state lives here;
// purely-local UI state stays in Zustand stores.

export function useWorkspaces() {
  return useQuery({
    queryKey: ["workspaces"],
    queryFn: async () => (await workspaceClient.listMyWorkspaces({})).workspaces,
  });
}

export function useProjects(workspaceId: string) {
  return useQuery({
    queryKey: ["projects", workspaceId],
    queryFn: async () => (await projectClient.listProjectsByWorkspace({ workspaceId })).projects,
    enabled: workspaceId.length > 0,
  });
}

// useFrameworkDetection previews how a PUBLIC repo would build (the same internal/builder logic
// the agent runs), so the add-project flow can show the detected runtime, suggested port, and
// generated Dockerfile before deploying. A detection failure is non-fatal — the agent still
// detects at build time — so it doesn't retry and callers fall back gracefully.
export function useFrameworkDetection(repoUrl: string, branch: string) {
  return useQuery({
    queryKey: ["frameworkDetection", repoUrl, branch],
    queryFn: async () => await serviceClient.detectFramework({ repoUrl, branch }),
    enabled: repoUrl.length > 0,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

export function useServers(workspaceId: string) {
  return useQuery({
    queryKey: ["servers", workspaceId],
    queryFn: async () => (await serverClient.listServersByWorkspace({ workspaceId })).servers,
    enabled: workspaceId.length > 0,
  });
}

export function useAgents(workspaceId: string) {
  return useQuery({
    queryKey: ["agents", workspaceId],
    queryFn: async () => (await agentClient.listAgentsByWorkspace({ workspaceId })).agents,
    enabled: workspaceId.length > 0,
    // Online/offline is derived from the last heartbeat, so poll to keep it fresh.
    refetchInterval: 15_000,
  });
}

export function useMembers(workspaceId: string) {
  return useQuery({
    queryKey: ["members", workspaceId],
    queryFn: async () => (await workspaceClient.listMembers({ workspaceId })).members,
    enabled: workspaceId.length > 0,
  });
}

export function useApiTokens() {
  return useQuery({
    queryKey: ["apiTokens"],
    queryFn: async () => (await authClient.listAPITokens({})).apiTokens,
  });
}

export function useEnvironments(projectId: string) {
  return useQuery({
    queryKey: ["environments", projectId],
    queryFn: async () =>
      (await environmentClient.listEnvironmentsByProject({ projectId })).environments,
    // Skip the live API for demo (prototype-*) ids — they aren't UUIDs, so the backend
    // would reject them; demo pages fall back to fixtures instead.
    enabled: projectId.length > 0 && !isPrototypeId(projectId),
  });
}

export function useEnvironment(environmentId: string) {
  return useQuery({
    queryKey: ["environment", environmentId],
    queryFn: async () => (await environmentClient.getEnvironment({ id: environmentId })).environment ?? null,
    enabled: environmentId.length > 0 && !isPrototypeId(environmentId),
  });
}

// Unified config: variables (readable) and secrets (write-only, value blank) at service or
// environment scope. ListConfig is keyed by serviceId — the server returns the service's
// service-level entries plus its environment's shared entries.
export function useConfig(serviceId: string) {
  return useQuery({
    queryKey: ["config", serviceId],
    queryFn: async () => (await configClient.listConfig({ serviceId })).entries,
    enabled: serviceId.length > 0,
  });
}

// GitHub source integration. getConnection reports whether the server has OAuth
// configured and whether this workspace is connected, driving the import UI.
export function useGitHubConnection(workspaceId: string) {
  return useQuery({
    queryKey: ["githubConnection", workspaceId],
    queryFn: async () => sourceClient.getConnection({ workspaceId }),
    enabled: workspaceId.length > 0,
  });
}

// useRepositories lists the connected account's repositories (first page, ~100, newest
// first). Enable only once connected; the caller filters client-side for snappy search.
export function useRepositories(workspaceId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["githubRepos", workspaceId],
    queryFn: async () => (await sourceClient.listRepositories({ workspaceId })).repositories,
    enabled: enabled && workspaceId.length > 0,
  });
}

export function useBranches(workspaceId: string, owner: string, repo: string) {
  return useQuery({
    queryKey: ["githubBranches", workspaceId, owner, repo],
    queryFn: async () => (await sourceClient.listBranches({ workspaceId, owner, repo })).branches,
    enabled: workspaceId.length > 0 && owner.length > 0 && repo.length > 0,
  });
}

// Services — a project's deployable components (each owns a source, port, visibility,
// env vars, and deployment history). Listed by project/environment, or read by id.
export function useServicesByProject(projectId: string) {
  return useQuery({
    queryKey: ["services", "project", projectId],
    queryFn: async () => (await serviceClient.listServicesByProject({ projectId })).services,
    // Skip the live API for demo (prototype-*) ids — they aren't UUIDs, so the backend
    // would reject them; demo pages fall back to fixtures instead.
    enabled: projectId.length > 0 && !isPrototypeId(projectId),
  });
}

export function useServicesByWorkspace(workspaceId: string) {
  return useQuery({
    queryKey: ["services", "workspace", workspaceId],
    queryFn: async () => (await serviceClient.listServicesByWorkspace({ workspaceId })).services,
    enabled: workspaceId.length > 0,
  });
}

export function useServicesByEnvironment(environmentId: string) {
  return useQuery({
    queryKey: ["services", "environment", environmentId],
    queryFn: async () =>
      (await serviceClient.listServicesByEnvironment({ environmentId })).services,
    enabled: environmentId.length > 0 && !isPrototypeId(environmentId),
  });
}

export function useService(serviceId: string) {
  return useQuery({
    queryKey: ["service", serviceId],
    queryFn: async () => (await serviceClient.getService({ id: serviceId })).service ?? null,
    enabled: serviceId.length > 0 && !isPrototypeId(serviceId),
  });
}

// A deployment is terminal once it is running, failed, or superseded — at which point
// the detail view stops polling.
export function isTerminalDeploymentStatus(status: string): boolean {
  return status === "running" || status === "failed" || status === "superseded";
}

export function useDeploymentsByWorkspace(workspaceId: string) {
  return useQuery({
    queryKey: ["deployments", "workspace", workspaceId],
    queryFn: async () =>
      (await deploymentClient.listDeploymentsByWorkspace({ workspaceId })).deployments,
    enabled: workspaceId.length > 0,
    // Keep the list fresh as in-flight deployments progress.
    refetchInterval: 5000,
  });
}

export function useDeploymentsByProject(projectId: string) {
  return useQuery({
    queryKey: ["deployments", "project", projectId],
    queryFn: async () =>
      (await deploymentClient.listDeploymentsByProject({ projectId })).deployments,
    // Skip the live API for demo (prototype-*) ids — they aren't UUIDs, so the backend
    // would reject them; demo pages fall back to fixtures instead.
    enabled: projectId.length > 0 && !isPrototypeId(projectId),
    refetchInterval: 5000,
  });
}

export function useDeploymentsByService(serviceId: string) {
  return useQuery({
    queryKey: ["deployments", "service", serviceId],
    queryFn: async () =>
      (await deploymentClient.listDeploymentsByService({ serviceId })).deployments,
    enabled: serviceId.length > 0 && !isPrototypeId(serviceId),
    refetchInterval: 5000,
  });
}

// A backup is terminal once it has succeeded or failed; the panel stops polling then.
export function isTerminalBackupStatus(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

// A teardown is terminal once it has succeeded or failed.
export function isTerminalTeardownStatus(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

// useTeardownsByService lists a service's preview-teardown jobs (newest first), polling while any is
// still in flight so a "removing…" preview row updates live, then settling once it is gone or failed.
export function useTeardownsByService(serviceId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["teardowns", "service", serviceId],
    queryFn: async () => (await deploymentClient.listTeardownJobsByService({ serviceId })).teardowns,
    enabled: enabled && serviceId.length > 0 && !isPrototypeId(serviceId),
    refetchInterval: (query) => {
      const rows = query.state.data;
      const inFlight = rows?.some((t) => !isTerminalTeardownStatus(t.status)) ?? false;
      return inFlight ? 2000 : false;
    },
  });
}

// useBackupsByService lists a managed database service's backups (newest first), polling while any
// is still in flight so status transitions appear live.
export function useBackupsByService(serviceId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["backups", "service", serviceId],
    queryFn: async () => (await backupClient.listBackupsByService({ serviceId })).backups,
    enabled: enabled && serviceId.length > 0 && !isPrototypeId(serviceId),
    refetchInterval: (query) => {
      const rows = query.state.data;
      const inFlight = rows?.some((b) => !isTerminalBackupStatus(b.status)) ?? false;
      return inFlight ? 2000 : false;
    },
  });
}

// useServiceReadiness fetches the Production Readiness Doctor's checklist for a service: a
// deterministic, on-demand verdict (ready | almost_ready | not_ready) plus per-check detail and
// remediation. It refetches when deployment/config/server state may have changed (the service
// page already polls deployments), so a short staleTime keeps it cheap without going stale.
export function useServiceReadiness(serviceId: string) {
  return useQuery({
    queryKey: ["readiness", "service", serviceId],
    queryFn: async () =>
      (await readinessClient.getServiceReadiness({ serviceId })).checklist ?? null,
    enabled: serviceId.length > 0 && !isPrototypeId(serviceId),
    staleTime: 10_000,
  });
}

// A restore is terminal once it has succeeded or failed.
export function isTerminalRestoreStatus(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

// useRestoresByService lists a managed database service's restore jobs (newest first), polling
// while any is in flight.
export function useRestoresByService(serviceId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["restores", "service", serviceId],
    queryFn: async () => (await backupClient.listRestoreJobsByService({ serviceId })).restores,
    enabled: enabled && serviceId.length > 0 && !isPrototypeId(serviceId),
    refetchInterval: (query) => {
      const rows = query.state.data;
      const inFlight = rows?.some((r) => !isTerminalRestoreStatus(r.status)) ?? false;
      return inFlight ? 2000 : false;
    },
  });
}

export function useDomainsByService(serviceId: string) {
  return useQuery({
    queryKey: ["domains", "service", serviceId],
    queryFn: async () => (await domainClient.listDomainsByService({ serviceId })).domains,
    enabled: serviceId.length > 0 && !isPrototypeId(serviceId),
  });
}

export function useDomainsByProject(projectId: string) {
  return useQuery({
    queryKey: ["domains", "project", projectId],
    queryFn: async () => (await domainClient.listDomainsByProject({ projectId })).domains,
    enabled: projectId.length > 0 && !isPrototypeId(projectId),
  });
}

export function useDomainsByWorkspace(workspaceId: string) {
  return useQuery({
    queryKey: ["domains", "workspace", workspaceId],
    queryFn: async () => (await domainClient.listDomainsByWorkspace({ workspaceId })).domains,
    enabled: workspaceId.length > 0,
  });
}

export function useDeployment(deploymentId: string) {
  return useQuery({
    queryKey: ["deployment", deploymentId],
    queryFn: async () => (await deploymentClient.getDeployment({ id: deploymentId })).deployment ?? null,
    enabled: deploymentId.length > 0,
    // Poll while the deployment is in flight; stop once it reaches a terminal status.
    refetchInterval: (query) => {
      const d = query.state.data;
      return d && isTerminalDeploymentStatus(d.status) ? false : 2000;
    },
  });
}

export function useDeploymentEvents(deploymentId: string, live: boolean) {
  return useQuery({
    queryKey: ["deploymentEvents", deploymentId],
    queryFn: async () =>
      (await deploymentClient.listDeploymentEvents({ deploymentId })).events,
    enabled: deploymentId.length > 0,
    refetchInterval: live ? 2000 : false,
  });
}

// --- Dashboard-managed server setup (SSH bootstrap) ---

// A setup run is terminal once it succeeds or fails; the progress view stops polling then.
export function isTerminalSetupStatus(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

export function useSetupRun(setupRunId: string) {
  return useQuery({
    queryKey: ["setupRun", setupRunId],
    queryFn: async () => (await setupClient.getSetupRun({ setupRunId })).run ?? null,
    enabled: setupRunId.length > 0,
    // Poll while the bootstrap is in flight; stop once it reaches a terminal status.
    refetchInterval: (query) => {
      const run = query.state.data;
      return run && isTerminalSetupStatus(run.status) ? false : 2000;
    },
  });
}

// Ordered, redacted status/log lines for a run. We refetch the full list while live (runs
// are short); the server guarantees seq order, so the UI renders them as-is.
export function useSetupEvents(setupRunId: string, live: boolean) {
  return useQuery({
    queryKey: ["setupEvents", setupRunId],
    queryFn: async () => (await setupClient.listSetupEvents({ setupRunId, afterSeq: 0n })).events,
    enabled: setupRunId.length > 0,
    refetchInterval: live ? 2000 : false,
  });
}

// The persistent SSH management key's NON-SECRET metadata (fingerprint, rotation state).
// Returns null when the server has no managed key (set up manually); never the private key.
export function useManagementKey(serverId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["managementKey", serverId],
    queryFn: async () => (await setupClient.getManagementKey({ serverId })).key ?? null,
    enabled: enabled && serverId.length > 0,
    retry: false,
  });
}
