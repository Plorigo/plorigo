import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { FlaskConical, LogOut, Monitor, Moon, Sun } from "lucide-react";

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { useLogout } from "@/lib/auth";
import { useDemoStore } from "@/lib/demo";
import { useEffectiveProjectId } from "@/lib/projectScope";
import { useThemeStore } from "@/lib/theme";
import { useCommandMenu } from "./commandMenuStore";
import { navItems } from "./nav";

export function CommandMenu() {
  const open = useCommandMenu((s) => s.open);
  const setOpen = useCommandMenu((s) => s.setOpen);
  const navigate = useNavigate();
  const setTheme = useThemeStore((s) => s.setTheme);
  const toggleDemo = useDemoStore((s) => s.toggleDemo);
  const projectId = useEffectiveProjectId();
  const logout = useLogout();

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        useCommandMenu.getState().toggle();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const run = (action: () => void) => {
    setOpen(false);
    action();
  };

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder="Search or jump to…" />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        <CommandGroup heading="Go to">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <CommandItem
                key={item.to}
                value={`${item.label} ${item.description}`}
                onSelect={() =>
                  run(() => {
                    if (projectId && item.projectTo) {
                      navigate({ to: item.projectTo, params: { projectId } });
                      return;
                    }
                    navigate({ to: item.to });
                  })
                }
              >
                <Icon className="h-4 w-4" aria-hidden="true" />
                <span>{item.label}</span>
                <span className="ml-auto truncate text-xs text-muted-foreground">
                  {item.description}
                </span>
              </CommandItem>
            );
          })}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading="Theme">
          <CommandItem value="theme light" onSelect={() => run(() => setTheme("light"))}>
            <Sun className="h-4 w-4" aria-hidden="true" />
            Light
          </CommandItem>
          <CommandItem value="theme dark" onSelect={() => run(() => setTheme("dark"))}>
            <Moon className="h-4 w-4" aria-hidden="true" />
            Dark
          </CommandItem>
          <CommandItem value="theme system" onSelect={() => run(() => setTheme("system"))}>
            <Monitor className="h-4 w-4" aria-hidden="true" />
            System
          </CommandItem>
        </CommandGroup>
        {import.meta.env.DEV && (
          <CommandGroup heading="Developer">
            <CommandItem value="toggle demo data" onSelect={() => run(toggleDemo)}>
              <FlaskConical className="h-4 w-4" aria-hidden="true" />
              Toggle demo data
            </CommandItem>
          </CommandGroup>
        )}
        <CommandSeparator />
        <CommandGroup heading="Account">
          <CommandItem value="log out sign out" onSelect={() => run(() => void logout())}>
            <LogOut className="h-4 w-4" aria-hidden="true" />
            Log out
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
