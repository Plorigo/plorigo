import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";
import { Button, Input } from "../components/ui";

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
      <AuthShell
        title="Almost there"
        description="Registration responses stay intentionally generic to protect account privacy."
      >
        <p className="text-sm leading-6 text-muted-foreground">
          {done.verify
            ? `If ${email} is a new address, we've emailed a verification link. Verify it, then log in.`
            : `If ${email} is a new address, your account is ready. You can log in now.`}
        </p>
        <Link
          to="/login"
          className="mt-5 inline-flex h-9 items-center justify-center rounded-md border border-primary bg-primary px-3 text-sm font-medium text-primary-foreground shadow-sm hover:bg-primary/90"
        >
          Go to log in
        </Link>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="Create your account"
      description="Start with a personal workspace, then connect servers and projects from the dashboard."
      footer={
        <>
          Already have an account?{" "}
          <Link to="/login" className="font-medium text-info hover:text-info/80">
            Log in
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
            placeholder="At least 8 characters"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            minLength={8}
            required
          />
        </label>
        {error && (
          <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}
        <Button className="w-full" type="submit" disabled={busy}>
          {busy ? "Creating account..." : "Sign up"}
        </Button>
      </form>
    </AuthShell>
  );
}
