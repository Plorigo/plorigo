import { Clock3 } from "lucide-react";

import { ComingSoon } from "@/components/ComingSoon";
import { PageHeader } from "@/components/PageHeader";
import { Panel, PanelHeader } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { activity } from "@/lib/mockDashboard";
import { useEffectiveProjectId } from "@/lib/projectScope";

export function ActivityPage() {
  const demo = useDemoData();
  useEffectiveProjectId();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Activity"
        description="An audit-ready timeline of deploys, secret changes, approvals, and server events."
      />
      {demo ? (
        <Panel>
          <PanelHeader title="Recent activity" description="Deploy, server, secret, and approval events." />
          <div className="divide-y divide-border">
            {activity.map((item) => (
              <div key={`${item.title}-${item.time}`} className="flex gap-3 px-4 py-3">
                <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-info" />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-foreground">{item.title}</p>
                  <p className="mt-1 text-sm leading-6 text-muted-foreground">{item.detail}</p>
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">{item.time}</span>
              </div>
            ))}
          </div>
        </Panel>
      ) : (
        <ComingSoon
          icon={Clock3}
          title="Audit-ready activity"
          description="Every production change — deploys, secret edits, terminal sessions, backups, and permission changes — recorded in one searchable, exportable timeline."
          points={[
            "Deploys & rollbacks",
            "Secret and permission changes",
            "Terminal session records",
            "Filter and export",
          ]}
        />
      )}
    </div>
  );
}
