import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";
import { Button, Input } from "../components/ui";

function tokenFromUrl(): string {
  return new URLSearchParams(window.location.search).get("token") ?? "";
}

export function ResetPasswordPage() {
  const navigate = useNavigate();
  const [token] = useState(tokenFromUrl);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await authClient.resetPassword({ token, newPassword: password });
      await navigate({ to: "/login" });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not reset password");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell
      title="Choose a new password"
      description="Set a new password with at least 8 characters. Reset links are single-use."
      footer={
        <Link to="/login" className="font-medium text-blue-600 hover:text-blue-700">
          Back to log in
        </Link>
      }
    >
      {token ? (
        <form onSubmit={onSubmit} className="space-y-4">
          <label className="block">
            <span className="mb-1.5 block text-sm font-medium text-zinc-800">New password</span>
            <Input
              type="password"
              placeholder="At least 8 characters"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              minLength={8}
              required
            />
          </label>
          {error && (
            <p className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </p>
          )}
          <Button className="w-full" type="submit" disabled={busy}>
            {busy ? "Resetting..." : "Reset password"}
          </Button>
        </form>
      ) : (
        <p className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          This reset link is missing its token.
        </p>
      )}
    </AuthShell>
  );
}
