import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";

import { AppShell } from "./app/AppShell";
import { Protected } from "./components/Protected";
import { ActivityPage } from "./features/activity/ActivityPage";
import { BackupsPage } from "./features/backups/BackupsPage";
import { DeploymentDetailPage } from "./features/deployments/DeploymentDetailPage";
import { DeploymentsPage } from "./features/deployments/DeploymentsPage";
import { NewDeploymentPage } from "./features/deployments/new/NewDeploymentPage";
import { DomainsPage } from "./features/domains/DomainsPage";
import { EnvironmentDetailPage } from "./features/environments/EnvironmentDetailPage";
import { OverviewPage } from "./features/overview/OverviewPage";
import { ProjectDetailPage } from "./features/projects/ProjectDetailPage";
import { ProjectsPage } from "./features/projects/ProjectsPage";
import { NewProjectPage } from "./features/projects/new/NewProjectPage";
import { EnvironmentVariablesPage } from "./features/variables/EnvironmentVariablesPage";
import { ServiceDetailPage } from "./features/services/ServiceDetailPage";
import { SecurityPage } from "./features/security/SecurityPage";
import { ServersPage } from "./features/servers/ServersPage";
import { TeamPage } from "./features/team/TeamPage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";
import { VerifyEmailPage } from "./pages/VerifyEmailPage";

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

// Protected layout: the auth gate + app shell mount once and persist across
// section navigation; each section is a child route under it.
const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  component: () => (
    <Protected>
      <AppShell />
    </Protected>
  ),
});

const overviewRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/", component: OverviewPage });
const projectsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/projects", component: ProjectsPage });
const projectsNewRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/new",
  component: NewProjectPage,
});
const projectDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId",
  component: ProjectDetailPage,
});
const serviceDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/services/$serviceId",
  component: ServiceDetailPage,
});
const environmentDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/environments/$environmentId",
  component: EnvironmentDetailPage,
});
const domainsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/domains", component: DomainsPage });
const projectDomainsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/domains",
  component: DomainsPage,
});
const serversRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/servers", component: ServersPage });
const variablesRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/variables", component: EnvironmentVariablesPage });
const projectVariablesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/variables",
  component: EnvironmentVariablesPage,
});
const deploymentsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/deployments", component: DeploymentsPage });
const projectDeploymentsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/deployments",
  component: DeploymentsPage,
});
// Static "/deployments/new" must be registered before the dynamic "/deployments/$deploymentId"
// so it's never parsed as a deployment id (mirrors projectsNewRoute vs projectDetailRoute).
const deploymentsNewRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/deployments/new",
  component: NewDeploymentPage,
  validateSearch: (s: Record<string, unknown>): { project?: string } =>
    typeof s.project === "string" && s.project ? { project: s.project } : {},
});
const deploymentDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/deployments/$deploymentId",
  component: DeploymentDetailPage,
});
const projectDeploymentDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/deployments/$deploymentId",
  component: DeploymentDetailPage,
});
const backupsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/backups", component: BackupsPage });
const securityRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/security", component: SecurityPage });
const teamRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/team", component: TeamPage });
const activityRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/activity", component: ActivityPage });
const projectActivityRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/projects/$projectId/activity",
  component: ActivityPage,
});

// The auth screens are public — siblings of the protected layout.
const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const registerRoute = createRoute({ getParentRoute: () => rootRoute, path: "/register", component: RegisterPage });
const forgotRoute = createRoute({ getParentRoute: () => rootRoute, path: "/forgot", component: ForgotPasswordPage });
const resetRoute = createRoute({ getParentRoute: () => rootRoute, path: "/reset", component: ResetPasswordPage });
const verifyRoute = createRoute({ getParentRoute: () => rootRoute, path: "/verify", component: VerifyEmailPage });

export const routeTree = rootRoute.addChildren([
  appLayoutRoute.addChildren([
    overviewRoute,
    projectsRoute,
    projectsNewRoute,
    projectDetailRoute,
    serviceDetailRoute,
    environmentDetailRoute,
    domainsRoute,
    projectDomainsRoute,
    serversRoute,
    variablesRoute,
    projectVariablesRoute,
    deploymentsRoute,
    projectDeploymentsRoute,
    deploymentsNewRoute,
    deploymentDetailRoute,
    projectDeploymentDetailRoute,
    backupsRoute,
    securityRoute,
    teamRoute,
    activityRoute,
    projectActivityRoute,
  ]),
  loginRoute,
  registerRoute,
  forgotRoute,
  resetRoute,
  verifyRoute,
]);
