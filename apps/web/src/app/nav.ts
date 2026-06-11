import {
  Box,
  Clock3,
  HardDrive,
  Home,
  Layers3,
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
  | "/deployments"
  | "/servers"
  | "/resources"
  | "/backups"
  | "/security"
  | "/team"
  | "/activity";

export interface NavItem {
  label: string;
  to: NavTo;
  icon: LucideIcon;
  description: string;
  // Which group the item belongs to when a project is selected (the split nav).
  // With no project selected the nav is one flat list and scope is ignored.
  scope: "project" | "workspace";
}

export const navItems: NavItem[] = [
  { label: "Overview", to: "/", icon: Home, description: "Workspace at a glance", scope: "project" },
  { label: "Projects", to: "/projects", icon: Box, description: "Apps, environments, and health", scope: "workspace" },
  { label: "Deployments", to: "/deployments", icon: Rocket, description: "Release timeline and logs", scope: "project" },
  { label: "Servers", to: "/servers", icon: Server, description: "Owned infrastructure", scope: "workspace" },
  { label: "Resources", to: "/resources", icon: Layers3, description: "Env vars and secrets", scope: "project" },
  { label: "Backups", to: "/backups", icon: HardDrive, description: "Recovery and restore", scope: "workspace" },
  { label: "Security", to: "/security", icon: Shield, description: "Production guardrails", scope: "workspace" },
  { label: "Team", to: "/team", icon: Users, description: "Members and access", scope: "workspace" },
  { label: "Activity", to: "/activity", icon: Clock3, description: "Audit-ready timeline", scope: "project" },
];

// The mobile bottom bar shows the five most-used sections.
export const mobileNavItems = navItems.slice(0, 5);
