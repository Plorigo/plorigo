import { Link } from "@tanstack/react-router";
import { LogOut } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useCurrentUser, useLogout } from "@/lib/auth";
import { plorigoLogoBlack } from "@/lib/brand";
import { useWorkspaces } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";
import { navItems } from "./nav";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

// The desktop sidebar (and the body of the mobile sheet). Nav items are real
// <Link>s so routes are deep-linkable and the active state comes from the router.
export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const { data: user } = useCurrentUser();
  const workspaces = useWorkspaces();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const logout = useLogout();

  const currentWorkspace = workspaces.data?.find((w) => w.id === workspaceId);
  const initial = user?.email?.slice(0, 1).toUpperCase() ?? "P";

  return (
    <div className="flex h-full flex-col bg-sidebar">
      <div className="flex h-14 items-center px-4">
        <img src={plorigoLogoBlack} alt="Plorigo" className="h-7 w-auto dark:invert" />
      </div>

      <div className="px-3 pb-2">
        <WorkspaceSwitcher />
      </div>

      <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 py-2">
        <p className="px-2 pb-1 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
          Dashboard
        </p>
        {navItems.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.to}
              to={item.to}
              onClick={onNavigate}
              activeOptions={{ exact: item.to === "/" }}
              className="flex h-9 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition"
              activeProps={{ className: "bg-accent text-accent-foreground" }}
              inactiveProps={{
                className: "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
              }}
            >
              <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
              <span className="truncate">{item.label}</span>
            </Link>
          );
        })}
      </nav>

      <div className="border-t border-sidebar-border p-3">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-md px-1.5 py-1.5 text-left outline-none transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-primary text-xs font-semibold text-primary-foreground">
                {initial}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium text-foreground">
                  {user?.email ?? "Account"}
                </span>
                <span className="block truncate text-[11px] text-muted-foreground">
                  {currentWorkspace?.name ?? "Workspace"}
                </span>
              </span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">
              {user?.email}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void logout()}>
              <LogOut className="h-4 w-4" aria-hidden="true" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
