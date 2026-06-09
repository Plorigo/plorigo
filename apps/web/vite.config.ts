import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const controlPlaneURL = process.env.VITE_CONTROLPLANE_URL ?? "http://localhost:8080";
const controlPlaneServices = [
  "AuthService",
  "WorkspaceService",
  "ProjectService",
  "EnvironmentService",
  "EnvVarService",
  "SecretService",
  "ServerService",
];

// During development the dashboard runs on the Vite dev server and proxies RPC
// calls to the control plane. In the production single binary the control plane
// serves both the UI and the API from the same origin, so no proxy is needed.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "dist" },
  server: {
    proxy: Object.fromEntries(
      controlPlaneServices.map((service) => [
        `/controlplane.v1.${service}`,
        { target: controlPlaneURL, changeOrigin: true },
      ]),
    ),
  },
});
