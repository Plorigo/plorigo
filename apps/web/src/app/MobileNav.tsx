import { Link } from "@tanstack/react-router";

import { useEffectiveProjectId } from "@/lib/projectScope";
import { mobileNavItems } from "./nav";

// Fixed bottom navigation on small screens. The full section list stays reachable
// from the sidebar sheet in the topbar.
export function MobileNav() {
  const projectId = useEffectiveProjectId();

  return (
    <nav className="fixed inset-x-3 bottom-3 z-30 grid grid-cols-5 rounded-xl border border-border bg-card/95 p-1 shadow-card backdrop-blur lg:hidden">
      {mobileNavItems.map((item) => {
        const Icon = item.icon;
        if (projectId && item.projectTo) {
          return (
            <Link
              key={item.to}
              to={item.projectTo}
              params={{ projectId }}
              activeOptions={{ exact: item.projectTo === "/projects/$projectId" }}
              className="flex h-12 min-w-0 flex-col items-center justify-center gap-1 rounded-lg text-[11px] font-medium transition"
              activeProps={{ className: "bg-accent text-accent-foreground" }}
              inactiveProps={{ className: "text-muted-foreground hover:text-foreground" }}
            >
              <Icon className="h-4 w-4" aria-hidden="true" />
              <span className="max-w-full truncate">{item.label}</span>
            </Link>
          );
        }
        return (
          <Link
            key={item.to}
            to={item.to}
            activeOptions={{ exact: item.to === "/" }}
            className="flex h-12 min-w-0 flex-col items-center justify-center gap-1 rounded-lg text-[11px] font-medium transition"
            activeProps={{ className: "bg-accent text-accent-foreground" }}
            inactiveProps={{ className: "text-muted-foreground hover:text-foreground" }}
          >
            <Icon className="h-4 w-4" aria-hidden="true" />
            <span className="max-w-full truncate">{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
