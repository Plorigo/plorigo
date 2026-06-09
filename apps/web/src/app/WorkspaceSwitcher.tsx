import { Check, ChevronsUpDown, Plus } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useWorkspaces } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

// Workspace picker used in the sidebar. Backed by the live workspace list and the
// shared Zustand store; every page reads the selected id from the same store.
export function WorkspaceSwitcher() {
  const workspaces = useWorkspaces();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const setWorkspaceId = useWorkspaceStore((s) => s.setWorkspaceId);

  const current = workspaces.data?.find((w) => w.id === workspaceId);
  const name = current?.name ?? (workspaces.isLoading ? "Loading…" : "Workspace");

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="flex w-full items-center gap-2 rounded-md border border-border bg-background px-2 py-1.5 text-left text-sm outline-none transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="grid h-6 w-6 shrink-0 place-items-center rounded bg-primary text-[11px] font-semibold text-primary-foreground">
            {name.slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0 flex-1 truncate font-medium text-foreground">{name}</span>
          <ChevronsUpDown className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-56">
        <DropdownMenuLabel className="text-xs text-muted-foreground">Workspaces</DropdownMenuLabel>
        {workspaces.data?.map((workspace) => (
          <DropdownMenuItem key={workspace.id} onClick={() => setWorkspaceId(workspace.id)}>
            <span className="min-w-0 truncate">{workspace.name}</span>
            {workspace.id === workspaceId && <Check className="ml-auto h-4 w-4" aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
        {!workspaces.data?.length && (
          <DropdownMenuItem disabled>No workspaces yet</DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled>
          <Plus className="h-4 w-4" aria-hidden="true" />
          New workspace
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
