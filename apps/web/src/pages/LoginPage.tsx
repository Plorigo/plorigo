import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";
import { Button, Input } from "../components/ui";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await authClient.login({ email, password });
      await queryClient.invalidateQueries({ queryKey: ["currentUser"] });
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
          <Link to="/register" className="font-medium text-blue-600 hover:text-blue-700">
            Sign up
          </Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <label className="block">
          <span className="mb-1.5 block text-sm font-medium text-zinc-800">Email</span>
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
          <span className="mb-1.5 block text-sm font-medium text-zinc-800">Password</span>
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
          <p className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </p>
        )}
        <Button className="w-full" type="submit" disabled={busy}>
          {busy ? "Logging in..." : "Log in"}
        </Button>
        <p className="text-sm">
          <Link to="/forgot" className="text-zinc-500 hover:text-zinc-900">
            Forgot password?
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}
