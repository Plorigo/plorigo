import { useState, type FormEvent, type ReactNode } from "react";
import { ConnectError } from "@connectrpc/connect";
import { TriangleAlert } from "lucide-react";

import { Button, Input, Select } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { DeployTemplate, TemplateOption } from "@/lib/templates";

// initialValues seeds the form from each option's default (empty when none).
function initialValues(options: TemplateOption[]): Record<string, string> {
  const v: Record<string, string> = {};
  for (const o of options) v[o.key] = o.default ?? "";
  return v;
}

// TemplateConfigDialog asks a template's options before deploying. It renders the template's
// `options` generically (read-only when an option is not editable — e.g. a managed database's
// fixed port) and hands the collected values back through onSubmit, which performs the actual
// create. onSubmit rejects with a message on failure; the dialog shows it and stays open. On
// success onSubmit navigates away, so the dialog simply unmounts.
export function TemplateConfigDialog({
  template,
  open,
  onOpenChange,
  environmentNames,
  serverLabel,
  onSubmit,
}: {
  template: DeployTemplate;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  environmentNames: string[];
  serverLabel: string;
  onSubmit: (values: Record<string, string>) => Promise<void>;
}) {
  const [values, setValues] = useState<Record<string, string>>(() => initialValues(template.options));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const managed = template.kind === "managed";
  // Managed services provision into a single environment; plain templates fan out to all selected.
  const target = managed
    ? `${serverLabel} · ${environmentNames[0] ?? "—"}`
    : `${serverLabel} · ${environmentNames.join(", ")}`;

  function set(key: string, value: string) {
    setError("");
    setValues((v) => ({ ...v, [key]: value }));
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await onSubmit(values);
      // Success navigates away (the page unmounts this dialog); nothing more to do here.
    } catch (err) {
      setError(err instanceof ConnectError || err instanceof Error ? err.message : "Could not deploy the template");
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Configure {template.name}</DialogTitle>
          <DialogDescription>{template.description}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {template.options.map((o) => (
            <Field key={o.key} label={o.label} help={o.help}>
              {o.editable === false ? (
                <Input value={values[o.key] ?? ""} readOnly disabled />
              ) : o.type === "select" ? (
                <Select value={values[o.key] ?? ""} onChange={(e) => set(o.key, e.target.value)} aria-label={o.label}>
                  {(o.choices ?? []).map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </Select>
              ) : (
                <Input
                  value={values[o.key] ?? ""}
                  onChange={(e) => set(o.key, e.target.value)}
                  type={o.type === "password" ? "password" : "text"}
                  inputMode={o.type === "number" ? "numeric" : undefined}
                  placeholder={o.placeholder}
                  autoCapitalize="none"
                  spellCheck={false}
                />
              )}
            </Field>
          ))}

          {managed && (
            <div className="flex items-start gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2.5 text-xs leading-5 text-warning">
              <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
              <p>
                Private to its environment, with generated credentials. Data is{" "}
                <span className="font-medium">not yet persisted across redeploys</span> — treat it as ephemeral for now.
                {environmentNames.length > 1 && ` Created in ${environmentNames[0]} (databases use one environment).`}
              </p>
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            Deploys to <span className="font-medium text-foreground">{target}</span>.
          </p>

          {error && (
            <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? "Deploying…" : "Deploy"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, help, children }: { label: string; help?: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm font-medium text-foreground">{label}</span>
      {children}
      {help && <span className="mt-1 block text-xs text-muted-foreground">{help}</span>}
    </label>
  );
}
