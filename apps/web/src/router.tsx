import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";

import { Protected } from "./components/Protected";
import { ProjectsPage } from "./pages/ProjectsPage";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";
import { VerifyEmailPage } from "./pages/VerifyEmailPage";

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

// The dashboard is gated; the auth screens are public. Each route is declared
// explicitly so TanStack Router infers its path for type-safe <Link>/navigate.
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: () => (
    <Protected>
      <ProjectsPage />
    </Protected>
  ),
});

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const registerRoute = createRoute({ getParentRoute: () => rootRoute, path: "/register", component: RegisterPage });
const forgotRoute = createRoute({ getParentRoute: () => rootRoute, path: "/forgot", component: ForgotPasswordPage });
const resetRoute = createRoute({ getParentRoute: () => rootRoute, path: "/reset", component: ResetPasswordPage });
const verifyRoute = createRoute({ getParentRoute: () => rootRoute, path: "/verify", component: VerifyEmailPage });

export const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  registerRoute,
  forgotRoute,
  resetRoute,
  verifyRoute,
]);
