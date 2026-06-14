import { useNavigate } from "@tanstack/react-router";
import { Box, Boxes, ChevronDown, Plus, Server, Users } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useWorkspaceStore } from "@/store";

// AddNewMenu is the Vercel-style "Add New" quick-add menu in the topbar: one dropdown
// of shortcuts to the things you create most, instead of a single fixed primary action.
// Every item routes to the matching flow. The trigger mirrors the raw-button pattern used
// by WorkspaceSwitcher (Radix needs the trigger node to anchor the menu, so we don't wrap
// the custom Button component).
export function AddNewMenu() {
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          disabled={!workspaceId}
          className="inline-flex h-9 shrink-0 items-center gap-2 rounded-md border border-primary bg-primary px-3 text-sm font-medium text-primary-foreground shadow-sm outline-none transition hover:bg-primary/90 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:border-border disabled:bg-muted disabled:text-muted-foreground"
        >
          <Plus className="h-4 w-4" aria-hidden="true" />
          <span className="hidden sm:inline">Add New</span>
          <ChevronDown className="h-4 w-4 opacity-80" aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuItem onClick={() => navigate({ to: "/projects/new" })}>
          <Box className="h-4 w-4" aria-hidden="true" />
          Project
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => navigate({ to: "/deployments/new", search: {} })}>
          <Boxes className="h-4 w-4" aria-hidden="true" />
          Service
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => navigate({ to: "/servers" })}>
          <Server className="h-4 w-4" aria-hidden="true" />
          Server
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => navigate({ to: "/team" })}>
          <Users className="h-4 w-4" aria-hidden="true" />
          Team Member
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
