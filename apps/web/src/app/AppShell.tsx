import { useEffect } from "react";
import { Outlet } from "@tanstack/react-router";

import { useWorkspaces } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";
import { CommandMenu } from "./CommandMenu";
import { MobileNav } from "./MobileNav";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";

// The authenticated layout: a persistent sidebar + topbar wrapping the routed
// page <Outlet/>. Mounted once by the protected layout route, so navigating
// between sections never remounts the shell.
export function AppShell() {
  const workspaces = useWorkspaces();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);

  // Default to the first workspace until the user picks one.
  useEffect(() => {
    if (workspaces.data && workspaces.data.length > 0) {
      if (!workspaces.data.some((w) => w.id === workspaceId)) {
        setWorkspaceId(workspaces.data[0].id);
      }
    }
  }, [workspaces.data, workspaceId, setWorkspaceId]);

  return (
    <div className="min-h-screen bg-background text-foreground">
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-64 border-r border-sidebar-border lg:block">
        <Sidebar />
      </aside>

      <div className="lg:pl-64">
        <Topbar />
        <main className="mx-auto max-w-7xl px-4 pb-24 pt-6 sm:px-6 lg:px-8 lg:pb-10">
          <Outlet />
        </main>
      </div>

      <MobileNav />
      <CommandMenu />
    </div>
  );
}
