import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";
import { Button, Input } from "../components/ui";

// Dev-only convenience: prefill the form from a local, gitignored apps/web/.env.local
// (VITE_DEV_EMAIL / VITE_DEV_PASSWORD) so a seeded dev login is one click. Vite strips
// `import.meta.env.DEV` branches from production builds, so this never ships.
const devEmail = import.meta.env.DEV ? (import.meta.env.VITE_DEV_EMAIL ?? "") : "";
const devPassword = import.meta.env.DEV ? (import.meta.env.VITE_DEV_PASSWORD ?? "") : "";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState(devEmail);
  const [password, setPassword] = useState(devPassword);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await authClient.login({ email, password });
      // Seed the auth cache from the login response so the route guard sees an
      // authenticated user immediately. Without this it reads the stale
      // logged-out cache and redirects back to /login until a refetch lands —
      // the "have to click Log in twice" bug.
      queryClient.setQueryData(["currentUser"], res.user ?? null);
      await navigate({ to: "/" });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not log in");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell
      title="Log in"
      description="Use your Plorigo account to manage workspaces, projects, servers, and deployment settings."
      footer={
        <>
          No account?{" "}
          <Link to="/register" className="font-medium text-info hover:text-info/80">
            Sign up
          </Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <label className="block">
          <span className="mb-1.5 block text-sm font-medium text-foreground">Email</span>
          <Input
            type="email"
            placeholder="you@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-sm font-medium text-foreground">Password</span>
          <Input
            type="password"
            placeholder="Your password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        {error && (
          <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}
        <Button className="w-full" type="submit" disabled={busy}>
          {busy ? "Logging in..." : "Log in"}
        </Button>
        <p className="text-sm">
          <Link to="/forgot" className="text-muted-foreground hover:text-foreground">
            Forgot password?
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}
