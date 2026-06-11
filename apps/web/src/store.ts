import { create } from "zustand";

// Small client-only state (see docs/architecture/dashboard.md). Server state lives
// in TanStack Query; Zustand holds purely-local UI state like the workspace and
// project filters. An empty projectId means "All projects" — pages then show the
// whole workspace; a non-empty id narrows the dashboard to that project.
interface WorkspaceState {
  workspaceId: string;
  setWorkspaceId: (id: string) => void;
  projectId: string;
  setProjectId: (id: string) => void;
  clearProject: () => void;
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  workspaceId: "",
  // Switching workspace always clears the project filter so a project id never
  // leaks from one workspace into another.
  setWorkspaceId: (id) => set({ workspaceId: id, projectId: "" }),
  projectId: "",
  setProjectId: (id) => set({ projectId: id }),
  clearProject: () => set({ projectId: "" }),
}));
