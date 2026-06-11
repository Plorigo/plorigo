import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Box, Check, ChevronsUpDown, Plus } from "lucide-react";

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useDashboardProjects } from "@/features/projects/projectData";
import { useWorkspaceStore } from "@/store";

// Project picker shown under the WorkspaceSwitcher. Writes the selected project to the
// shared store (an empty id means "All projects" — the whole workspace). It sources the
// same dashboard-project list the /projects page uses, so it matches what the user sees
// there (including demo fixtures). Selecting a project lands on Overview so the user
// immediately sees that project's overview.
export function ProjectSwitcher({ onNavigate }: { onNavigate?: () => void }) {
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const projectId = useWorkspaceStore((s) => s.projectId);
  const setProjectId = useWorkspaceStore((s) => s.setProjectId);
  const clearProject = useWorkspaceStore((s) => s.clearProject);
  const { dashboardProjects, query } = useDashboardProjects();
  const [open, setOpen] = useState(false);

  const current = dashboardProjects.find((p) => p.id === projectId);
  const label = current?.name ?? (query.isLoading ? "Loading…" : "All projects");

  function select(id: string) {
    setOpen(false);
    if (id) setProjectId(id);
    else clearProject();
    onNavigate?.();
    navigate({ to: "/" });
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex w-full items-center gap-2 rounded-md border border-border bg-background px-2 py-1.5 text-left text-sm outline-none transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="grid h-6 w-6 shrink-0 place-items-center rounded bg-muted text-muted-foreground">
            <Box className="h-3.5 w-3.5" aria-hidden="true" />
          </span>
          <span className="min-w-0 flex-1 truncate font-medium text-foreground">{label}</span>
          <ChevronsUpDown className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-56 p-0">
        <Command>
          <CommandInput placeholder="Find project…" />
          <CommandList>
            <CommandEmpty>No projects found.</CommandEmpty>
            <CommandGroup>
              <CommandItem value="all projects" onSelect={() => select("")}>
                <span className="min-w-0 truncate">All projects</span>
                {!projectId && <Check className="ml-auto h-4 w-4" aria-hidden="true" />}
              </CommandItem>
            </CommandGroup>
            {dashboardProjects.length > 0 && (
              <CommandGroup heading="Projects">
                {dashboardProjects.map((project) => (
                  <CommandItem
                    key={project.id}
                    value={`${project.name} ${project.id}`}
                    onSelect={() => select(project.id)}
                  >
                    <span className="min-w-0 truncate">{project.name}</span>
                    {project.id === projectId && <Check className="ml-auto h-4 w-4" aria-hidden="true" />}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            <CommandSeparator />
            <CommandGroup>
              <CommandItem
                value="add new project"
                disabled={!workspaceId}
                onSelect={() => {
                  if (!workspaceId) return;
                  setOpen(false);
                  onNavigate?.();
                  navigate({ to: "/projects/new" });
                }}
              >
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add project
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
