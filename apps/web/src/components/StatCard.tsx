import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

import { cn } from "@/lib/cn";
import { intentDot, intentSoft, type Intent } from "@/lib/status";
import { Panel } from "./ui";

// The single stat/metric card for the whole dashboard. Replaces the four
// near-duplicate cards (MetricCard, WireStatCard, ProjectMetricCard, and the
// inline deployment ones). Colors come from the shared intent tokens.
export function StatCard({
  label,
  value,
  detail,
  icon: Icon,
  intent = "neutral",
  trend,
  accentBar = false,
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  icon?: LucideIcon;
  intent?: Intent;
  trend?: ReactNode;
  accentBar?: boolean;
}) {
  return (
    <Panel className="group relative overflow-hidden p-4 transition hover:-translate-y-0.5 hover:shadow-card-hover">
      {accentBar && (
        <div className={cn("absolute inset-x-0 top-0 h-0.5", intentDot[intent])} />
      )}
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {Icon && (
            <span
              className={cn(
                "grid h-8 w-8 shrink-0 place-items-center rounded-lg border",
                intentSoft[intent],
              )}
            >
              <Icon className="h-4 w-4" aria-hidden="true" />
            </span>
          )}
          <p className="truncate text-xs font-medium text-muted-foreground">{label}</p>
        </div>
        {trend && <div className="shrink-0">{trend}</div>}
      </div>
      <div className="mt-3 min-w-0">
        <p className="truncate text-2xl font-semibold tracking-tight text-foreground">{value}</p>
        {detail && <p className="mt-1 truncate text-xs text-muted-foreground">{detail}</p>}
      </div>
    </Panel>
  );
}
