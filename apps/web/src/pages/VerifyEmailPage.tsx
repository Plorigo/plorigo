import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ConnectError } from "@connectrpc/connect";

import { authClient } from "../lib/clients";
import { AuthShell } from "../components/AuthShell";

type Status = "verifying" | "ok" | "error";

export function VerifyEmailPage() {
  const [status, setStatus] = useState<Status>("verifying");
  const [message, setMessage] = useState("");

  useEffect(() => {
    const token = new URLSearchParams(window.location.search).get("token") ?? "";
    if (!token) {
      setStatus("error");
      setMessage("This verification link is missing its token.");
      return;
    }
    authClient
      .verifyEmail({ token })
      .then(() => setStatus("ok"))
      .catch((err) => {
        setStatus("error");
        setMessage(err instanceof ConnectError ? err.message : "Verification failed");
      });
  }, []);

  return (
    <AuthShell
      title="Verify email"
      description="Email verification links are single-use and complete the account safety check."
      footer={
        <Link to="/" className="font-medium text-info hover:text-info/80">
          Go to dashboard
        </Link>
      }
    >
      {status === "verifying" && (
        <p className="rounded-md border border-border bg-muted px-3 py-2 text-sm text-muted-foreground">
          Verifying...
        </p>
      )}
      {status === "ok" && (
        <p className="rounded-md border border-success/20 bg-success/10 px-3 py-2 text-sm text-success">
          Your email is verified.
        </p>
      )}
      {status === "error" && (
        <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {message}
        </p>
      )}
    </AuthShell>
  );
}
