import { CheckCircle2, Circle, Loader2, XCircle } from "lucide-react";

import { cn } from "@/lib/cn";

export type TimelineStatus = "done" | "active" | "pending" | "failed";

export interface TimelineStep {
  label: string;
  detail?: string;
  value?: string;
  status: TimelineStatus;
}

const iconFor: Record<TimelineStatus, typeof CheckCircle2> = {
  done: CheckCircle2,
  active: Loader2,
  pending: Circle,
  failed: XCircle,
};

const colorFor: Record<TimelineStatus, string> = {
  done: "text-success",
  active: "text-info",
  pending: "text-muted-foreground",
  failed: "text-destructive",
};

// Reusable deploy/process timeline — Build → Health → Route → Live. Satisfies the
// contract's "every deploy has a timeline" pattern; data is wired by the caller.
export function Timeline({ steps }: { steps: TimelineStep[] }) {
  return (
    <ol className="space-y-1">
      {steps.map((step, index) => {
        const Icon = iconFor[step.status];
        const isLast = index === steps.length - 1;
        return (
          <li key={step.label} className="flex gap-3">
            <div className="flex flex-col items-center">
              <Icon
                className={cn("h-5 w-5 shrink-0", colorFor[step.status], step.status === "active" && "animate-spin")}
                aria-hidden="true"
              />
              {!isLast && <span className="my-0.5 w-px flex-1 bg-border" />}
            </div>
            <div className={cn("min-w-0 flex-1", isLast ? "pb-0" : "pb-4")}>
              <div className="flex items-center justify-between gap-3">
                <p className="truncate text-sm font-medium text-foreground">{step.label}</p>
                {step.value && <span className="shrink-0 text-sm text-muted-foreground">{step.value}</span>}
              </div>
              {step.detail && <p className="mt-0.5 truncate text-xs text-muted-foreground">{step.detail}</p>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
