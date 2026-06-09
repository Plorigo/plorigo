import { KeyRound, Users } from "lucide-react";

import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, EmptyState, Panel, PanelHeader, Skeleton } from "@/components/ui";
import { errorMessage, formatDate } from "@/lib/format";
import { useApiTokens, useMembers } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";

export function TeamPage() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const members = useMembers(workspaceId);
  const apiTokens = useApiTokens();
  const error = errorMessage(members.error || apiTokens.error);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Team & access"
        description="Workspace members and API tokens. Roles control who can deploy and read secrets."
        actions={
          <Button size="sm" disabled>
            <Users className="h-4 w-4" aria-hidden="true" />
            Invite member
          </Button>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Panel>
          <PanelHeader title="Members" description="People with access to this workspace." />
          <div className="p-4">
            {members.isLoading && <Skeleton className="h-32 w-full" />}
            {error && <EmptyState title="Access API returned an error" body={error} />}
            {!members.isLoading && !error && (members.data?.length ?? 0) === 0 && (
              <EmptyState title="No members yet" body="Invite teammates to collaborate on this workspace." />
            )}
            {!members.isLoading && !error && (members.data?.length ?? 0) > 0 && (
              <div className="space-y-2">
                {members.data?.map((member) => (
                  <div
                    key={member.userId}
                    className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background px-3 py-2.5"
                  >
                    <div className="flex min-w-0 items-center gap-3">
                      <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">
                        {member.email.slice(0, 1).toUpperCase()}
                      </span>
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-foreground">{member.email}</p>
                        <p className="text-xs text-muted-foreground">Joined {formatDate(member.createdAt)}</p>
                      </div>
                    </div>
                    <Badge tone="neutral">{member.role}</Badge>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Panel>

        <Panel>
          <PanelHeader
            title="API tokens"
            description="Token metadata only — raw values are never returned."
            action={
              <Button size="sm" variant="secondary" disabled>
                <KeyRound className="h-4 w-4" aria-hidden="true" />
                New token
              </Button>
            }
          />
          <div className="space-y-2 p-4">
            {apiTokens.isLoading && <Skeleton className="h-24 w-full" />}
            {!apiTokens.isLoading && (apiTokens.data?.length ?? 0) === 0 && (
              <EmptyState title="No tokens yet" body="Create an API token to use the CLI or automate deployments." />
            )}
            {!apiTokens.isLoading &&
              apiTokens.data?.map((token) => (
                <div key={token.id} className="rounded-lg border border-border bg-background p-3">
                  <p className="truncate text-sm font-medium text-foreground">{token.name}</p>
                  <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
                    {token.tokenPrefix} · last used {token.lastUsedAt ? formatDate(token.lastUsedAt) : "never"}
                  </p>
                </div>
              ))}
          </div>
        </Panel>
      </div>
    </div>
  );
}
