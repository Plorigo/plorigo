import { useState } from "react";
import { Boxes, GitBranch } from "lucide-react";

import { Badge } from "@/components/ui";
import { cn } from "@/lib/cn";
import { deployTemplates, templateSourceKind, type DeployTemplate, type TemplateCategory } from "@/lib/templates";

const CATEGORIES: Array<"All" | TemplateCategory> = ["All", "Starter", "Web", "API"];

// TemplateGallery is the launchpad's "Clone Template" column: a category filter
// over the curated list, each card selecting a template to prefill the wizard.
export function TemplateGallery({ onSelect }: { onSelect: (template: DeployTemplate) => void }) {
  const [category, setCategory] = useState<"All" | TemplateCategory>("All");
  const templates =
    category === "All" ? deployTemplates : deployTemplates.filter((t) => t.category === category);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-1.5">
        {CATEGORIES.map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => setCategory(c)}
            className={cn(
              "h-7 rounded-md px-2.5 text-xs font-medium transition",
              category === c
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
            )}
          >
            {c}
          </button>
        ))}
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        {templates.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => onSelect(t)}
            className="flex flex-col gap-1 rounded-lg border border-border bg-card p-3 text-left shadow-sm transition hover:border-ring hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="flex min-w-0 items-center gap-1.5 text-sm font-medium text-foreground">
                {templateSourceKind(t) === "image" ? (
                  <Boxes className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                ) : (
                  <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                )}
                <span className="truncate">{t.name}</span>
              </span>
              <Badge tone="neutral">{t.category}</Badge>
            </div>
            <p className="text-xs leading-5 text-muted-foreground">{t.description}</p>
          </button>
        ))}
      </div>
    </div>
  );
}
