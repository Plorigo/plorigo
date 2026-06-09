import { Shield } from "lucide-react";

import { ComingSoon } from "@/components/ComingSoon";
import { PageHeader } from "@/components/PageHeader";
import { Panel, PanelHeader, StatusDot } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { type Tone } from "@/lib/status";

const guardrails: Array<[string, string, Tone]> = [
  ["Recovery path", "Rollback target kept", "green"],
  ["Secret handling", "Write-only values", "green"],
  ["AI operations", "Approval required", "amber"],
  ["Audit trail", "Action history", "green"],
];

export function SecurityPage() {
  const demo = useDemoData();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Security"
        description="Production guardrails: recovery paths, secret handling, AI approvals, and audit."
      />
      {demo ? (
        <Panel>
          <PanelHeader title="Production guardrails" description="Modeled from the architecture principles." />
          <div className="grid gap-3 p-4 sm:grid-cols-2">
            {guardrails.map(([label, value, tone]) => (
              <div key={label} className="rounded-xl border border-border bg-background p-4">
                <StatusDot tone={tone} label={label} />
                <p className="mt-3 text-sm font-semibold text-foreground">{value}</p>
              </div>
            ))}
          </div>
        </Panel>
      ) : (
        <ComingSoon
          icon={Shield}
          title="Security center"
          description="A single place to see exposed ports, public services, dangerous Docker options, SSL and backup status, the secrets policy, and the audit log."
          points={[
            "Exposed ports & public services",
            "Dangerous Docker option detection",
            "SSL & backup posture",
            "Audit log of every change",
          ]}
        />
      )}
    </div>
  );
}
