import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff, KeyRound, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ConfigScope, ConfigType } from "@/gen/controlplane/v1/config_pb";
import { configClient } from "@/lib/clients";
import { useDemoData } from "@/lib/demo";
import { errorMessage, formatDate } from "@/lib/format";
import { configItems } from "@/lib/mockDashboard";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { useConfig, useEnvironments, useProjects, useServicesByProject } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

// EnvironmentVariablesPage is the single place to manage a service's configuration. Each entry has
// two independent axes: TYPE (variable, readable / secret, write-only encrypted) and SCOPE
// (service-level / environment-shared). The page is scoped to a chosen project → service; the
// environment is derived from the service. A service receives its environment-shared entries plus
// its own service-level entries at deploy time, the latter overriding on a key collision.

type Kind = "variable" | "secret";
type ScopeKind = "service" | "environment";

interface ConfigRow {
  key: string;
  value: string;
  kind: Kind;
  scope: ScopeKind;
  updated: string;
}

export function EnvironmentVariablesPage() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const selectedProjectId = useEffectiveProjectId();
  const projects = useProjects(workspaceId);

  const [localProjectId, setLocalProjectId] = useState("");
  const [selectedServiceId, setSelectedServiceId] = useState("");

  // When a project filter is active, lock onto it; otherwise default to the first project and let the
  // user pick freely. The effective project/service are derived during render so a freshly loaded (or
  // changed) list settles on a valid selection without syncing through an effect.
  const locked = Boolean(selectedProjectId);
  const projectList = projects.data ?? [];
  const localProject =
    localProjectId && projectList.some((p) => p.id === localProjectId)
      ? localProjectId
      : (projectList[0]?.id ?? "");
  const projectId = selectedProjectId || localProject;

  // A service belongs to exactly one environment, so the service drives the scope: pick a service and
  // the environment is derived from it (shown read-only). Listing services by project avoids the
  // dead-end where a chosen environment happens to have no services.
  const services = useServicesByProject(projectId);
  const serviceList = services.data ?? [];
  const serviceId =
    serviceList.length > 0
      ? (serviceList.some((s) => s.id === selectedServiceId) ? selectedServiceId : serviceList[0].id)
      : "";
  const selectedService = serviceList.find((s) => s.id === serviceId);
  const environmentId = selectedService?.environmentId ?? "";

  // Environments are loaded only to label the derived environment in the read-only selector.
  const environments = useEnvironments(projectId);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Environment variables"
        description="Variables (readable) and secrets (write-only) at service or environment-shared scope. Secret values stay write-only — you see metadata, never the value."
      />

      <Panel>
        <PanelHeader
          title="Scope"
          description={
            locked
              ? "Pick a service to manage its configuration. Its environment is shown alongside. Clear the project filter to switch projects."
              : "Pick a project and service. The environment is set by the service."
          }
        />
        <div className="grid gap-3 p-4 sm:grid-cols-3">
          <Select value={projectId} onChange={(e) => setLocalProjectId(e.target.value)} disabled={locked || !projects.data?.length} aria-label="Project">
            {projects.data?.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
            {!projects.data?.length && <option value="">No projects</option>}
          </Select>
          <Select value={serviceId} onChange={(e) => setSelectedServiceId(e.target.value)} disabled={!services.data?.length} aria-label="Service">
            {services.data?.map((service) => (
              <option key={service.id} value={service.id}>
                {service.name}
              </option>
            ))}
            {!services.data?.length && <option value="">No services</option>}
          </Select>
          {/* Environment is determined by the selected service, so it's read-only context, not a filter. */}
          <Select value={environmentId} disabled aria-label="Environment (set by service)">
            {environments.data?.map((environment) => (
              <option key={environment.id} value={environment.id}>
                {environment.name} ({environment.type || environment.slug})
              </option>
            ))}
            {!environmentId && <option value="">No environment</option>}
          </Select>
        </div>
      </Panel>

      <ConfigPanel serviceId={serviceId} environmentId={environmentId} />
    </div>
  );
}

