import { fileURLToPath, URL } from "node:url";

import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Component tests run under jsdom with the same "@/" alias the app uses. We keep this
// separate from vite.config.ts (which carries the Tailwind plugin and dev proxy) so the
// test runner pulls in only what it needs.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    globals: false,
    // Clear mock call history between tests (impls are re-set in each suite's beforeEach).
    clearMocks: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
