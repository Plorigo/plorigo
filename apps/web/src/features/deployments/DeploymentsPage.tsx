import { AlertTriangle, CheckCircle2, Rocket, Terminal } from "lucide-react";

import { ComingSoon } from "@/components/ComingSoon";
import { FailureSummary } from "@/components/FailureSummary";
import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Badge, Panel, PanelHeader, StatusDot } from "@/components/ui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/cn";
import { useDemoData } from "@/lib/demo";
import { deployments, logLines } from "@/lib/mockDashboard";
import { statusTone } from "@/lib/status";

export function DeploymentsPage() {
  const demo = useDemoData();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Deployments"
        description="A timeline for every release, with build and runtime logs and rollback context."
      />
      {demo ? (
        <DeploymentsDemo />
      ) : (
        <ComingSoon
          icon={Rocket}
          title="Deployment timeline"
          description="Every deploy gets a live timeline — build, health check, route switch — with logs you can stream and a one-click rollback to the previous healthy release."
          points={[
            "Build & runtime logs over SSE",
            "Health-check and route-switch stages",
            "One-click rollback to a kept release",
            "Plain-English failure summaries",
          ]}
        />
      )}
    </div>
  );
}

function DeploymentsDemo() {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-3">
        <StatCard label="Latest production" value="Ready" detail="main · 8f41c2a" icon={CheckCircle2} intent="success" accentBar />
        <StatCard label="Preview build" value="Building" detail="release/checkout" icon={Rocket} intent="info" accentBar />
        <StatCard label="Needs attention" value="1 failed" detail="staging worker" icon={AlertTriangle} intent="warning" accentBar />
      </div>

      <FailureSummary
        headline="Queue Worker failed to start on staging — the health check never passed."
        suggestion="The container exited before binding its port. This usually means a missing environment variable or a crash on boot. Check REDIS_URL, then redeploy — the previous release is still serving."
        logs={[
          "14:21:03  worker   Error: connect ECONNREFUSED 127.0.0.1:6379",
          "14:21:03  worker   at TCPConnectWrap.afterConnect [as oncomplete]",
          "14:21:04  health   readiness probe failed: container not listening on :8080",
          "14:21:34  deploy   health check timed out after 30s — rolling back",
        ]}
      />

      <Panel>
        <PanelHeader title="Deployment timeline" description="Recent releases across environments." />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Deployment</TableHead>
                <TableHead>Environment</TableHead>
                <TableHead>Branch</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Duration</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deployments.map((deployment) => (
                <TableRow key={deployment.id}>
                  <TableCell>
                    <p className="font-medium text-foreground">{deployment.project}</p>
                    <p className="text-xs text-muted-foreground">
                      {deployment.commit} by {deployment.author} · {deployment.age}
                    </p>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{deployment.environment}</TableCell>
                  <TableCell className="text-muted-foreground">{deployment.branch}</TableCell>
                  <TableCell>
                    <StatusDot tone={statusTone(deployment.status)} label={deployment.status} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">{deployment.duration}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Panel>

      <Panel>
        <PanelHeader title="Runtime logs" description="Streamed from the deploy agent." />
        <div className="space-y-2 p-4">
          {logLines.map((line) => (
            <div
              key={`${line.time}-${line.service}`}
              className="grid gap-2 rounded-md bg-zinc-950 px-3 py-2 text-xs text-zinc-100 sm:grid-cols-[64px_92px_minmax(0,1fr)]"
            >
              <span className="font-mono text-zinc-400">{line.time}</span>
              <span
                className={cn(
                  "inline-flex items-center gap-1 font-mono",
                  line.level === "error"
                    ? "text-red-300"
                    : line.level === "warn"
                      ? "text-amber-300"
                      : "text-emerald-300",
                )}
              >
                <Terminal className="h-3 w-3" aria-hidden="true" />
                {line.service}
              </span>
              <span className="min-w-0 truncate font-mono">{line.message}</span>
            </div>
          ))}
        </div>
      </Panel>

      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Badge tone="purple">Demo</Badge>
        Showing prototype deployment data. Turn off demo mode to see your real releases.
      </p>
    </div>
  );
}
