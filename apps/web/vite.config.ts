import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// During development the dashboard runs on the Vite dev server and proxies RPC
// calls to the control plane. In the production single binary the control plane
// serves both the UI and the API from the same origin, so no proxy is needed.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "dist" },
  server: {
    proxy: {
      // Each ConnectRPC service is a path prefix proxied to the control plane.
      "/controlplane.v1.AuthService": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/controlplane.v1.WorkspaceService": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/controlplane.v1.ProjectService": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
