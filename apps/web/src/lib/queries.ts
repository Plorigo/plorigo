import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";

import {
  agentClient,
  authClient,
  deploymentClient,
  environmentClient,
  envVarClient,
  projectClient,
  secretClient,
  serverClient,
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

export function useEnvVars(environmentId: string) {
  return useQuery({
    queryKey: ["envVars", environmentId],
    queryFn: async () => (await envVarClient.listEnvVars({ environmentId })).envVars,
    enabled: environmentId.length > 0,
  });
}

export function useSecrets(environmentId: string) {
  return useQuery({
    queryKey: ["secrets", environmentId],
    queryFn: async () => (await secretClient.listSecrets({ environmentId })).secrets,
    enabled: environmentId.length > 0,
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

// useProjectSource returns the project's connected repository, or null when none is
// connected (the backend reports NotFound, which is the expected "not connected" state).
export function useProjectSource(projectId: string) {
  return useQuery({
    queryKey: ["projectSource", projectId],
    queryFn: async () => {
      try {
        return (await sourceClient.getProjectSource({ projectId })).source ?? null;
      } catch (err) {
        if (err instanceof ConnectError && err.code === Code.NotFound) return null;
        throw err;
      }
    },
    // Skip the live API for demo (prototype-*) ids — they aren't UUIDs, so the backend
    // would reject them; demo pages fall back to fixtures instead.
    enabled: projectId.length > 0 && !isPrototypeId(projectId),
  });
}

// useSourcesByWorkspace batches every project's source for the grid (avoids an N+1).
export function useSourcesByWorkspace(workspaceId: string) {
  return useQuery({
    queryKey: ["sources", workspaceId],
    queryFn: async () => (await sourceClient.listSourcesByWorkspace({ workspaceId })).sources,
    enabled: workspaceId.length > 0,
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
