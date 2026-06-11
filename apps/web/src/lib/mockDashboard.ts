// Prototype fixtures shown only in demo mode (see lib/demo). A "source" tag marks
// whether a target feature is backed by a live API, a prototype, or planned.
export type DataStatus = "live" | "prototype" | "planned";

export interface DashboardProject {
  id: string;
  name: string;
  slug: string;
  repo: string;
  framework: string;
  url: string;
  branch: string;
  commit: string;
  status: "healthy" | "building" | "warning" | "attention";
  source: DataStatus;
  updated: string;
  environments: Array<{ name: string; tone: "green" | "blue" | "purple" | "neutral" }>;
  services: string[];
  owner: string;
  readiness: number;
  collaborators: number;
  sparkline: number[];
  kind: "web" | "api" | "worker" | "service" | "analytics";
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
  readiness: "ready" | "degraded" | "unavailable";
  readinessReason?: string;
  dockerVersion?: string;
  os?: string;
  arch?: string;
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

export interface ProjectActivityItem {
  title: string;
  detail: string;
  time: string;
  tone: "green" | "blue" | "amber" | "red";
}

export interface ProjectAttentionItem {
  project: string;
  detail: string;
  tone: "amber" | "red";
}

export const projectDashboardActivity: ProjectActivityItem[] = [
  {
    title: "Payments API",
    detail: "Deployed to production",
    time: "35m ago",
    tone: "green",
  },
  {
    title: "Storefront",
    detail: "Preview deployment created",
    time: "42m ago",
    tone: "blue",
  },
  {
    title: "Auth Service",
    detail: "Backup completed",
    time: "1h ago",
    tone: "green",
  },
  {
    title: "Queue Worker",
    detail: "Worker restarted",
    time: "2h ago",
    tone: "blue",
  },
  {
    title: "Analytics",
    detail: "Deployment failed",
    time: "5h ago",
    tone: "red",
  },
];

export const projectAttentionItems: ProjectAttentionItem[] = [
  { project: "Analytics", detail: "Readiness 68%", tone: "red" },
  { project: "Queue Worker", detail: "Readiness 82%", tone: "amber" },
  { project: "Auth Service", detail: "Staging deploy failing", tone: "red" },
];

export const releaseHealth = [
  { label: "Successful", value: 42, tone: "green" as const },
  { label: "Warnings", value: 6, tone: "amber" as const },
  { label: "Failed", value: 3, tone: "red" as const },
];

export const prototypeProjects: DashboardProject[] = [
  {
    id: "prototype-storefront",
    name: "Storefront",
    slug: "storefront",
    repo: "github.com/plorigo/storefront",
    framework: "Next.js",
    url: "storefront.plorigo.app",
    branch: "main",
    commit: "a1b2c3d",
    status: "healthy",
    source: "prototype",
    updated: "8m ago",
    environments: [
      { name: "Production", tone: "green" },
      { name: "Preview x2", tone: "blue" },
      { name: "Staging", tone: "purple" },
    ],
    services: ["Web", "Worker", "DB"],
    owner: "Platform",
    readiness: 98,
    collaborators: 3,
    sparkline: [34, 28, 48, 40, 56, 44, 52, 36, 46],
    kind: "web",
  },
  {
    id: "prototype-api",
    name: "Payments API",
    slug: "payments-api",
    repo: "github.com/plorigo/payments-api",
    framework: "Express",
    url: "api.plorigo.com",
    branch: "main",
    commit: "d4e5f6a",
    status: "healthy",
    source: "prototype",
    updated: "35m ago",
    environments: [
      { name: "Production", tone: "green" },
      { name: "Preview x1", tone: "blue" },
      { name: "Dev", tone: "neutral" },
    ],
    services: ["Web", "DB"],
    owner: "Billing",
    readiness: 94,
    collaborators: 2,
    sparkline: [18, 28, 20, 38, 52, 34, 46, 24, 42],
    kind: "api",
  },
  {
    id: "prototype-worker",
    name: "Queue Worker",
    slug: "queue-worker",
    repo: "github.com/plorigo/worker",
    framework: "Go",
    url: "worker.plorigo.app",
    branch: "main",
    commit: "9f8e7d6",
    status: "warning",
    source: "prototype",
    updated: "1h ago",
    environments: [
      { name: "Production", tone: "green" },
      { name: "Dev", tone: "neutral" },
    ],
    services: ["Worker", "Redis"],
    owner: "Platform",
    readiness: 82,
    collaborators: 2,
    sparkline: [30, 18, 22, 44, 28, 42, 26, 30, 18],
    kind: "worker",
  },
  {
    id: "prototype-marketing",
    name: "Marketing Site",
    slug: "marketing-site",
    repo: "github.com/plorigo/marketing",
    framework: "Next.js",
    url: "marketing-plorigo.vercel.app",
    branch: "main",
    commit: "b7c6d5e",
    status: "healthy",
    source: "prototype",
    updated: "2h ago",
    environments: [
      { name: "Preview x2", tone: "blue" },
      { name: "Staging", tone: "purple" },
    ],
    services: ["Web"],
    owner: "Growth",
    readiness: 97,
    collaborators: 1,
    sparkline: [18, 20, 46, 48, 34, 20, 36, 26, 42],
    kind: "web",
  },
  {
    id: "prototype-auth",
    name: "Auth Service",
    slug: "auth-service",
    repo: "github.com/plorigo/auth-service",
    framework: "FastAPI",
    url: "auth.plorigo.com",
    branch: "main",
    commit: "e3d2c1b",
    status: "healthy",
    source: "prototype",
    updated: "3h ago",
    environments: [
      { name: "Production", tone: "green" },
      { name: "Staging", tone: "purple" },
    ],
    services: ["Web", "DB"],
    owner: "Identity",
    readiness: 93,
    collaborators: 2,
    sparkline: [20, 22, 36, 56, 40, 44, 24, 32, 38],
    kind: "service",
  },
  {
    id: "prototype-analytics",
    name: "Analytics",
    slug: "analytics",
    repo: "github.com/plorigo/analytics",
    framework: "Python",
    url: "analytics.plorigo.app",
    branch: "main",
    commit: "6a5b4c3",
    status: "attention",
    source: "prototype",
    updated: "5h ago",
    environments: [{ name: "Dev", tone: "neutral" }],
    services: ["Web", "DB"],
    owner: "Data",
    readiness: 68,
    collaborators: 1,
    sparkline: [26, 16, 34, 20, 40, 18, 34, 20, 24],
    kind: "analytics",
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
    readiness: "ready",
    dockerVersion: "27.1.1",
    os: "linux",
    arch: "amd64",
    source: "prototype",
  },
  {
    name: "helsinki-1",
    region: "EU North",
    cpu: "64%",
    memory: "10.8 / 16 GB",
    disk: "191 / 240 GB",
    status: "degraded",
    readiness: "degraded",
    readinessReason:
      "Docker isn't reachable on this server. Install or start Docker; the agent recovers automatically once it's running.",
    os: "linux",
    arch: "amd64",
    source: "prototype",
  },
  {
    name: "preview-pool",
    region: "Auto",
    cpu: "0%",
    memory: "standby",
    disk: "standby",
    status: "offline",
    readiness: "unavailable",
    readinessReason:
      "Agent offline — no heartbeat in over 90 seconds. Check the machine is on and the plorigo-agent service is running.",
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
