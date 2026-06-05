import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

export function RegisterPage() {
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
      await authClient.register({ email, password });
      await queryClient.invalidateQueries({ queryKey: ["currentUser"] });
      await navigate({ to: "/" });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not sign up");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell
      title="Create your account"
      footer={
        <>
          Already have an account?{" "}
          <Link to="/login" className="text-blue-600">
            Log in
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
          placeholder="Password (at least 8 characters)"
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
          {busy ? "…" : "Sign up"}
        </button>
      </form>
    </AuthShell>
  );
}
