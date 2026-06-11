import { useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
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
import { environmentClient } from "@/lib/clients";

// The environment type vocabulary, matching the backend CHECK constraint.
const ENV_TYPES = ["production", "staging", "preview", "custom"] as const;

// AddEnvironmentDialog adds an environment to an existing project (deploy targets are
// per-environment, so a project needs at least one to deploy). A project is created with
// Production + Preview by default; this adds more.
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
