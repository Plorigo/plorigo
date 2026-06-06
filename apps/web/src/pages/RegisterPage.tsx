import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

export function RegisterPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<null | { verify: boolean }>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await authClient.register({ email, password });
      // Registration never logs you in (so a new signup and an already-registered
      // email are indistinguishable). The user logs in next.
      setDone({ verify: res.emailVerificationRequired });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not sign up");
    } finally {
      setBusy(false);
    }
  }

  // Deliberately worded so the screen never confirms whether the address was new.
  if (done) {
    return (
      <AuthShell title="Almost there">
        <p className="text-sm text-gray-600">
          {done.verify
            ? `If ${email} is a new address, we've emailed a verification link. Verify it, then log in.`
            : `If ${email} is a new address, your account is ready — you can log in now.`}
        </p>
        <Link
          to="/login"
          className="mt-4 inline-block rounded bg-blue-600 px-3 py-2 text-white"
        >
          Go to log in
        </Link>
      </AuthShell>
    );
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
