import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Box, HardDrive, KeyRound, Server, ShieldCheck, Users } from "lucide-react";

import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Panel, PanelHeader } from "@/components/ui";
import { useCurrentUser } from "@/lib/auth";
import { useDemoData } from "@/lib/demo";
import { projectDashboardActivity } from "@/lib/mockDashboard";
import { useApiTokens, useMembers, useProjects, useServers, useWorkspaces } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

export function OverviewPage() {
  const demo = useDemoData();
  const { data: user } = useCurrentUser();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const workspaces = useWorkspaces();
  const projects = useProjects(workspaceId);
  const servers = useServers(workspaceId);
  const members = useMembers(workspaceId);
  const tokens = useApiTokens();

  const workspaceName = workspaces.data?.find((w) => w.id === workspaceId)?.name ?? "your workspace";
  const num = (n?: number) => String(n ?? 0);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Overview"
        description={`Welcome back${user?.email ? `, ${user.email}` : ""} — here's ${workspaceName} at a glance.`}
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Projects" value={num(projects.data?.length)} detail="In this workspace" icon={Box} intent="info" accentBar />
        <StatCard label="Servers" value={num(servers.data?.length)} detail="Connected machines" icon={Server} intent="violet" accentBar />
        <StatCard label="Members" value={num(members.data?.length)} detail="With access" icon={Users} intent="success" accentBar />
        <StatCard label="API tokens" value={num(tokens.data?.length)} detail="Active tokens" icon={KeyRound} intent="neutral" accentBar />
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.7fr)]">
        <Panel>
          <PanelHeader title="Workspace operations" description="Jump into the areas you use most." />
          <div className="grid gap-3 p-4 sm:grid-cols-2">
            <OpLink to="/projects" icon={Box} label="Projects" detail="Apps, environments, health" />
            <OpLink to="/servers" icon={Server} label="Servers" detail="Owned infrastructure" />
            <OpLink to="/resources" icon={KeyRound} label="Resources" detail="Env vars & secrets" />
            <OpLink to="/backups" icon={HardDrive} label="Backups" detail="Recovery & restore" />
          </div>
        </Panel>

        <Panel>
          <PanelHeader title="Recent activity" />
          {demo ? (
            <div className="divide-y divide-border px-4">
              {projectDashboardActivity.slice(0, 4).map((item) => (
                <div key={`${item.title}-${item.time}`} className="flex items-start gap-3 py-3">
                  <span
                    className={cnDot(item.tone)}
                    aria-hidden="true"
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-foreground">{item.title}</p>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">{item.detail}</p>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">{item.time}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="px-4 py-8 text-center">
              <p className="text-sm font-medium text-foreground">No activity yet</p>
              <p className="mx-auto mt-1 max-w-xs text-xs text-muted-foreground">
                Deploys, backups, and access changes will show up here.
              </p>
            </div>
          )}
        </Panel>
      </div>

      <section className="flex items-start gap-4 rounded-xl border border-border bg-muted/40 p-5">
        <span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-border bg-card text-foreground shadow-sm">
          <ShieldCheck className="h-6 w-6" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h3 className="text-base font-semibold text-foreground">Your infrastructure. Your data.</h3>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Plorigo is BYOS — it runs on servers you own, with rollbacks, backups, and an audit trail for every
            scary action. You run it. You own it.
          </p>
        </div>
      </section>
    </div>
  );
}

function OpLink({
  to,
  icon: Icon,
  label,
  detail,
}: {
  to: "/projects" | "/servers" | "/resources" | "/backups";
  icon: typeof Box;
  label: string;
  detail: string;
}) {
  return (
    <Link
      to={to}
      className="flex items-center gap-3 rounded-xl border border-border bg-background px-3 py-3 transition hover:border-ring/40 hover:bg-accent/50"
    >
      <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-border bg-card text-muted-foreground shadow-sm">
        <Icon className="h-5 w-5" aria-hidden="true" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-foreground">{label}</p>
        <p className="truncate text-xs text-muted-foreground">{detail}</p>
      </div>
      <ArrowUpRight className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
    </Link>
  );
}

function cnDot(tone: "green" | "blue" | "amber" | "red") {
  const map = {
    green: "bg-success",
    blue: "bg-info",
    amber: "bg-warning",
    red: "bg-destructive",
  };
  return `mt-1.5 h-2 w-2 shrink-0 rounded-full ${map[tone]}`;
}
