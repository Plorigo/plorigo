import type { ReactNode } from "react";
import { Navigate } from "@tanstack/react-router";

import { useCurrentUser } from "../lib/auth";

// Protected gates a route on authentication: it shows nothing while resolving,
// redirects to /login when logged out, and renders children when signed in.
export function Protected({ children }: { children: ReactNode }) {
  const { data: user, isLoading } = useCurrentUser();
  if (isLoading) {
    return <div className="p-8 text-gray-500">Loading…</div>;
  }
  if (!user) {
    return <Navigate to="/login" />;
  }
  return <>{children}</>;
}
