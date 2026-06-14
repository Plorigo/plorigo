import { useState } from "react";
import { Globe2, LockKeyhole } from "lucide-react";

import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Panel, PanelHeader, Select, Skeleton, StatusDot } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { errorMessage, formatDate } from "@/lib/format";
import { configItems, domains } from "@/lib/mockDashboard";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { useEnvironments, useEnvVars, useProjects, useSecrets } from "@/lib/queries";
import { statusTone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

interface ConfigRow {
  key: string;
  value: string;
  kind: "env" | "secret";
  updated: string;
}

export function ResourcesPage() {
  const demo = useDemoData();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const selectedProjectId = useEffectiveProjectId();
  const projects = useProjects(workspaceId);

  const [localProjectId, setLocalProjectId] = useState("");
  const [selectedEnvironmentId, setSelectedEnvironmentId] = useState("");

  // When a project filter is active, lock onto it; otherwise default to the first project
  // and let the user pick freely. The effective project and environment are derived during
  // render so a freshly loaded (or changed) list settles on a valid selection without
  // syncing through an effect.
  const locked = Boolean(selectedProjectId);
  const projectList = projects.data ?? [];
  const localProject =
    localProjectId && projectList.some((p) => p.id === localProjectId)
      ? localProjectId
      : (projectList[0]?.id ?? "");
  const projectId = selectedProjectId || localProject;

  const environments = useEnvironments(projectId);
  const envList = environments.data ?? [];
  const environmentId =
    envList.length > 0
      ? (envList.some((e) => e.id === selectedEnvironmentId) ? selectedEnvironmentId : envList[0].id)
      : "";

  const envVars = useEnvVars(environmentId);
  const secrets = useSecrets(environmentId);

  const liveRows: ConfigRow[] = [
    ...(envVars.data ?? []).map((item) => ({
      key: item.key,
      value: item.value,
      kind: "env" as const,
      updated: formatDate(item.updatedAt),
    })),
    ...(secrets.data ?? []).map((item) => ({
      key: item.key,
      value: "",
      kind: "secret" as const,
      updated: formatDate(item.updatedAt),
    })),
  ];
  const rows: ConfigRow[] =
    liveRows.length > 0
      ? liveRows
      : demo
        ? configItems.map((item) => ({ key: item.key, value: item.value, kind: item.kind, updated: item.updated }))
        : [];

  const loading = projects.isLoading || environments.isLoading || envVars.isLoading || secrets.isLoading;
  const error = errorMessage(environments.error || envVars.error || secrets.error);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Resources"
        description="Per-environment variables and secrets. Secret values stay write-only — you see metadata, never the value."
      />

      <Panel>
        <PanelHeader
          title="Environment"
          description={
            locked
              ? "Showing the selected project. Clear the project filter to switch projects."
              : "Pick a project and environment to inspect its configuration."
          }
        />
        <div className="grid gap-3 p-4 sm:grid-cols-2">
          <Select value={projectId} onChange={(e) => setLocalProjectId(e.target.value)} disabled={locked || !projects.data?.length} aria-label="Project">
            {projects.data?.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
            {!projects.data?.length && <option value="">No projects</option>}
          </Select>
          <Select value={environmentId} onChange={(e) => setSelectedEnvironmentId(e.target.value)} disabled={!environments.data?.length} aria-label="Environment">
            {environments.data?.map((environment) => (
              <option key={environment.id} value={environment.id}>
                {environment.name} ({environment.type || environment.slug})
              </option>
            ))}
            {!environments.data?.length && <option value="">No environments</option>}
          </Select>
        </div>
      </Panel>

      <Panel>
        <PanelHeader
          title="Variables & secrets"
          description="Env vars can be read back; secret values are masked."
          action={
            <Button size="sm" disabled>
              <LockKeyhole className="h-4 w-4" aria-hidden="true" />
              Add value
            </Button>
          }
        />
        <div className="p-4">
          {loading && <Skeleton className="h-40 w-full" />}
          {error && <EmptyState title="Config API returned an error" body={error} />}
          {!loading && !error && rows.length === 0 && (
            <EmptyState
              title="No variables yet"
              body="Add environment variables and secrets to this environment to see them here."
            />
          )}
          {!loading && !error && rows.length > 0 && (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Key</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Kind</TableHead>
                    <TableHead>Updated</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((item) => (
                    <TableRow key={`${item.kind}-${item.key}`}>
                      <TableCell className="font-medium text-foreground">{item.key}</TableCell>
                      <TableCell className="max-w-[260px] truncate font-mono text-xs text-muted-foreground">
                        {item.kind === "secret" ? "••••••••••••" : item.value}
                      </TableCell>
                      <TableCell>
                        <Badge tone={item.kind === "secret" ? "purple" : "blue"}>{item.kind}</Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{item.updated}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      </Panel>

      {demo && (
        <Panel>
          <PanelHeader title="Domains" description="Custom domains and SSL (prototype)." />
          <div className="space-y-2 p-4">
            {domains.map((domain) => (
              <div
                key={domain.domain}
                className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <Globe2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-foreground">{domain.domain}</p>
                    <p className="truncate text-xs text-muted-foreground">{domain.target}</p>
                  </div>
                </div>
                <StatusDot tone={statusTone(domain.status)} label={domain.status} />
              </div>
            ))}
          </div>
        </Panel>
      )}
    </div>
  );
}
