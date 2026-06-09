import { useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";

import { Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { environmentClient, projectClient } from "@/lib/clients";

// The environment type vocabulary, matching the backend CHECK constraint.
const ENV_TYPES = ["production", "staging", "preview", "custom"] as const;

// NewProjectDialog creates a project AND its first environment in one step, so the
// project is immediately deployable (the deploy dialog requires an environment).
// Mutations follow the imperative try/await/invalidate style used by the other dialogs.
export function NewProjectDialog({
  workspaceId,
  open,
  onOpenChange,
}: {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [envName, setEnvName] = useState("Production");
  const [envType, setEnvType] = useState<string>("production");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function reset() {
    setName("");
    setEnvName("Production");
    setEnvType("production");
    setBusy(false);
    setError("");
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const projectName = name.trim();
    const environmentName = envName.trim();
    if (!projectName || !environmentName) return;
    setBusy(true);
    setError("");

    let projectId = "";
    try {
      const { project } = await projectClient.createProject({ workspaceId, name: projectName });
      if (!project) throw new Error("the project was not created");
      projectId = project.id;
      await queryClient.invalidateQueries({ queryKey: ["projects", workspaceId] });
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not create the project");
      setBusy(false);
      return;
    }

    try {
      await environmentClient.createEnvironment({ projectId, name: environmentName, type: envType });
      await queryClient.invalidateQueries({ queryKey: ["environments", projectId] });
    } catch (err) {
      // The project exists; only its environment failed. Land on the project page,
      // where an environment can be added — and say exactly that.
      const detail = err instanceof ConnectError ? err.message : "unknown error";
      toast.error(`Project created, but its environment failed: ${detail}. Add one from the project page.`);
      onOpenChange(false);
      reset();
      void navigate({ to: "/projects/$projectId", params: { projectId } });
      return;
    }

    toast.success(`Project ${projectName} created`);
    onOpenChange(false);
    reset();
    void navigate({ to: "/projects/$projectId", params: { projectId } });
  }

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
          <DialogTitle>New project</DialogTitle>
          <DialogDescription>
            A project groups the environments an app deploys to. It starts with one
            environment so you can deploy right away.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="space-y-4">
          <Field label="Project name">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-app"
              autoFocus
              required
            />
          </Field>

          <div className="grid grid-cols-[minmax(0,1fr)_140px] gap-3">
            <Field label="First environment">
              <Input value={envName} onChange={(e) => setEnvName(e.target.value)} required />
            </Field>
            <Field label="Type">
              <Select value={envType} onChange={(e) => setEnvType(e.target.value)}>
                {ENV_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </Select>
            </Field>
          </div>

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="submit" disabled={busy || !name.trim() || !envName.trim()}>
              {busy ? "Creating..." : "Create project"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// AddEnvironmentDialog adds an environment to an existing project (deploy targets are
// per-environment, so a project needs at least one to deploy).
export function AddEnvironmentDialog({
  projectId,
  open,
  onOpenChange,
}: {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [type, setType] = useState<string>("preview");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function reset() {
    setName("");
    setType("preview");
    setBusy(false);
    setError("");
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setBusy(true);
    setError("");
    try {
      await environmentClient.createEnvironment({ projectId, name: trimmed, type });
      await queryClient.invalidateQueries({ queryKey: ["environments", projectId] });
      toast.success(`Environment ${trimmed} created`);
      onOpenChange(false);
      reset();
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "Could not create the environment");
      setBusy(false);
    }
  }

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
          <DialogTitle>Add environment</DialogTitle>
          <DialogDescription>
            A deployment target within this project — preview, staging, production, or custom.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="space-y-4">
          <div className="grid grid-cols-[minmax(0,1fr)_140px] gap-3">
            <Field label="Name">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Staging"
                autoFocus
                required
              />
            </Field>
            <Field label="Type">
              <Select value={type} onChange={(e) => setType(e.target.value)}>
                {ENV_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </Select>
            </Field>
          </div>

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="submit" disabled={busy || !name.trim()}>
              {busy ? "Creating..." : "Add environment"}
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
