import { AlertTriangle, ArrowRight, ChevronDown } from "lucide-react";

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

// Plain-English failure summary with the raw details one click away — the "explain failures
// simply, raw details always available" principle. The caller passes a readable headline, a
// one-line explanation and a fix, optionally the timeline step that failed and a jump-to-logs
// action, the raw reported message, and the relevant log lines.
export function FailureSummary({
  headline,
  explanation,
  fix,
  failedPhase,
  rawMessage,
  logStreamLabel,
  onViewLogs,
  logs,
}: {
  headline: string;
  explanation: string;
  fix: string;
  failedPhase?: string;
  rawMessage?: string;
  logStreamLabel?: string;
  onViewLogs?: () => void;
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
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{explanation}</p>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{fix}</p>

          {/* Link back to the timeline (name the step that failed) and to the relevant logs. */}
          {(failedPhase || onViewLogs) && (
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
              {failedPhase && (
                <span className="text-muted-foreground">
                  Failed at the <span className="font-medium text-foreground">{failedPhase}</span> step
                </span>
              )}
              {onViewLogs && (
                <button
                  type="button"
                  onClick={onViewLogs}
                  className="inline-flex items-center gap-1 font-medium text-blue-400 transition hover:text-blue-300 hover:underline"
                >
                  View {logStreamLabel ? `${logStreamLabel} ` : ""}logs
                  <ArrowRight className="h-3 w-3" aria-hidden="true" />
                </button>
              )}
            </div>
          )}

          {rawMessage && <p className="mt-3 break-all font-mono text-xs text-muted-foreground/80">{rawMessage}</p>}

          <Collapsible className="mt-3">
            <CollapsibleTrigger className="group inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition hover:text-foreground">
              <ChevronDown className="h-3.5 w-3.5 transition group-data-[state=open]:rotate-180" aria-hidden="true" />
              View raw logs
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="mt-2 space-y-1 rounded-lg bg-zinc-950 p-3 font-mono text-xs text-zinc-100">
                {logs.length === 0 ? (
                  <p className="text-zinc-400">No log lines were captured for this failure.</p>
                ) : (
                  logs.map((line, index) => (
                    <p key={index} className="truncate">
                      {line}
                    </p>
                  ))
                )}
              </div>
            </CollapsibleContent>
          </Collapsible>
        </div>
      </div>
    </div>
  );
}
