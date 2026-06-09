import type { DataStatus } from "../components/ui";

export interface DashboardProject {
  id: string;
  name: string;
  slug: string;
  repo: string;
  framework: string;
  url: string;
  branch: string;
  status: "healthy" | "building" | "attention";
  source: DataStatus;
  updated: string;
}

export interface DeploymentRow {
  id: string;
  project: string;
  environment: string;
  branch: string;
  commit: string;
  status: "ready" | "building" | "failed" | "queued";
  author: string;
  duration: string;
  age: string;
  source: DataStatus;
}

export interface LogLine {
  level: "info" | "warn" | "error";
  time: string;
  service: string;
  message: string;
}

export interface ServerHealth {
  name: string;
  region: string;
  cpu: string;
  memory: string;
  disk: string;
  status: "online" | "degraded" | "offline";
  source: DataStatus;
}

export interface ConfigItem {
  key: string;
  value: string;
  kind: "env" | "secret";
  updated: string;
  source: DataStatus;
}

export interface ActivityItem {
  title: string;
  detail: string;
  time: string;
  status: DataStatus;
}

export const prototypeProjects: DashboardProject[] = [
  {
    id: "prototype-storefront",
    name: "Storefront",
    slug: "storefront",
    repo: "github.com/plorigo/storefront",
    framework: "Next.js",
    url: "storefront.plorigo.app",
    branch: "main",
    status: "healthy",
    source: "prototype",
    updated: "12m ago",
  },
  {
    id: "prototype-api",
    name: "Payments API",
    slug: "payments-api",
    repo: "github.com/plorigo/payments-api",
    framework: "Go",
    url: "api.plorigo.app",
    branch: "release/checkout",
    status: "building",
    source: "prototype",
    updated: "24m ago",
  },
  {
    id: "prototype-worker",
    name: "Queue Worker",
    slug: "queue-worker",
    repo: "github.com/plorigo/worker",
    framework: "Dockerfile",
    url: "internal worker",
    branch: "main",
    status: "attention",
    source: "prototype",
    updated: "1h ago",
  },
];

export const deployments: DeploymentRow[] = [
  {
    id: "dpl_01",
    project: "Storefront",
    environment: "Production",
    branch: "main",
    commit: "8f41c2a",
    status: "ready",
    author: "Ismat",
    duration: "48s",
    age: "12m ago",
    source: "planned",
  },
  {
    id: "dpl_02",
    project: "Payments API",
    environment: "Preview",
    branch: "release/checkout",
    commit: "20b8c91",
    status: "building",
    author: "Maya",
    duration: "1m 14s",
    age: "24m ago",
    source: "planned",
  },
  {
    id: "dpl_03",
    project: "Queue Worker",
    environment: "Staging",
    branch: "main",
    commit: "bd32a09",
    status: "failed",
    author: "Alex",
    duration: "31s",
    age: "1h ago",
    source: "planned",
  },
  {
    id: "dpl_04",
    project: "Storefront",
    environment: "Preview",
    branch: "feat/pricing",
    commit: "112ab8f",
    status: "ready",
    author: "Ismat",
    duration: "52s",
    age: "2h ago",
    source: "planned",
  },
];

export const logLines: LogLine[] = [
  {
    level: "info",
    time: "14:20:11",
    service: "builder",
    message: "Detected Next.js app from package.json and selected node runtime.",
  },
  {
    level: "info",
    time: "14:20:38",
    service: "buildkit",
    message: "Layer cache restored from previous production deployment.",
  },
  {
    level: "warn",
    time: "14:21:03",
    service: "health",
    message: "First readiness probe was slow; retry succeeded after 4.8s.",
  },
  {
    level: "info",
    time: "14:21:09",
    service: "caddy",
    message: "Production route switched to container storefront-8f41c2a.",
  },
];

export const serverHealth: ServerHealth[] = [
  {
    name: "nashville-1",
    region: "US East",
    cpu: "28%",
    memory: "4.1 / 16 GB",
    disk: "118 / 240 GB",
    status: "online",
    source: "prototype",
  },
  {
    name: "helsinki-1",
    region: "EU North",
    cpu: "64%",
    memory: "10.8 / 16 GB",
    disk: "191 / 240 GB",
    status: "degraded",
    source: "prototype",
  },
  {
    name: "preview-pool",
    region: "Auto",
    cpu: "0%",
    memory: "standby",
    disk: "standby",
    status: "offline",
    source: "planned",
  },
];

export const configItems: ConfigItem[] = [
  {
    key: "NEXT_PUBLIC_APP_URL",
    value: "https://storefront.plorigo.app",
    kind: "env",
    updated: "2h ago",
    source: "prototype",
  },
  {
    key: "STRIPE_SECRET_KEY",
    value: "write-only",
    kind: "secret",
    updated: "4h ago",
    source: "prototype",
  },
  {
    key: "DATABASE_URL",
    value: "write-only",
    kind: "secret",
    updated: "yesterday",
    source: "prototype",
  },
];

export const readinessChecks = [
  { label: "Health checks", value: "Passing", tone: "green" as const, status: "planned" as const },
  { label: "Rollback target", value: "Available", tone: "green" as const, status: "planned" as const },
  { label: "Backups", value: "Last test pending", tone: "amber" as const, status: "planned" as const },
  { label: "Production lock", value: "Not required", tone: "neutral" as const, status: "planned" as const },
];

export const domains = [
  { domain: "storefront.plorigo.app", target: "production", status: "ready", source: "planned" as const },
  { domain: "checkout-preview.plorigo.app", target: "preview", status: "protected", source: "planned" as const },
  { domain: "api.plorigo.app", target: "payments-api", status: "ready", source: "planned" as const },
];

export const backups = [
  { name: "postgres-primary", schedule: "Every 6 hours", lastRun: "03:00", status: "healthy", source: "planned" as const },
  { name: "media-volume", schedule: "Daily", lastRun: "01:15", status: "needs restore test", source: "planned" as const },
];

export const activity: ActivityItem[] = [
  {
    title: "Deployment promoted",
    detail: "Storefront main became production after health checks passed.",
    time: "12m ago",
    status: "planned",
  },
  {
    title: "Secret metadata updated",
    detail: "STRIPE_SECRET_KEY was rotated for the production environment.",
    time: "4h ago",
    status: "prototype",
  },
  {
    title: "Server registered",
    detail: "nashville-1 connected and is ready for signed jobs.",
    time: "yesterday",
    status: "prototype",
  },
  {
    title: "Approval requested",
    detail: "Production migration requires owner approval before deployment.",
    time: "planned flow",
    status: "planned",
  },
];

export const apiSurface = [
  { name: "Auth and sessions", status: "live" as const },
  { name: "Workspaces and projects", status: "live" as const },
  { name: "Servers, environments, env vars, secrets", status: "live" as const },
  { name: "Deployments, logs, domains, backups", status: "planned" as const },
  { name: "Readiness doctor and AI approvals", status: "planned" as const },
];
