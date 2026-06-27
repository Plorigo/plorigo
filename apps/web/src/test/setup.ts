// Registers @testing-library/jest-dom's matchers (toBeInTheDocument, toBeDisabled, …) on
// vitest's expect and augments its Assertion types project-wide. Loaded via setupFiles.
import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Unmount the React tree after each test. Auto-cleanup isn't registered when vitest globals
// are off, so the rendered DOM would otherwise leak across tests.
afterEach(() => cleanup());

// jsdom doesn't implement these DOM APIs, but Radix UI (dialogs, tabs) calls them. Stub them
// so component tests that open a dialog or switch tabs don't crash.
const proto = Element.prototype as unknown as Record<string, unknown>;
proto.hasPointerCapture ??= () => false;
proto.setPointerCapture ??= () => {};
proto.releasePointerCapture ??= () => {};
proto.scrollIntoView ??= () => {};

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
