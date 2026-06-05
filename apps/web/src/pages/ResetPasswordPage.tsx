import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

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
      footer={
        <Link to="/login" className="text-blue-600">
          Back to log in
        </Link>
      }
    >
      {token ? (
        <form onSubmit={onSubmit} className="space-y-3">
          <input
            className="w-full rounded border border-gray-300 px-3 py-2"
            type="password"
            placeholder="New password (at least 8 characters)"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            minLength={8}
            required
          />
          {error && <p className="text-sm text-red-600">{error}</p>}
          <button
            className="w-full rounded bg-blue-600 px-3 py-2 text-white disabled:opacity-50"
            type="submit"
            disabled={busy}
          >
            {busy ? "…" : "Reset password"}
          </button>
        </form>
      ) : (
        <p className="text-sm text-red-600">This reset link is missing its token.</p>
      )}
    </AuthShell>
  );
}
