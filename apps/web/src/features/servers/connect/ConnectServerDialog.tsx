import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";

import { Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { agentClient, serverClient, setupClient } from "@/lib/clients";

import { InstallCommandBlock } from "./InstallCommand";
import { SetupProgress } from "./SetupProgress";

type AuthMethod = "password" | "key";
type ManagedPhase = "form" | "progress";

// ConnectServerDialog offers two ways to connect a machine: run a one-line install command
// yourself, or let Plorigo prepare it over SSH. Both paths create at most ONE server record
// (reused across tabs and retries). The managed path's bootstrap credential is held only until
// StartSetup is called, then cleared from state.
//
// Pass `existingServer` to operate on a server that already exists (e.g. re-run SSH setup after
// changing its password, or set up over SSH a box first connected with a command). In that mode
// the dialog opens on the SSH tab, locks the name, and reuses the existing record instead of
// creating a new one — backend StartSetup is re-runnable (it reuses the pinned host key and
// re-provisions the management credential).
export function ConnectServerDialog({
  workspaceId,
  open,
  onOpenChange,
  onManagedRun,
  existingServer,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Reports the server + run id of a started managed bootstrap, so the page can reflect
  // "setting up" / "failed setup" on that server's card.
  onManagedRun: (serverId: string, runId: string) => void;
  // When set, the dialog edits this server's connection instead of creating a new one.
  existingServer?: { id: string; name: string };
}) {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<"manual" | "managed">(existingServer ? "managed" : "manual");
  const [name, setName] = useState(existingServer?.name ?? "");
  const [serverId, setServerId] = useState(existingServer?.id ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Manual path.
  const [manualResult, setManualResult] = useState<{ installCommand: string; expiresAt: string } | null>(null);

  // Managed path (SSH). Secrets live here only until StartSetup is called.
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [username, setUsername] = useState("root");
  const [authMethod, setAuthMethod] = useState<AuthMethod>("password");
  const [password, setPassword] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [phase, setPhase] = useState<ManagedPhase>("form");
  const [runId, setRunId] = useState("");

  function clearSecrets() {
    setPassword("");
    setPrivateKey("");
    setPassphrase("");
  }

  function reset() {
    // Keep the target server's identity in existing-server mode (only the create flow starts
    // from a blank slate); everything else returns to a fresh form.
    setTab(existingServer ? "managed" : "manual");
    setName(existingServer?.name ?? "");
    setServerId(existingServer?.id ?? "");
    setBusy(false);
    setError("");
    setManualResult(null);
    setHost("");
    setPort("22");
    setUsername("root");
    setAuthMethod("password");
    clearSecrets();
    setPhase("form");
    setRunId("");
  }

  function invalidateServers() {
    void queryClient.invalidateQueries({ queryKey: ["servers", workspaceId] });
    void queryClient.invalidateQueries({ queryKey: ["agents", workspaceId] });
  }

  function toMessage(err: unknown, fallback: string): string {
    return err instanceof ConnectError ? err.message : fallback;
  }

  // ensureServer creates the server record once and reuses it for retries and tab switches,
  // so falling back from managed to manual (or retrying) never leaves orphan servers.
  async function ensureServer(): Promise<string> {
    if (serverId) return serverId;
    const { server } = await serverClient.createServer({ workspaceId, name: name.trim() });
    if (!server) throw new Error("the server was not created");
    setServerId(server.id);
    return server.id;
  }

  async function mintCommand() {
    setBusy(true);
    setError("");
    try {
      const id = await ensureServer();
      const token = await agentClient.createRegistrationToken({ serverId: id });
      setManualResult({ installCommand: token.installCommand, expiresAt: token.expiresAt });
      invalidateServers();
    } catch (err) {
      setError(toMessage(err, "Could not generate an install command"));
    } finally {
      setBusy(false);
    }
  }

  async function onManualSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    await mintCommand();
  }

  async function onManagedSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim() || !host.trim() || !username.trim()) return;
    if (authMethod === "password" ? !password : !privateKey.trim()) return;
    setBusy(true);
    setError("");
    try {
      const id = await ensureServer();
      const { run } = await setupClient.startSetup({
        serverId: id,
        host: host.trim(),
        port: Number(port) || 22,
        username: username.trim(),
        password: authMethod === "password" ? password : "",
        privateKey: authMethod === "key" ? privateKey : "",
        privateKeyPassphrase: authMethod === "key" ? passphrase : "",
      });
      if (!run) throw new Error("the setup run did not start");
      setRunId(run.id);
      setPhase("progress");
      onManagedRun(id, run.id);
      invalidateServers();
    } catch (err) {
      setError(toMessage(err, "Could not start setup"));
    } finally {
      setBusy(false);
      // The bootstrap credential has done its only job (or the attempt failed) — never keep it
      // in client state past the request. Retry re-prompts for it.
      clearSecrets();
    }
  }

  function fallbackToManual() {
    setTab("manual");
    setPhase("form");
    void mintCommand();
  }

  const canSubmitManaged =
    name.trim() !== "" &&
    host.trim() !== "" &&
    username.trim() !== "" &&
    (authMethod === "password" ? password !== "" : privateKey.trim() !== "");

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) reset();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{existingServer ? `Set up ${existingServer.name}` : "Connect a server"}</DialogTitle>
          <DialogDescription>
            {existingServer
              ? "Re-run Plorigo's SSH setup on this server — for example after changing its password — or generate a fresh install command instead."
              : "Run a one-line command on the machine yourself, or let Plorigo prepare a fresh Ubuntu server for you over SSH."}
          </DialogDescription>
        </DialogHeader>

        <Tabs value={tab} onValueChange={(v) => setTab(v as "manual" | "managed")}>
          <TabsList className="w-full">
            <TabsTrigger value="manual">Run a command</TabsTrigger>
            <TabsTrigger value="managed">Plorigo sets it up</TabsTrigger>
          </TabsList>

          {/* Manual: name → install command. */}
          <TabsContent value="manual" className="mt-4">
            {manualResult ? (
              <div className="space-y-4">
                <p className="text-sm text-muted-foreground">
                  Run this on the machine you&apos;re connecting. It appears online here once the
                  agent connects.
                </p>
                <InstallCommandBlock
                  installCommand={manualResult.installCommand}
                  expiresAt={manualResult.expiresAt}
                />
                <DialogFooter>
                  <Button onClick={() => onOpenChange(false)}>Done</Button>
                </DialogFooter>
              </div>
            ) : (
              <form onSubmit={onManualSubmit} className="space-y-4">
                <Field label="Server name">
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="prod-1"
                    autoFocus
                    required
                    disabled={!!existingServer}
                  />
                </Field>
                {error && <ErrorNote>{error}</ErrorNote>}
                <DialogFooter>
                  <Button type="submit" disabled={busy || !name.trim()}>
                    {busy ? "Generating..." : "Generate install command"}
                  </Button>
                </DialogFooter>
              </form>
            )}
          </TabsContent>

          {/* Managed: SSH form → live progress. */}
          <TabsContent value="managed" className="mt-4">
            {phase === "progress" ? (
              <SetupProgress
                setupRunId={runId}
                serverName={name.trim()}
                onRetry={() => setPhase("form")}
                onFallbackManual={fallbackToManual}
                onClose={() => onOpenChange(false)}
                onSuccess={invalidateServers}
              />
            ) : (
              <form onSubmit={onManagedSubmit} className="space-y-4">
                <p className="text-sm text-muted-foreground">
                  Plorigo connects over SSH, installs Docker, Caddy, and the agent, and creates a
                  least-privilege management user. Your login is used once and never stored.
                </p>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Server name">
                    <Input
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="prod-1"
                      required
                      disabled={!!existingServer}
                    />
                  </Field>
                  <Field label="Host or IP">
                    <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="203.0.113.10" required />
                  </Field>
                  <Field label="SSH port">
                    <Input
                      value={port}
                      onChange={(e) => setPort(e.target.value)}
                      inputMode="numeric"
                      placeholder="22"
                    />
                  </Field>
                  <Field label="Username">
                    <Input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="root" required />
                  </Field>
                </div>
                <Field label="Authentication">
                  <Select value={authMethod} onChange={(e) => setAuthMethod(e.target.value as AuthMethod)}>
                    <option value="password">Password</option>
                    <option value="key">Private key</option>
                  </Select>
                </Field>
                {authMethod === "password" ? (
                  <Field label="Password">
                    <Input
                      type="password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      autoComplete="off"
                      placeholder="••••••••"
                    />
                  </Field>
                ) : (
                  <>
                    <Field label="Private key">
                      <textarea
                        value={privateKey}
                        onChange={(e) => setPrivateKey(e.target.value)}
                        rows={4}
                        spellCheck={false}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs text-foreground shadow-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                      />
                    </Field>
                    <Field label="Key passphrase (optional)">
                      <Input
                        type="password"
                        value={passphrase}
                        onChange={(e) => setPassphrase(e.target.value)}
                        autoComplete="off"
                        placeholder="leave blank if none"
                      />
                    </Field>
                  </>
                )}
                {error && <ErrorNote>{error}</ErrorNote>}
                <DialogFooter>
                  <Button type="submit" disabled={busy || !canSubmitManaged}>
                    {busy ? "Starting..." : "Prepare server"}
                  </Button>
                </DialogFooter>
              </form>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm font-medium text-foreground">{label}</span>
      {children}
    </label>
  );
}

function ErrorNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {children}
    </p>
  );
}
