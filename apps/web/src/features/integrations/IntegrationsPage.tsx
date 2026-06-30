import { useEffect, useState } from "react";

import { ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import { GitBranch, Plug, Plus } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Skeleton } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { sourceClient } from "@/lib/clients";
import { errorMessage, formatDate } from "@/lib/format";
import { useConnections } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

// Providers that exist as a concept but aren't implemented yet — shown disabled so the page
// communicates the roadmap without faking capability.
const COMING_SOON = ["GitLab", "Bitbucket"];

// IntegrationsPage lists a workspace's connected integrations (multiple, across providers) and offers
// an "Add integration" dialog. Connecting is a redirect to the provider; the page just lists + adds +
// disconnects, keeping the surface scannable.
export function IntegrationsPage() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const queryClient = useQueryClient();
  const connections = useConnections(workspaceId);
  const [addOpen, setAddOpen] = useState(false);

  const conns = connections.data?.connections ?? [];
  const providers = connections.data?.providers ?? [];
  const github = providers.find((p) => p.provider === "github");

  // Surface a connect/register outcome on return, refresh the list, then strip the params.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const status = params.get("github");
    if (!status) return;
    const messages: Record<string, string> = {
      connected: "GitHub connected",
      app_connected: "GitHub App installed",
      app_registered: "GitHub App registered",
    };
    if (messages[status]) {
      toast.success(messages[status]);
      void queryClient.invalidateQueries({ queryKey: ["connections"] });
    } else if (status === "error") {
      toast.error(params.get("reason") || "Could not connect");
    }
    params.delete("github");
    params.delete("reason");
    const qs = params.toString();
    window.history.replaceState({}, "", window.location.pathname + (qs ? `?${qs}` : ""));
  }, [queryClient]);

  function go(path: string) {
    if (!workspaceId) return;
    const returnTo = encodeURIComponent("/integrations");
    window.location.assign(`${path}?workspace_id=${encodeURIComponent(workspaceId)}&return_to=${returnTo}`);
  }

  async function disconnect(connectionId: string) {
    try {
      await sourceClient.disconnectConnection({ connectionId });
      await queryClient.invalidateQueries({ queryKey: ["connections", workspaceId] });
      toast.success("Integration disconnected");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not disconnect");
    }
  }

  const error = errorMessage(connections.error);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Integrations"
        description="Connect Git providers to import repositories, deploy private repos, and get pull-request previews."
        actions={
          <Dialog open={addOpen} onOpenChange={setAddOpen}>
            <DialogTrigger asChild>
              <Button size="sm" disabled={!workspaceId}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add integration
              </Button>
            </DialogTrigger>
            <AddIntegrationDialog
              github={github}
              onAction={(path) => {
                setAddOpen(false);
                go(path);
              }}
              onRegister={(org) => {
                setAddOpen(false);
                const returnTo = encodeURIComponent("/integrations");
                const orgParam = org.trim() ? `&org=${encodeURIComponent(org.trim())}` : "";
                window.location.assign(
                  `/api/github/app/manifest/new?workspace_id=${encodeURIComponent(workspaceId)}${orgParam}&return_to=${returnTo}`,
                );
              }}
            />
          </Dialog>
        }
      />

      <Panel>
        <PanelHeader title="Connected" description="Integrations this workspace can deploy from." />
        <div className="p-4">
          {connections.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : error ? (
            <EmptyState title="Couldn't load integrations" body={error} />
          ) : conns.length === 0 ? (
            <EmptyState title="No integrations yet" body="Add a GitHub integration to import repositories and enable previews." />
          ) : (
            <div className="space-y-2">
              {conns.map((c) => (
                <div
                  key={c.id}
                  className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2.5"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-border bg-muted text-muted-foreground">
                      <GitBranch className="h-4 w-4" aria-hidden="true" />
                    </span>
                    <div className="min-w-0">
                      <p className="flex items-center gap-2 truncate text-sm font-medium text-foreground">
                        {c.accountLogin || c.provider}
                        <Badge tone={c.kind === "app" ? "blue" : "neutral"}>{c.kind === "app" ? "App" : "OAuth"}</Badge>
                      </p>
                      <p className="truncate text-xs text-muted-foreground">
                        {c.provider} · connected {formatDate(c.createdAt)}
                      </p>
                    </div>
                  </div>
                  <ConfirmDialog
                    trigger={
                      <Button size="sm" variant="ghost">
                        Disconnect
                      </Button>
                    }
                    title={`Disconnect ${c.accountLogin || c.provider}?`}
                    description="Services that build from this integration will stop deploying until reconnected."
                    recovery="You can reconnect it at any time from this page."
                    confirmLabel="Disconnect"
                    onConfirm={() => void disconnect(c.id)}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      </Panel>
    </div>
  );
}

// AddIntegrationDialog is the provider chooser: GitHub's connect methods (gated on server config),
// plus disabled "coming soon" rows for providers not yet implemented.
function AddIntegrationDialog({
  github,
  onAction,
  onRegister,
}: {
  github?: { oauthConfigured: boolean; appConfigured: boolean };
  onAction: (path: string) => void;
  onRegister: (org: string) => void;
}) {
  const [org, setOrg] = useState("");
  const appConfigured = github?.appConfigured ?? false;
  const oauthConfigured = github?.oauthConfigured ?? false;

  return (
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Add integration</DialogTitle>
        <DialogDescription>Connect a Git provider to import repositories and enable previews.</DialogDescription>
      </DialogHeader>

      <div className="space-y-4">
        <div className="rounded-lg border border-border p-3">
          <p className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
            <Plug className="h-4 w-4" aria-hidden="true" /> GitHub
          </p>
          {appConfigured || oauthConfigured ? (
            <div className="flex flex-wrap gap-2">
              {appConfigured && (
                <Button size="sm" onClick={() => onAction("/api/github/app/install")}>
                  Install GitHub App
                </Button>
              )}
              {oauthConfigured && (
                <Button size="sm" variant="secondary" onClick={() => onAction("/api/github/connect")}>
                  Connect account
                </Button>
              )}
            </div>
          ) : (
            // No GitHub App configured on the server — offer automated registration (owner-only).
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">
                No GitHub App configured. Register one automatically (owners only), or set the
                <code className="mx-1">GITHUB_*</code>env vars. See docs/architecture/sources.md.
              </p>
              <Input value={org} onChange={(e) => setOrg(e.target.value)} placeholder="GitHub org (optional)" className="max-w-xs" />
              <Button size="sm" onClick={() => onRegister(org)}>
                Register GitHub App
              </Button>
            </div>
          )}
        </div>

        {COMING_SOON.map((name) => (
          <div key={name} className="flex items-center justify-between rounded-lg border border-dashed border-border p-3 opacity-60">
            <span className="text-sm font-medium text-foreground">{name}</span>
            <Badge tone="neutral">Coming soon</Badge>
          </div>
        ))}
      </div>
    </DialogContent>
  );
}
