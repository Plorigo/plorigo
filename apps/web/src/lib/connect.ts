import { createConnectTransport } from "@connectrpc/connect-web";

// Same-origin transport that sends the session cookie (credentials: "include").
// In development Vite proxies the RPC paths to the control plane (see
// vite.config.ts); in the production single binary the control plane serves both
// the dashboard and the API. Don't hand-roll fetch wrappers — use the generated
// typed clients with this transport (see docs/architecture/dashboard.md).
export const transport = createConnectTransport({
  baseUrl: "/",
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});
