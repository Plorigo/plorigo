import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    // The server always returns OK (it never reveals whether the email exists), so
    // there is no error branch to surface here.
    await authClient.requestPasswordReset({ email });
    setBusy(false);
    setSent(true);
  }

  return (
    <AuthShell
      title="Reset your password"
      footer={
        <Link to="/login" className="text-blue-600">
          Back to log in
        </Link>
      }
    >
      {sent ? (
        <p className="text-sm text-gray-600">
          If an account exists for {email}, we&apos;ve sent a reset link. Check your
          inbox (or, without SMTP configured, the control-plane logs).
        </p>
      ) : (
        <form onSubmit={onSubmit} className="space-y-3">
          <input
            className="w-full rounded border border-gray-300 px-3 py-2"
            type="email"
            placeholder="Email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
          <button
            className="w-full rounded bg-blue-600 px-3 py-2 text-white disabled:opacity-50"
            type="submit"
            disabled={busy}
          >
            {busy ? "…" : "Send reset link"}
          </button>
        </form>
      )}
    </AuthShell>
  );
}
