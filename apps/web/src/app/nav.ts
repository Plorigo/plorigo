import {
  Box,
  Clock3,
  Globe2,
  HardDrive,
  Home,
  Layers3,
  Plug,
  Rocket,
  Server,
  Shield,
  Users,
  type LucideIcon,
} from "lucide-react";

// Single source of truth for the dashboard sections. The literal union keeps
// TanStack Router's typed <Link to> happy while letting the sidebar, mobile nav,
// and command palette share one list. Mirrors the product plan's §7.18 nav.
export type NavTo =
  | "/"
  | "/projects"
  | "/domains"
  | "/deployments"
  | "/servers"
  | "/variables"
  | "/backups"
  | "/integrations"
  | "/security"
  | "/team"
  | "/activity";

export type ProjectNavTo =
  | "/projects/$projectId"
  | "/projects/$projectId/domains"
  | "/projects/$projectId/deployments"
  | "/projects/$projectId/variables"
  | "/projects/$projectId/activity";

export interface NavItem {
  label: string;
  to: NavTo;
  projectTo?: ProjectNavTo;
  icon: LucideIcon;
  description: string;
  // Which group the item belongs to when a project is selected (the split nav).
  // With no project selected the nav is one flat list and scope is ignored.
  scope: "project" | "workspace";
}

export const navItems: NavItem[] = [
  { label: "Overview", to: "/", projectTo: "/projects/$projectId", icon: Home, description: "Workspace at a glance", scope: "project" },
  { label: "Projects", to: "/projects", icon: Box, description: "Apps, environments, and health", scope: "workspace" },
  { label: "Domains", to: "/domains", projectTo: "/projects/$projectId/domains", icon: Globe2, description: "Custom hostnames and DNS", scope: "project" },
  { label: "Deployments", to: "/deployments", projectTo: "/projects/$projectId/deployments", icon: Rocket, description: "Release timeline and logs", scope: "project" },
  { label: "Servers", to: "/servers", icon: Server, description: "Owned infrastructure", scope: "workspace" },
  { label: "Environment Variables", to: "/variables", projectTo: "/projects/$projectId/variables", icon: Layers3, description: "Variables and secrets", scope: "project" },
  { label: "Backups", to: "/backups", icon: HardDrive, description: "Recovery and restore", scope: "workspace" },
  { label: "Integrations", to: "/integrations", icon: Plug, description: "GitHub App and OAuth", scope: "workspace" },
  { label: "Security", to: "/security", icon: Shield, description: "Production guardrails", scope: "workspace" },
  { label: "Team", to: "/team", icon: Users, description: "Members and access", scope: "workspace" },
  { label: "Activity", to: "/activity", projectTo: "/projects/$projectId/activity", icon: Clock3, description: "Audit-ready timeline", scope: "project" },
];

// The mobile bottom bar shows the five most-used sections.
export const mobileNavItems = navItems.slice(0, 5);
