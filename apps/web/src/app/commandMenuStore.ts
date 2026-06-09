import { create } from "zustand";

// Shared open state so the topbar search button and the ⌘K shortcut drive the
// same command palette.
export const useCommandMenu = create<{
  open: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
}>((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
  toggle: () => set((s) => ({ open: !s.open })),
}));
