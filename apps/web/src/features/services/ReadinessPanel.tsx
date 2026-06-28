import { ArrowRight, CircleCheck, CircleDashed, CircleX, TriangleAlert } from "lucide-react";

import { Badge, EmptyState, Panel, PanelHeader, Skeleton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { errorMessage } from "@/lib/format";
import { useServiceReadiness } from "@/lib/queries";
import { intentSoft, intentText, type Intent, type Tone } from "@/lib/status";
import type { ReadinessCheck } from "@/gen/controlplane/v1/readiness_pb";

// ReadinessPanel renders the Production Readiness Doctor for a service: the overall verdict first
// (progressive disclosure), then each deterministic check with its severity, plain-English detail,
// and — when it isn't passing — the next step to fix it. Critical issues are visually distinct
// from advisory warnings.
export function ReadinessPanel({ serviceId }: { serviceId: string }) {
  const readiness = useServiceReadiness(serviceId);
  const error = errorMessage(readiness.error);
  const checklist = readiness.data;

  const verdict = checklist ? overallVerdict(checklist.overallLevel) : null;

  return (
    <Panel>
      <PanelHeader
        title="Production readiness"
        description="Deterministic checks that tell you whether this service is safe to launch — and what to fix next."
        action={verdict && <Badge tone={verdict.tone}>{verdict.label}</Badge>}
      />
      {readiness.isLoading && !checklist ? (
        <div className="p-4">
          <Skeleton className="h-40 w-full" />
        </div>
      ) : error ? (
        <div className="p-4">
          <EmptyState title="Couldn't run readiness checks" body={error} />
        </div>
      ) : !checklist || checklist.checks.length === 0 ? (
        <div className="p-4">
          <EmptyState title="No checks to run" body="Deploy this service to start seeing readiness checks." />
        </div>
      ) : (
        <ul className="divide-y divide-border">
          {checklist.checks.map((check, i) => (
            <CheckRow key={`${check.category}-${i}`} check={check} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function CheckRow({ check }: { check: ReadinessCheck }) {
  const visual = stateVisual(check.state);
  const Icon = visual.icon;
  return (
    <li className="flex items-start gap-3 px-4 py-3">
      <span className={cn("mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-lg border", intentSoft[visual.intent])}>
        <Icon className="h-4 w-4" aria-hidden="true" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium text-foreground">{check.title}</p>
          {check.severity === "critical" && check.state === "fail" && (
            <Badge tone="red">Critical</Badge>
          )}
        </div>
        <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{check.detail}</p>
        {check.remediation && (
          <p className={cn("mt-1 flex items-start gap-1.5 text-xs leading-5", intentText[visual.intent])}>
            <ArrowRight className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            <span>{check.remediation}</span>
          </p>
        )}
      </div>
    </li>
  );
}

// overallVerdict maps the checklist's overall level to a labeled, toned badge.
function overallVerdict(level: string): { label: string; tone: Tone } {
  switch (level) {
    case "ready":
      return { label: "Ready for production", tone: "green" };
    case "almost_ready":
      return { label: "Almost ready", tone: "amber" };
    case "not_ready":
      return { label: "Not production-ready", tone: "red" };
    default:
      return { label: level, tone: "neutral" };
  }
}

// stateVisual maps a check's state to its icon + intent color. Critical-vs-advisory is carried by
// the state (fail vs warn), so the icon alone distinguishes them.
function stateVisual(state: string): { icon: typeof CircleCheck; intent: Intent } {
  switch (state) {
    case "pass":
      return { icon: CircleCheck, intent: "success" };
    case "warn":
      return { icon: TriangleAlert, intent: "warning" };
    case "fail":
      return { icon: CircleX, intent: "danger" };
    default:
      return { icon: CircleDashed, intent: "neutral" };
  }
}
