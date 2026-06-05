import { createConnectTransport } from "@connectrpc/connect-web";

// Same-origin transport. In development Vite proxies the RPC path to the control
// plane (see vite.config.ts); in the production single binary the control plane
// serves both the dashboard and the API. Don't hand-roll fetch wrappers — use the
// generated typed clients with this transport (see docs/architecture/dashboard.md).
export const transport = createConnectTransport({
  baseUrl: "/",
});
