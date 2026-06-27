import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { ServerCog, TerminalSquare, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui";
import { setupClient } from "@/lib/clients";
import { useManagementKey } from "@/lib/queries";

// ServerCardActions groups a server's per-card actions: re-run SSH setup, mint a fresh install
// command, manage the SSH management credential (rotate / revoke), and delete the server.
// "Set up over SSH" / "Re-run setup" is always available so a server first connected with a
// command can later be set up over SSH, and an SSH-managed server can be re-run (e.g. after its
// password changed). Rotate and revoke appear only when the server has an active managed key.
export function ServerCardActions({
  serverId,
  serverName,
  minting,
  onSetup,
  onInstallCommand,
  onDelete,
}: {
  serverId: string;
  serverName: string;
  minting: boolean;
  onSetup: () => void;
  onInstallCommand: () => void;
  onDelete: () => void;
}) {
  const queryClient = useQueryClient();
  const mk = useManagementKey(serverId, true);
  // A revoked credential still returns (with revokedAt set); treat only an active key as present.
  const hasKey = mk.data != null && mk.data.revokedAt === "";

  async function rotate() {
    try {
      await setupClient.rotateManagementKey({ serverId });
      toast.success(`Rotated the SSH management key for ${serverName}`);
      await queryClient.invalidateQueries({ queryKey: ["managementKey", serverId] });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not rotate the key");
    }
  }

  async function revoke() {
    try {
      await setupClient.revokeManagementKey({ serverId });
      toast.success(`Revoked SSH access for ${serverName}`);
      await queryClient.invalidateQueries({ queryKey: ["managementKey", serverId] });
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not revoke access");
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2 border-t border-border pt-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={onSetup}
            aria-label={`${hasKey ? "Re-run" : "Set up"} SSH setup for ${serverName}`}
          >
            <ServerCog className="h-4 w-4" aria-hidden="true" />
            {hasKey ? "Re-run setup" : "Set up over SSH"}
          </Button>
          <Button size="sm" variant="secondary" disabled={minting} onClick={onInstallCommand}>
            <TerminalSquare className="h-4 w-4" aria-hidden="true" />
            {minting ? "Generating..." : "Install command"}
          </Button>
        </div>
        <ConfirmDialog
          trigger={
            <Button size="icon" variant="ghost" aria-label={`Delete server ${serverName}`}>
              <Trash2 className="h-4 w-4" aria-hidden="true" />
            </Button>
          }
          title={`Delete server ${serverName}?`}
          description="This disconnects its agent and permanently removes the server, its agent registration, and its deployment history. Containers already running on the machine are not touched."
          recovery="You can connect the machine again at any time — create a new server and run a fresh install command on it."
          confirmLabel="Delete server"
          onConfirm={onDelete}
        />
      </div>

      {hasKey && (
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">SSH management key</span>
          <div className="flex items-center gap-1">
            <ConfirmDialog
              trigger={
                <Button size="sm" variant="ghost" aria-label={`Rotate SSH key for ${serverName}`}>
                  Rotate
                </Button>
              }
              title="Rotate the SSH management key?"
              description="Generates a new key, installs it on the server, and removes the old one — all in one step."
              recovery="A failed rotation leaves the working key in place, so access is never stranded."
              confirmLabel="Rotate key"
              onConfirm={() => void rotate()}
            />
            <ConfirmDialog
              trigger={
                <Button size="sm" variant="ghost" aria-label={`Revoke SSH access for ${serverName}`}>
                  Revoke
                </Button>
              }
              title="Revoke SSH access?"
              description="Removes Plorigo's management key from the server and marks the credential revoked. SSH management stops immediately; the agent and running containers are unaffected."
              recovery="Re-run managed setup to provision a fresh key if you need SSH management again."
              confirmLabel="Revoke access"
              onConfirm={() => void revoke()}
            />
          </div>
        </div>
      )}
    </div>
  );
}
