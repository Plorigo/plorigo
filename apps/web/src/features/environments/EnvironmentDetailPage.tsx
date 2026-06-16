import { useState, type FormEvent } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowLeft, Container, GitFork, Globe, KeyRound, Lock, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Skeleton } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { secretClient } from "@/lib/clients";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { useEnvironment, useSecrets, useServicesByEnvironment } from "@/lib/queries";
import { type Tone } from "@/lib/status";
import { isPublic, sourceLabel } from "@/features/services/serviceData";

// EnvironmentDetailPage is a deployment target within a project: its identity, the encrypted
// secrets shared by every service deployed into it, and the services running there. Secrets are
// environment-scoped (a deliberate asymmetry with service-scoped env vars — see
// docs/architecture/security.md) and write-only: a value is set here but never read back.
export function EnvironmentDetailPage() {
  const { projectId, environmentId } = useParams({ strict: false }) as {
    projectId?: string;
    environmentId?: string;
  };
  const id = environmentId ?? "";
  const scopedProjectId = useEffectiveProjectId();
  const pid = projectId ?? scopedProjectId;
  const env = useEnvironment(id);

  if (env.isLoading && !env.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={pid} />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!env.data) {
    return (
      <div className="space-y-6">
        <BackLink projectId={pid} />
        <EmptyState title="Environment not found" body="It may have been removed, or it belongs to another workspace." />
      </div>
    );
  }

  const e = env.data;

  return (
    <div className="space-y-6">
      <BackLink projectId={pid} />

      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">{e.name}</h1>
          <Badge tone={environmentTone(e.type)}>{e.type}</Badge>
        </div>
        <p className="mt-1.5 text-sm text-muted-foreground">
          A deployment target in this project. Secrets set here are shared by every service deployed into it.
        </p>
      </div>

      <SecretsPanel environmentId={id} />
      <ServicesPanel environmentId={id} projectId={pid} />
    </div>
  );
}

// SecretsPanel manages an environment's encrypted secrets. SetSecret upserts by key; the value is
// sealed server-side and never returned, so the list shows metadata only (key + last update) and
// the input is treated as a password. Setting an existing key replaces its value.
function SecretsPanel({ environmentId }: { environmentId: string }) {
  const queryClient = useQueryClient();
  const secrets = useSecrets(environmentId);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const rows = secrets.data ?? [];

  async function invalidate() {
    await queryClient.invalidateQueries({ queryKey: ["secrets", environmentId] });
  }

  async function onSet(e: FormEvent) {
    e.preventDefault();
    const k = key.trim();
    if (!k || busy) return;
    setBusy(true);
    try {
      await secretClient.setSecret({ environmentId, key: k, value });
      await invalidate();
      setKey("");
      setValue("");
      toast.success(`Saved ${k}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not save the secret");
    } finally {
      setBusy(false);
    }
  }

  async function onDelete(k: string) {
    try {
      await secretClient.deleteSecret({ environmentId, key: k });
      await invalidate();
      toast.success(`Removed ${k}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not remove the secret");
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Secrets"
        description="Encrypted, write-only values shared by every service in this environment. A value is set once and never shown again — set the key again to replace it. Applied on the next deploy."
      />
      <div className="space-y-4 p-4">
        <form onSubmit={onSet} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1">
            <span className="mb-1.5 block text-xs font-medium text-foreground">Key</span>
            <Input value={key} onChange={(e) => setKey(e.target.value)} placeholder="DATABASE_URL" autoCapitalize="none" autoComplete="off" spellCheck={false} />
          </div>
          <div className="min-w-0 flex-1">
            <span className="mb-1.5 block text-xs font-medium text-foreground">Value</span>
            <Input
              type="password"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="••••••••"
              autoCapitalize="none"
              autoComplete="new-password"
              spellCheck={false}
            />
          </div>
          <Button type="submit" size="sm" disabled={busy || !key.trim()}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            {busy ? "Saving…" : "Set secret"}
          </Button>
        </form>

        {secrets.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No secrets yet"
            body="Add a key and value above. Secrets are encrypted at rest, never returned by the API, and injected into this environment's services at deploy time."
          />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Key</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((sec) => (
                  <TableRow key={sec.id || sec.key}>
                    <TableCell className="font-medium text-foreground">
                      <span className="inline-flex items-center gap-1.5">
                        <KeyRound className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                        {sec.key}
                      </span>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">••••••••</TableCell>
                    <TableCell className="text-muted-foreground">{sec.updatedAt ? timeAgo(sec.updatedAt) : "—"}</TableCell>
                    <TableCell>
                      <Button size="sm" variant="ghost" aria-label={`Remove ${sec.key}`} onClick={() => onDelete(sec.key)}>
                        <Trash2 className="h-4 w-4" aria-hidden="true" />
                      </Button>
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

// ServicesPanel lists the services deployed into this environment, giving the page context and a
// jump-off point to each service (whose own page manages its non-secret env vars).
function ServicesPanel({ environmentId, projectId }: { environmentId: string; projectId: string }) {
  const navigate = useNavigate();
  const services = useServicesByEnvironment(environmentId);
  const rows = services.data ?? [];

  return (
    <Panel>
      <PanelHeader title="Services" description="The services deployed into this environment." />
      {services.isLoading ? (
        <div className="p-4">
          <Skeleton className="h-32 w-full" />
        </div>
      ) : rows.length === 0 ? (
        <div className="p-4">
          <EmptyState title="No services here yet" body="Add a service to this project and pick this environment to see it listed here." />
        </div>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Service</TableHead>
                <TableHead>Source</TableHead>
                <TableHead>Visibility</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((s) => (
                <TableRow
                  key={s.id}
                  className="cursor-pointer"
                  onClick={() =>
                    navigate({
                      to: "/projects/$projectId/services/$serviceId",
                      params: { projectId, serviceId: s.id },
                    })
                  }
                >
                  <TableCell>
                    <p className="truncate text-sm font-medium text-foreground">{s.name}</p>
                    <p className="text-xs text-muted-foreground">{s.id.slice(0, 8)}</p>
                  </TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                      {s.sourceKind === "git" ? (
                        <GitFork className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                      ) : (
                        <Container className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                      )}
                      <span className="truncate font-mono text-xs">{sourceLabel(s)}</span>
                    </span>
                  </TableCell>
                  <TableCell>
                    {isPublic(s) ? (
                      <Badge tone="green">
                        <Globe className="mr-1 h-3 w-3" aria-hidden="true" />
                        public
                      </Badge>
                    ) : (
                      <Badge tone="purple">
                        <Lock className="mr-1 h-3 w-3" aria-hidden="true" />
                        private
                      </Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Panel>
  );
}

// environmentTone tints an environment badge by its type (mirrors the project overview).
function environmentTone(type: string): Tone {
  if (type === "production") return "green";
  if (type === "staging") return "amber";
  if (type === "preview") return "blue";
  return "purple";
}

// timeAgo renders a short relative time for an RFC 3339 timestamp.
function timeAgo(iso: string): string {
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}

function BackLink({ projectId }: { projectId: string }) {
  if (projectId) {
    return (
      <Link
        to="/projects/$projectId"
        params={{ projectId }}
        className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to project
      </Link>
    );
  }
  return (
    <Link to="/projects" className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-4 w-4" aria-hidden="true" />
      Back to projects
    </Link>
  );
}
