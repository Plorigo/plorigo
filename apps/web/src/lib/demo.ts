import { create } from "zustand";
import { persist } from "zustand/middleware";

// Demo mode fills the dashboard with prototype fixtures (lib/mockDashboard) so
// screenshots and demos look full. It is OFF by default and only takes effect in
// dev builds — production users always see their real data and honest empty
// states. Toggle it from the command palette (⌘K) during development.
interface DemoState {
  demo: boolean;
  setDemo: (demo: boolean) => void;
  toggleDemo: () => void;
}

const initialDemo = import.meta.env.DEV && import.meta.env.VITE_DEMO_DATA === "1";

export const useDemoStore = create<DemoState>()(
  persist(
    (set) => ({
      demo: initialDemo,
      setDemo: (demo) => set({ demo }),
      toggleDemo: () => set((s) => ({ demo: !s.demo })),
    }),
    { name: "plorigo-demo" },
  ),
);

// True only when demo mode is on AND we're in a dev build. Pages call this to
// decide whether to fall back to fixtures when live data is empty.
export function useDemoData(): boolean {
  const demo = useDemoStore((s) => s.demo);
  return demo && import.meta.env.DEV;
}
