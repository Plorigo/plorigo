import { useEffect, useRef } from "react";

import { Button } from "@/components/ui";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/cn";
import { isTerminalSetupStatus, useSetupEvents, useSetupRun } from "@/lib/queries";
import { intentDot } from "@/lib/status";

import { deriveStepStates, SETUP_STEPS, type StepState } from "./setupSteps";

// SetupProgress polls a managed setup run and renders its progress as a step timeline:
// plain-English summary up top, raw log one click away (progressive disclosure). On failure
// it shows the reason and recovery actions (retry, or fall back to the manual command).
export function SetupProgress({
  setupRunId,
  serverName,
  onRetry,
  onFallbackManual,
  onClose,
  onSuccess,
}: {
  setupRunId: string;
  serverName: string;
  onRetry: () => void;
  onFallbackManual: () => void;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const run = useSetupRun(setupRunId);
  const status = run.data?.status ?? "running";
  const live = !run.data || !isTerminalSetupStatus(status);
  const events = useSetupEvents(setupRunId, live);

  // Fire onSuccess exactly once when the run reaches "succeeded" (so the page can refresh the
  // servers/agents lists). A ref guards against the post-success poll re-triggering it.
  const notified = useRef(false);
  useEffect(() => {
    if (status === "succeeded" && !notified.current) {
      notified.current = true;
      onSuccess();
    }
  }, [status, onSuccess]);

  const stepStates = deriveStepStates(events.data ?? [], status);
  const logLines = (events.data ?? [])
    .filter((e) => e.message.trim().length > 0)
    .map((e) => `[${e.step}] ${e.message}`);

  return (
    <div className="space-y-4">
      <ol className="space-y-3">
        {SETUP_STEPS.map((step) => (
          <li key={step.key} className="flex items-start gap-3">
            <StepIndicator state={stepStates[step.key]} />
            <div className="min-w-0">
              <p
                className={cn(
                  "text-sm font-medium",
                  stepStates[step.key] === "pending" ? "text-muted-foreground" : "text-foreground",
                )}
              >
                {step.label}
              </p>
              <p className="truncate text-xs text-muted-foreground">{step.detail}</p>
            </div>
          </li>
        ))}
      </ol>

      {status === "failed" && (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <p className="font-medium">Setup failed</p>
          <p className="mt-1 leading-5">{run.data?.failureReason || "The setup run failed."}</p>
        </div>
      )}

      {status === "succeeded" && (
        <div className="rounded-md border border-success/20 bg-success/10 px-3 py-2 text-sm text-success">
          {serverName} is ready. The agent is online and the machine can deploy.
        </div>
      )}

      {logLines.length > 0 && (
        <Collapsible>
          <CollapsibleTrigger className="text-xs font-medium text-muted-foreground underline-offset-2 hover:underline">
            Show raw log
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-muted p-3 text-xs text-foreground">
              {logLines.join("\n")}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      )}

      <div className="flex flex-wrap justify-end gap-2">
        {status === "failed" && (
          <>
            <Button variant="ghost" size="sm" onClick={onFallbackManual}>
              Use a command instead
            </Button>
            <Button size="sm" onClick={onRetry}>
              Retry setup
            </Button>
          </>
        )}
        {status === "succeeded" && (
          <Button size="sm" onClick={onClose}>
            Done
          </Button>
        )}
        {live && (
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        )}
      </div>
    </div>
  );
}

// StepIndicator renders the small status dot for one timeline step.
function StepIndicator({ state }: { state: StepState }) {
  if (state === "done") {
    return (
      <span
        aria-label="done"
        className={cn("mt-0.5 flex h-4 w-4 items-center justify-center rounded-full text-[10px] font-bold text-white", intentDot.success)}
      >
        ✓
      </span>
    );
  }
  if (state === "failed") {
    return (
      <span
        aria-label="failed"
        className={cn("mt-0.5 flex h-4 w-4 items-center justify-center rounded-full text-[10px] font-bold text-white", intentDot.danger)}
      >
        ✕
      </span>
    );
  }
  if (state === "active") {
    return <span aria-label="in progress" className={cn("mt-1 h-3 w-3 animate-pulse rounded-full", intentDot.info)} />;
  }
  return <span aria-label="pending" className={cn("mt-1 h-3 w-3 rounded-full", intentDot.neutral, "opacity-40")} />;
}