// ConfigPanel renders all configuration that applies to a service (its service-level entries plus
// its environment's shared entries) and a single add form whose Type and Scope toggles decide how
// each upsert/delete is routed.
function ConfigPanel({ serviceId, environmentId }: { serviceId: string; environmentId: string }) {
  const demo = useDemoData();
  const queryClient = useQueryClient();
  const config = useConfig(serviceId);

  const [kind, setKind] = useState<Kind>("variable");
  const [scope, setScope] = useState<ScopeKind>("service");
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [busy, setBusy] = useState(false);

  const liveRows: ConfigRow[] = (config.data ?? []).map((e) => ({
    key: e.key,
    value: e.type === ConfigType.SECRET ? "" : e.value,
    kind: e.type === ConfigType.SECRET ? "secret" : "variable",
    scope: e.scope === ConfigScope.ENVIRONMENT ? "environment" : "service",
    updated: formatDate(e.updatedAt),
  }));
  const rows: ConfigRow[] =
    liveRows.length > 0
      ? liveRows
      : demo
        ? configItems.map((item) => ({
            key: item.key,
            value: item.kind === "secret" ? "" : item.value,
            kind: item.kind === "secret" ? "secret" : "variable",
            scope: "service" as const,
            updated: item.updated,
          }))
        : [];

  const loading = config.isLoading;
  const error = errorMessage(config.error);
  // A live write needs a real service (service scope) and environment (environment scope); both are
  // present together once a service is selected. Demo ids are prototype-* and never reach the backend.
  const canEdit = !demo && serviceId.length > 0 && environmentId.length > 0;

  // Duplicate guard is PER SCOPE: a key may exist at both service and environment scope (the override),
  // but not twice within one scope (a variable and a secret can't share a key in one scope).
  const existingInScope = new Set(liveRows.filter((r) => r.scope === scope).map((r) => r.key));
  const duplicate = key.trim().length > 0 && existingInScope.has(key.trim());

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const k = key.trim();
    if (!k || busy || !canEdit) return;
    if (existingInScope.has(k)) {
      toast.error(`"${k}" already exists at ${scope === "service" ? "service" : "environment"} scope — delete it first to replace it.`);
      return;
    }
    setBusy(true);
    try {
      await configClient.setConfig({
        type: kind === "secret" ? ConfigType.SECRET : ConfigType.VARIABLE,
        scope: scope === "environment" ? ConfigScope.ENVIRONMENT : ConfigScope.SERVICE,
        serviceId,
        environmentId,
        key: k,
        value,
      });
      await queryClient.invalidateQueries({ queryKey: ["config", serviceId] });
      setKey("");
      setValue("");
      setShowSecret(false);
      toast.success(`Saved ${k}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not save the value");
    } finally {
      setBusy(false);
    }
  }

  async function onDelete(row: ConfigRow) {
    try {
      await configClient.deleteConfig({
        scope: row.scope === "environment" ? ConfigScope.ENVIRONMENT : ConfigScope.SERVICE,
        serviceId,
        environmentId,
        key: row.key,
      });
      await queryClient.invalidateQueries({ queryKey: ["config", serviceId] });
      toast.success(`Removed ${row.key}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not remove the value");
    }
  }

  const segBtn = (active: boolean) =>
    `h-9 rounded px-3 text-sm font-medium transition ${
      active ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
    }`;

  return (
    <Panel>
      <PanelHeader
        title="Variables & secrets"
        description="Variables are readable; secrets are encrypted and write-only. Service-level entries override environment-shared ones on the same key. Applied on the next deploy."
      />
      <div className="space-y-4 p-4">
        <form onSubmit={onSubmit} className="space-y-3">
          <div className="flex flex-wrap gap-x-8 gap-y-3">
            <div>
              <span className="mb-1.5 block text-xs font-medium text-foreground">Type</span>
              <div className="inline-flex rounded-md border border-input bg-muted/40 p-0.5">
                <button type="button" onClick={() => setKind("variable")} className={segBtn(kind === "variable")}>
                  Variable
                </button>
                <button type="button" onClick={() => setKind("secret")} className={segBtn(kind === "secret")}>
                  Secret
                </button>
              </div>
            </div>
            <div>
              <span className="mb-1.5 block text-xs font-medium text-foreground">Scope</span>
              <div className="inline-flex rounded-md border border-input bg-muted/40 p-0.5">
                <button type="button" onClick={() => setScope("service")} className={segBtn(scope === "service")}>
                  Service
                </button>
                <button type="button" onClick={() => setScope("environment")} className={segBtn(scope === "environment")}>
                  Environment-shared
                </button>
              </div>
            </div>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1">
              <span className="mb-1.5 block text-xs font-medium text-foreground">Key</span>
              <Input
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder={kind === "secret" ? "DATABASE_URL" : "PORT"}
                autoCapitalize="none"
                autoComplete="off"
                spellCheck={false}
                disabled={!canEdit}
              />
            </div>
            <div className="min-w-0 flex-1">
              <span className="mb-1.5 block text-xs font-medium text-foreground">Value</span>
              <div className="relative">
                <Input
                  type={kind === "secret" && !showSecret ? "password" : "text"}
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder={kind === "secret" ? "••••••••" : "8080"}
                  autoCapitalize="none"
                  autoComplete={kind === "secret" ? "new-password" : "off"}
                  spellCheck={false}
                  disabled={!canEdit}
                  className={kind === "secret" ? "pr-9" : undefined}
                />
                {kind === "secret" && (
                  <button
                    type="button"
                    onClick={() => setShowSecret((v) => !v)}
                    className="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted-foreground transition hover:text-foreground"
                    aria-label={showSecret ? "Hide value" : "Show value"}
                    tabIndex={-1}
                  >
                    {showSecret ? <EyeOff className="h-4 w-4" aria-hidden="true" /> : <Eye className="h-4 w-4" aria-hidden="true" />}
                  </button>
                )}
              </div>
            </div>
            <Button type="submit" size="sm" disabled={busy || !key.trim() || !canEdit || duplicate}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              {busy ? "Saving…" : kind === "secret" ? "Set secret" : "Set variable"}
            </Button>
          </div>
        </form>

        {duplicate && (
          <p className="text-xs text-destructive">
            &ldquo;{key.trim()}&rdquo; already exists at {scope === "service" ? "service" : "environment"} scope. Delete it first to replace its value.
          </p>
        )}

        {loading && <Skeleton className="h-40 w-full" />}
        {error && <EmptyState title="Config API returned an error" body={error} />}
        {!loading && !error && rows.length === 0 && (
          <EmptyState
            title="No variables yet"
            body={
              canEdit
                ? "Add a variable or secret above to configure this service."
                : "Pick a project and service to manage configuration."
            }
          />
        )}
        {!loading && !error && rows.length > 0 && (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Key</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Scope</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((item) => (
                  <TableRow key={`${item.scope}-${item.kind}-${item.key}`}>
                    <TableCell className="font-medium text-foreground">
                      <span className="inline-flex items-center gap-1.5">
                        {item.kind === "secret" && <KeyRound className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />}
                        {item.key}
                      </span>
                    </TableCell>
                    <TableCell className="max-w-[260px] truncate font-mono text-xs text-muted-foreground">
                      {item.kind === "secret" ? "••••••••" : item.value}
                    </TableCell>
                    <TableCell>
                      <Badge tone={item.kind === "secret" ? "purple" : "blue"}>{item.kind}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {item.scope === "environment" ? "Environment-shared" : "Service"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{item.updated}</TableCell>
                    <TableCell>
                      {canEdit && (
                        <ConfirmDialog
                          trigger={
                            <Button size="sm" variant="ghost" aria-label={`Remove ${item.key}`}>
                              <Trash2 className="h-4 w-4" aria-hidden="true" />
                            </Button>
                          }
                          title={`Delete ${item.kind} ${item.key}?`}
                          description={
                            item.scope === "environment"
                              ? `Removes "${item.key}" from this environment's shared config, used by every service in it. Applied on the next deploy.`
                              : `Removes "${item.key}" from this service's config. Applied on the next deploy.`
                          }
                          recovery={`You can re-add ${item.key} at any time.`}
                          confirmLabel="Delete"
                          onConfirm={() => onDelete(item)}
                        />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </Panel>
  );
}
