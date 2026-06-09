import { AlertTriangle, ChevronDown } from "lucide-react";

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

// Plain-English failure summary with the raw details one click away — the
// "explain failures simply, raw details always available" principle. The caller
// passes a human headline, a suggested fix, and the raw log lines.
export function FailureSummary({
  headline,
  suggestion,
  logs,
}: {
  headline: string;
  suggestion?: string;
  logs: string[];
}) {
  return (
    <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4">
      <div className="flex items-start gap-3">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-destructive/20 bg-destructive/10 text-destructive">
          <AlertTriangle className="h-5 w-5" aria-hidden="true" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-foreground">{headline}</p>
          {suggestion && <p className="mt-1 text-sm leading-6 text-muted-foreground">{suggestion}</p>}

          <Collapsible className="mt-3">
            <CollapsibleTrigger className="group inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition hover:text-foreground">
              <ChevronDown className="h-3.5 w-3.5 transition group-data-[state=open]:rotate-180" aria-hidden="true" />
              View raw logs
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="mt-2 space-y-1 rounded-lg bg-zinc-950 p-3 font-mono text-xs text-zinc-100">
                {logs.map((line, index) => (
                  <p key={index} className="truncate">
                    {line}
                  </p>
                ))}
              </div>
            </CollapsibleContent>
          </Collapsible>
        </div>
      </div>
    </div>
  );
}
