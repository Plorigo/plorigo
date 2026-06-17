import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowLeft, Container, GitFork, Globe, Lock } from "lucide-react";

import { Badge, EmptyState, Panel, PanelHeader, Skeleton } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { useEnvironment, useServicesByEnvironment } from "@/lib/queries";
import { type Tone } from "@/lib/status";
import { isPublic, sourceLabel } from "@/features/services/serviceData";

// EnvironmentDetailPage is a deployment target within a project: its identity and the services
// running there. Its variables and secrets are managed on the Environment Variables page (secrets
// are environment-scoped and shared by every service in the environment — see
// docs/architecture/security.md).
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
          A deployment target in this project. Manage its variables and secrets on the Environment
          Variables page.
        </p>
      </div>

      <ServicesPanel environmentId={id} projectId={pid} />
    </div>
  );
}

// ServicesPanel lists the services deployed into this environment, giving the page context and a
// jump-off point to each service.
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
