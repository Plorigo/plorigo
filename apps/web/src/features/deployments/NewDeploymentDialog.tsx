import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { deploymentClient } from "@/lib/clients";
import { useAgents, useEnvironments, useProjects, useServers } from "@/lib/queries";
import { pickDefaultServer, serverStatusLabel } from "@/lib/serverSelection";

// NewDeploymentDialog triggers a deployment: pick a project, environment, and connected
// server, give an image reference and the port it listens on, and the server's agent
// pulls and runs it. The mutation follows the imperative try/await/invalidate style used
// by the Connect-server dialog (the app has no useMutation wrapper).
export function NewDeploymentDialog({
  workspaceId,
  open,
  onOpenChange,
  defaultProjectId,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProjectId?: string;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const projects = useProjects(workspaceId);
  const servers = useServers(workspaceId);
  const agents = useAgents(workspaceId);

  const [projectId, setProjectId] = useState("");
  const [environmentId, setEnvironmentId] = useState("");
  const [serverId, setServerId] = useState("");
  const [imageRef, setImageRef] = useState("traefik/whoami");
  const [containerPort, setContainerPort] = useState("80");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const environments = useEnvironments(projectId);

  // Default the project when the dialog opens (preselected project wins).
  useEffect(() => {
    if (!open) return;
    if (defaultProjectId) setProjectId(defaultProjectId);
    else if (!projectId && projects.data?.length) setProjectId(projects.data[0].id);
  }, [open, defaultProjectId, projects.data, projectId]);

  // Keep the environment valid for the chosen project (default to its first).
  useEffect(() => {
    const envs = environments.data;
    if (!envs?.length) {
      setEnvironmentId("");
      return;
    }
    setEnvironmentId((cur) => (cur && envs.some((e) => e.id === cur) ? cur : envs[0].id));
  }, [environments.data]);

  // Default the server to a ready one (then any online, then the first server).
  useEffect(() => {
    if (serverId) return;
    const def = pickDefaultServer(servers.data, agents.data);
    if (def) setServerId(def.id);
  }, [servers.data, agents.data, serverId]);

  function reset() {
    setBusy(false);
    setError("");
    setImageRef("traefik/whoami");
    setContainerPort("80");
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const port = Number(containerPort);
    if (!environmentId) return setError("Pick an environment (create one on the project first)");
    if (!serverId) return setError("Pick a connected server");
    if (!imageRef.trim()) return setError("Enter an image reference, e.g. traefik/whoami");
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return setError("Container port must be between 1 and 65535");
    }
    setBusy(true);
    setError("");
    try {
      const { deployment } = await deploymentClient.createDeployment({
        environmentId,
        serverId,
        imageRef: imageRef.trim(),
        containerPort: port,
      });
      if (!deployment) throw new Error("the deployment was not created");
      await queryClient.invalidateQueries({ queryKey: ["deployments"] });
      onOpenChange(false);
      void navigate({ to: "/deployments/$deploymentId", params: { deploymentId: deployment.id } });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not start the deployment");
    } finally {
      setBusy(false);
    }
  }

  const noServers = !servers.isLoading && (servers.data?.length ?? 0) === 0;
  const noEnvironments = projectId.length > 0 && !environments.isLoading && (environments.data?.length ?? 0) === 0;

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
          <DialogTitle>New deployment</DialogTitle>
          <DialogDescription>
            Pull a public image and run it on a connected server. The server&apos;s agent pulls,
            starts, and health-checks the container, then publishes it on a host port.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="space-y-4">
          <Field label="Project">
            <Select value={projectId} onChange={(e) => setProjectId(e.target.value)}>
              {(projects.data ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </Field>

          <Field label="Environment">
            <Select
              value={environmentId}
              onChange={(e) => setEnvironmentId(e.target.value)}
              disabled={!environments.data?.length}
            >
              {(environments.data ?? []).map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name} ({env.type})
                </option>
              ))}
            </Select>
            {noEnvironments && (
              <p className="mt-1 text-xs text-muted-foreground">
                This project has no environments yet — add one on the project page first.
              </p>
            )}
          </Field>

          <Field label="Server">
            <Select
              value={serverId}
              onChange={(e) => setServerId(e.target.value)}
              disabled={!servers.data?.length}
            >
              {(servers.data ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} — {serverStatusLabel(s.id, agents.data)}
                </option>
              ))}
            </Select>
            {noServers && (
              <p className="mt-1 text-xs text-muted-foreground">
                No servers connected — connect one on the Servers page first.
              </p>
            )}
          </Field>

          <div className="grid grid-cols-[minmax(0,1fr)_120px] gap-3">
            <Field label="Image">
              <Input
                value={imageRef}
                onChange={(e) => setImageRef(e.target.value)}
                placeholder="traefik/whoami"
                spellCheck={false}
                required
              />
            </Field>
            <Field label="Container port">
              <Input
                value={containerPort}
                onChange={(e) => setContainerPort(e.target.value)}
                inputMode="numeric"
                placeholder="80"
                required
              />
            </Field>
          </div>

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="submit" disabled={busy || noServers}>
              {busy ? "Starting..." : "Deploy"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm font-medium text-foreground">{label}</span>
      {children}
    </label>
  );
}
