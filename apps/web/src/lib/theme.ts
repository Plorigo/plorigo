import { create } from "zustand";
import { persist } from "zustand/middleware";

// Theme preference is purely-local UI state (see store.ts). "system" follows the
// OS preference; the ThemeProvider resolves it to a concrete light/dark class.
export type Theme = "light" | "dark" | "system";

interface ThemeState {
  theme: Theme;
  setTheme: (theme: Theme) => void;
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set) => ({
      theme: "system",
      setTheme: (theme) => set({ theme }),
    }),
    { name: "plorigo-theme" },
  ),
);
