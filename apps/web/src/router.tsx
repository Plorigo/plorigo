import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";

import { AppShell } from "./app/AppShell";
import { Protected } from "./components/Protected";
import { ActivityPage } from "./features/activity/ActivityPage";
import { BackupsPage } from "./features/backups/BackupsPage";
import { DeploymentDetailPage } from "./features/deployments/DeploymentDetailPage";
import { DeploymentsPage } from "./features/deployments/DeploymentsPage";
import { OverviewPage } from "./features/overview/OverviewPage";
import { ProjectDetailPage } from "./features/projects/ProjectDetailPage";
import { ProjectsPage } from "./features/projects/ProjectsPage";
import { NewProjectPage } from "./features/projects/new/NewProjectPage";
import { ResourcesPage } from "./features/resources/ResourcesPage";
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
const serversRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/servers", component: ServersPage });
const resourcesRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/resources", component: ResourcesPage });
const deploymentsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/deployments", component: DeploymentsPage });
const deploymentDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/deployments/$deploymentId",
  component: DeploymentDetailPage,
});
const backupsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/backups", component: BackupsPage });
const securityRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/security", component: SecurityPage });
const teamRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/team", component: TeamPage });
const activityRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: "/activity", component: ActivityPage });

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
    serversRoute,
    resourcesRoute,
    deploymentsRoute,
    deploymentDetailRoute,
    backupsRoute,
    securityRoute,
    teamRoute,
    activityRoute,
  ]),
  loginRoute,
  registerRoute,
  forgotRoute,
  resetRoute,
  verifyRoute,
]);
