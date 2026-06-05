import { create } from "zustand";

// Small client-only state (see docs/architecture/dashboard.md). Server state lives
// in TanStack Query; Zustand holds purely-local UI state like the workspace filter.
interface WorkspaceState {
  workspaceId: string;
  setWorkspaceId: (id: string) => void;
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  workspaceId: "",
  setWorkspaceId: (id) => set({ workspaceId: id }),
}));
