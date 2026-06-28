import { useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { DatabaseBackup, History, RotateCcw, TriangleAlert } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, EmptyState, Panel, PanelHeader, Skeleton, StatusDot } from "@/components/ui";
import type { Backup, RestoreJob } from "@/gen/controlplane/v1/backups_pb";
import { backupClient } from "@/lib/clients";
import { useBackupsByService, useRestoresByService } from "@/lib/queries";
import type { Tone } from "@/lib/status";

// BackupsPanel manages a managed Postgres service's backups and restores: take a new backup, see
// each one's status, and restore a successful backup back into the database (the proof that a
// backup is usable). Both run on the database's server agent, so status streams in as it reports.
export function BackupsPanel({ serviceId }: { serviceId: string }) {
  const queryClient = useQueryClient();
  const backups = useBackupsByService(serviceId, true);
  const restores = useRestoresByService(serviceId, true);
  const [creating, setCreating] = useState(false);
  const [restoringId, setRestoringId] = useState("");
  const rows = backups.data ?? [];
  const restoreRows = restores.data ?? [];

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

  async function restoreBackup(backupId: string) {
    setRestoringId(backupId);
    try {
      await backupClient.restoreBackup({ backupId });
      await queryClient.invalidateQueries({ queryKey: ["restores", "service", serviceId] });
      toast.success("Restore started");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not start the restore");
    } finally {
      setRestoringId("");
    }
  }

  return (
    <Panel>
      <PanelHeader
        title="Backups"
        description="Snapshot this database with pg_dump, and restore a snapshot back into it. Both run on the database's server."
        action={
          <Button size="sm" disabled={creating} onClick={() => void createBackup()}>
            <DatabaseBackup className="h-4 w-4" aria-hidden="true" />
            {creating ? "Starting…" : "Back up now"}
          </Button>
        }
      />
      <div className="space-y-4 p-4">
        <div className="flex items-start gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2.5 text-xs leading-5 text-warning">
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
              <BackupRow
                key={b.id}
                backup={b}
                restoring={restoringId === b.id}
                onRestore={() => void restoreBackup(b.id)}
              />
            ))}
          </div>
        )}

        {restoreRows.length > 0 && (
          <div>
            <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <History className="h-3.5 w-3.5" aria-hidden="true" />
              Restores
            </div>
            <div className="space-y-2">
              {restoreRows.map((r) => (
                <RestoreRow key={r.id} restore={r} />
              ))}
            </div>
          </div>
        )}
      </div>
    </Panel>
  );
}

function BackupRow({ backup, restoring, onRestore }: { backup: Backup; restoring: boolean; onRestore: () => void }) {
  const detail = backup.status === "failed" && backup.error ? backup.error : backup.message;
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2.5">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <StatusDot tone={jobTone(backup.status)} label={backup.status} />
          {backup.sizeBytes > 0n && (
            <span className="text-xs text-muted-foreground">{formatBytes(backup.sizeBytes)}</span>
          )}
        </div>
        {detail && <p className="mt-0.5 truncate text-xs text-muted-foreground">{detail}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-3">
        <span className="text-xs text-muted-foreground">{timeAgo(backup.createdAt)}</span>
        {backup.status === "succeeded" && (
          <ConfirmDialog
            trigger={
              <Button size="sm" variant="secondary" disabled={restoring}>
                <RotateCcw className="h-4 w-4" aria-hidden="true" />
                {restoring ? "Restoring…" : "Restore"}
              </Button>
            }
            title="Restore this backup?"
            description="This overwrites the running database with the contents of this backup, using psql inside the database container."
            recovery="Take a fresh backup first if you want to be able to return to the current state."
            confirmLabel="Restore"
            onConfirm={onRestore}
          />
        )}
      </div>
    </div>
  );
}

function RestoreRow({ restore }: { restore: RestoreJob }) {
  const detail = restore.status === "failed" && restore.error ? restore.error : restore.message;
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2">
      <div className="min-w-0">
        <StatusDot tone={jobTone(restore.status)} label={`restore ${restore.status}`} />
        {detail && <p className="mt-0.5 truncate text-xs text-muted-foreground">{detail}</p>}
      </div>
      <span className="shrink-0 text-xs text-muted-foreground">{timeAgo(restore.createdAt)}</span>
    </div>
  );
}

// jobTone colors a backup/restore status: green when it succeeded, red when it failed, blue while
// it is still running. statusTone doesn't know this vocabulary, so map it here.
function jobTone(status: string): Tone {
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
