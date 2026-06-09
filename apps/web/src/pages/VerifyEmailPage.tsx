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
        <Link to="/" className="font-medium text-blue-600 hover:text-blue-700">
          Go to dashboard
        </Link>
      }
    >
      {status === "verifying" && (
        <p className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-600">
          Verifying...
        </p>
      )}
      {status === "ok" && (
        <p className="rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
          Your email is verified.
        </p>
      )}
      {status === "error" && (
        <p className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {message}
        </p>
      )}
    </AuthShell>
  );
}
