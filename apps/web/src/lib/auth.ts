import { useQuery } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";

import type { User } from "../gen/controlplane/v1/auth_pb";
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
