import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";

import { ProjectsPage } from "./pages/ProjectsPage";

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: ProjectsPage,
});

export const routeTree = rootRoute.addChildren([indexRoute]);
