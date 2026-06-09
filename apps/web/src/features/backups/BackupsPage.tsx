import { HardDrive } from "lucide-react";

import { ComingSoon } from "@/components/ComingSoon";
import { PageHeader } from "@/components/PageHeader";
import { Panel, PanelHeader, StatusDot } from "@/components/ui";
import { useDemoData } from "@/lib/demo";
import { backups } from "@/lib/mockDashboard";
import { statusTone } from "@/lib/status";

export function BackupsPage() {
  const demo = useDemoData();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Backups"
        description="Backups you can see, test, and restore — not just a checkbox."
      />
      {demo ? (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_340px]">
          <Panel>
            <PanelHeader title="Backups" description="Schedules and last run per target." />
            <div className="space-y-2 p-4">
              {backups.map((backup) => (
                <div
                  key={backup.name}
                  className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2.5"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-foreground">{backup.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {backup.schedule} · last run {backup.lastRun}
                    </p>
                  </div>
                  <StatusDot tone={statusTone(backup.status)} label={backup.status} />
                </div>
              ))}
            </div>
          </Panel>
          <Panel>
            <PanelHeader title="Restore confidence" description="Restore testing keeps backups trustworthy." />
            <div className="space-y-2 p-4">
              {[
                ["Last restore test", "Pending"],
                ["Retention", "14 days"],
                ["Encrypted snapshots", "Enabled"],
              ].map(([label, value]) => (
                <div
                  key={label}
                  className="flex items-center justify-between rounded-lg border border-border bg-background px-3 py-2"
                >
                  <span className="text-sm text-muted-foreground">{label}</span>
                  <span className="text-sm font-semibold text-foreground">{value}</span>
                </div>
              ))}
            </div>
          </Panel>
        </div>
      ) : (
        <ComingSoon
          icon={HardDrive}
          title="Backups & restore center"
          description="Schedule database and volume backups to S3-compatible storage, watch their status, and restore with confidence — including restore-test reminders."
          points={[
            "Postgres & volume backups",
            "S3 / R2 / MinIO targets",
            "One-click restore",
            "Backup-before-migration",
          ]}
        />
      )}
    </div>
  );
}
