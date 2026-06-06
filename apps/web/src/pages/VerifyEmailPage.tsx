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
      footer={
        <Link to="/" className="text-blue-600">
          Go to dashboard
        </Link>
      }
    >
      {status === "verifying" && <p className="text-sm text-gray-500">Verifying…</p>}
      {status === "ok" && <p className="text-sm text-green-700">Your email is verified.</p>}
      {status === "error" && <p className="text-sm text-red-600">{message}</p>}
    </AuthShell>
  );
}
