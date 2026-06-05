import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

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
      footer={
        <>
          No account?{" "}
          <Link to="/register" className="text-blue-600">
            Sign up
          </Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="space-y-3">
        <input
          className="w-full rounded border border-gray-300 px-3 py-2"
          type="email"
          placeholder="Email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
        />
        <input
          className="w-full rounded border border-gray-300 px-3 py-2"
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {error && <p className="text-sm text-red-600">{error}</p>}
        <button
          className="w-full rounded bg-blue-600 px-3 py-2 text-white disabled:opacity-50"
          type="submit"
          disabled={busy}
        >
          {busy ? "…" : "Log in"}
        </button>
        <p className="text-sm">
          <Link to="/forgot" className="text-gray-500">
            Forgot password?
          </Link>
        </p>
      </form>
    </AuthShell>
  );
}
