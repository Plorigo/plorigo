import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";
import { Button, Input } from "../components/ui";

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
      description="Enter the email for your account. The response does not reveal whether the address exists."
      footer={
        <Link to="/login" className="font-medium text-blue-600 hover:text-blue-700">
          Back to log in
        </Link>
      }
    >
      {sent ? (
        <p className="rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm leading-6 text-emerald-800">
          If an account exists for {email}, we&apos;ve sent a reset link. Check your
          inbox (or, without SMTP configured, the control-plane logs).
        </p>
      ) : (
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
          <Button className="w-full" type="submit" disabled={busy}>
            {busy ? "Sending..." : "Send reset link"}
          </Button>
        </form>
      )}
    </AuthShell>
  );
}
