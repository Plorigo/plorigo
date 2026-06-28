import { useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { DatabaseBackup, TriangleAlert } from "lucide-react";
import { toast } from "sonner";

import { Button, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import type { Backup } from "@/gen/controlplane/v1/backups_pb";
import { backupClient } from "@/lib/clients";
import { useBackupsByService } from "@/lib/queries";
import type { Tone } from "@/lib/status";

// BackupsPanel manages a managed Postgres service's backups: take a new one, and see the status
// and failures of past ones. A backup runs on the database's server agent (pg_dump to the server's
// own disk), so its status streams in as the agent reports progress.
export function BackupsPanel({ serviceId }: { serviceId: string }) {
  const queryClient = useQueryClient();
  const backups = useBackupsByService(serviceId, true);
  const [creating, setCreating] = useState(false);
  const rows = backups.data ?? [];

  async function createBackup() {
    setCreating(true);
    try {
      await backupClient.createBackup({ serviceId });
      await queryClient.invalidateQueries({ queryKey: ["backups", "service", serviceId] });
      toast.success("Backup started");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not start the backup");
    } finally {
      setCreating(false);
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Backups"
        description="Snapshot this database with pg_dump. Backups run on the database's server and you can see each one's status here."
        action={
          <Button size="sm" disabled={creating} onClick={() => void createBackup()}>
            <DatabaseBackup className="h-4 w-4" aria-hidden="true" />
            {creating ? "Starting…" : "Back up now"}
          </Button>
        }
      />
      <div className="p-4">
        <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2.5 text-xs leading-5 text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <p>
            Backups are written to the <span className="font-medium">server&rsquo;s own disk</span> for now. Off-server
            (S3-compatible) destinations and scheduled backups are coming in a later release.
          </p>
        </div>
        {backups.isLoading && rows.length === 0 ? (
          <Skeleton className="h-32 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No backups yet"
            body="Click “Back up now” to snapshot this database. The database must be running first."
          />
        ) : (
          <div className="space-y-2">
            {rows.map((b) => (
              <BackupRow key={b.id} backup={b} />
            ))}
          </div>
        )}
      </div>
    </Panel>
  );
}

function BackupRow({ backup }: { backup: Backup }) {
  const detail = backup.status === "failed" && backup.error ? backup.error : backup.message;
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2.5">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <StatusDot tone={backupTone(backup.status)} label={backup.status} />
          {backup.sizeBytes > 0n && (
            <span className="text-xs text-muted-foreground">{formatBytes(backup.sizeBytes)}</span>
          )}
        </div>
        {detail && <p className="mt-0.5 truncate text-xs text-muted-foreground">{detail}</p>}
      </div>
      <span className="shrink-0 text-xs text-muted-foreground">{timeAgo(backup.createdAt)}</span>
    </div>
  );
}

// backupTone colors a backup's status: green when it succeeded, red when it failed, blue while it
// is still running. statusTone doesn't know the backup vocabulary, so map it here.
function backupTone(status: string): Tone {
  if (status === "succeeded") return "green";
  if (status === "failed") return "red";
  return "blue";
}

function formatBytes(bytes: bigint): string {
  const n = Number(bytes);
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  return `${(n / 1024 ** 3).toFixed(1)} GB`;
}

function timeAgo(iso: string): string {
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return new Date(iso).toLocaleDateString();
}
