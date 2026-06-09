import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Code, ConnectError } from "@connectrpc/connect";

import type { User } from "../gen/controlplane/v1/auth_pb";
import { useWorkspaceStore } from "../store";
import { authClient } from "./clients";

// useCurrentUser returns the signed-in user, or null when unauthenticated. An
// Unauthenticated response is "logged out", not an error to retry.
export function useCurrentUser() {
  return useQuery<User | null>({
    queryKey: ["currentUser"],
    queryFn: async () => {
      try {
        const res = await authClient.currentUser({});
        return res.user ?? null;
      } catch (err) {
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          return null;
        }
        throw err;
      }
    },
    retry: false,
  });
}

// Logs out, clears the selected workspace, drops cached queries, and returns to
// the login screen. Shared by the sidebar footer and the command palette.
export function useLogout() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);
  return async () => {
    await authClient.logout({});
    setWorkspaceId("");
    await queryClient.invalidateQueries();
    await navigate({ to: "/login" });
  };
}
