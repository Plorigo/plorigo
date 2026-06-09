import { useState } from "react";
import { Menu, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { DemoBadge } from "@/components/DemoBadge";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { useCommandMenu } from "./commandMenuStore";
import { Sidebar } from "./Sidebar";

// Global top chrome. Page titles live in each page's <PageHeader>, so this stays
// lean: mobile menu, the ⌘K search trigger, theme, demo indicator, primary action.
export function Topbar() {
  const toggleCommand = useCommandMenu((s) => s.toggle);
  const [sheetOpen, setSheetOpen] = useState(false);

  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border bg-background/80 px-4 backdrop-blur sm:px-6">
      <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <SheetTrigger asChild>
          <Button variant="ghost" size="icon" className="lg:hidden" aria-label="Open menu">
            <Menu className="h-5 w-5" aria-hidden="true" />
          </Button>
        </SheetTrigger>
        <SheetContent side="left" className="w-72 p-0">
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <Sidebar onNavigate={() => setSheetOpen(false)} />
        </SheetContent>
      </Sheet>

      <button
        type="button"
        onClick={toggleCommand}
        className="flex h-9 max-w-md flex-1 items-center gap-2 rounded-md border border-border bg-card px-3 text-sm text-muted-foreground shadow-sm outline-none transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Search className="h-4 w-4 shrink-0" aria-hidden="true" />
        <span className="truncate">Search or jump to…</span>
        <kbd className="ml-auto hidden items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground sm:inline-flex">
          ⌘K
        </kbd>
      </button>

      <div className="ml-auto flex items-center gap-2">
        <DemoBadge />
        <ThemeToggle />
        <Button
          size="sm"
          onClick={() => toast.info("Creating deployments from the dashboard is coming soon.")}
        >
          <Plus className="h-4 w-4" aria-hidden="true" />
          <span className="hidden sm:inline">New deployment</span>
        </Button>
      </div>
    </header>
  );
}
