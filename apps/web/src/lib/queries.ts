import { useQuery } from "@tanstack/react-query";

import {
  authClient,
  environmentClient,
  envVarClient,
  projectClient,
  secretClient,
  serverClient,
  workspaceClient,
} from "./clients";

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
    enabled: projectId.length > 0,
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
