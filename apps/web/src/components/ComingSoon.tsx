import type { LucideIcon } from "lucide-react";
import { Sparkles } from "lucide-react";

import { Panel } from "./ui";

// Honest placeholder for features whose backend isn't built yet. Instead of fake
// data, it states plainly what the feature will do (drawn from the architecture
// contract) so the product reads as intentional, not unfinished.
export function ComingSoon({
  icon: Icon,
  title,
  description,
  points,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  points?: string[];
}) {
  return (
    <Panel className="overflow-hidden">
      <div className="flex flex-col items-center gap-4 px-6 py-12 text-center">
        <span className="grid h-14 w-14 place-items-center rounded-2xl border border-border bg-muted text-muted-foreground">
          <Icon className="h-6 w-6" aria-hidden="true" />
        </span>
        <div className="max-w-md">
          <div className="flex items-center justify-center gap-2">
            <h2 className="text-lg font-semibold text-foreground">{title}</h2>
            <span className="inline-flex items-center gap-1 rounded-full border border-info/20 bg-info/10 px-2 py-0.5 text-[11px] font-medium text-info">
              <Sparkles className="h-3 w-3" aria-hidden="true" />
              Coming soon
            </span>
          </div>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        {points && points.length > 0 && (
          <ul className="grid w-full max-w-lg gap-2 text-left sm:grid-cols-2">
            {points.map((point) => (
              <li
                key={point}
                className="rounded-lg border border-border bg-background px-3 py-2 text-xs leading-5 text-muted-foreground"
              >
                {point}
              </li>
            ))}
          </ul>
        )}
      </div>
    </Panel>
  );
}
