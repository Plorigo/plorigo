import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowRight, GitFork, Globe, Lock, Search, Settings2 } from "lucide-react";
import { toast } from "sonner";

import { Button, Panel, PanelHeader, Skeleton } from "@/components/ui";
import { useGitHubConnection, useRepositories } from "@/lib/queries";
import { useWorkspaceStore } from "@/store";
import { NewProjectDialog } from "../NewProjectDialog";
import { SimpleDeployWizard, type DeploySelection } from "./SimpleDeployWizard";
import { TemplateGallery } from "./TemplateGallery";

// NewProjectLaunchpad is the Vercel-style "deploy from where?" entry page. It asks
// for a source — a connected GitHub repo, a public Git URL, or a curated template —
// then hands off to the simple wizard. An "advanced" card opens the granular flow.
export function NewProjectLaunchpad() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const queryClient = useQueryClient();
  const [selection, setSelection] = useState<DeploySelection | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  // Surface the GitHub OAuth redirect outcome (?github=connected|error) here too,
  // since the connect button lives on this page; mirror the Projects page handler.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const status = params.get("github");
    if (!status) return;
    if (status === "connected") {
      toast.success("GitHub connected");
      void queryClient.invalidateQueries({ queryKey: ["githubConnection"] });
    } else if (status === "error") {
      toast.error(params.get("reason") || "Could not connect GitHub");
    }
    params.delete("github");
    params.delete("reason");
    const qs = params.toString();
    window.history.replaceState({}, "", window.location.pathname + (qs ? `?${qs}` : ""));
  }, [queryClient]);

  if (selection) {
    return <SimpleDeployWizard workspaceId={workspaceId} selection={selection} onBack={() => setSelection(null)} />;
  }

  return (
    <div className="space-y-6">
      <Link
        to="/projects"
        className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to projects
      </Link>

      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">Let&apos;s deploy something</h1>
        <p className="mt-1.5 max-w-2xl text-sm leading-6 text-muted-foreground">
          Import a repository, paste a public Git URL, or start from a template — Plorigo sets up the project and runs it
          on a server you own.
        </p>
      </div>

      <PublicUrlBar onContinue={(repoUrl) => setSelection({ kind: "public", repoUrl })} />

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="Import Git Repository" description="Deploy from a repository in your connected GitHub account." />
          <div className="p-4">
            <ImportGitRepository
              workspaceId={workspaceId}
              onSelect={(repo) =>
                setSelection({
                  kind: "oauth",
                  owner: repo.owner,
                  repo: repo.name,
                  fullName: repo.fullName,
                  defaultBranch: repo.defaultBranch,
                  isPrivate: repo.isPrivate,
                })
              }
            />
          </div>
        </Panel>

        <Panel>
          <PanelHeader title="Clone Template" description="Start from a pre-configured stack." />
          <div className="p-4">
            <TemplateGallery onSelect={(template) => setSelection({ kind: "template", template })} />
          </div>
        </Panel>
      </div>

      <Panel className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-3">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-border bg-card text-muted-foreground">
            <Settings2 className="h-4 w-4" aria-hidden="true" />
          </span>
          <div>
            <p className="text-sm font-medium text-foreground">Configure manually</p>
            <p className="text-xs leading-5 text-muted-foreground">
              Create an empty project and set up environments, sources, and deploys step by step.
            </p>
          </div>
        </div>
        <Button variant="secondary" size="sm" disabled={!workspaceId} onClick={() => setAdvancedOpen(true)}>
          Advanced setup
        </Button>
      </Panel>

      <NewProjectDialog workspaceId={workspaceId} open={advancedOpen} onOpenChange={setAdvancedOpen} />
    </div>
  );
}

function PublicUrlBar({ onContinue }: { onContinue: (repoUrl: string) => void }) {
  const [url, setUrl] = useState("");
  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = url.trim();
    if (trimmed) onContinue(trimmed);
  }
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-2 sm:flex-row sm:items-center">
      <div className="flex flex-1 items-center gap-2 rounded-md border border-input bg-card px-3 shadow-sm">
        <Globe className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="Deploy a public Git repository — https://github.com/owner/repo"
          inputMode="url"
          autoCapitalize="none"
          spellCheck={false}
          className="h-10 min-w-0 flex-1 border-0 bg-transparent p-0 text-sm text-foreground outline-none placeholder:text-muted-foreground"
        />
      </div>
      <Button type="submit" variant="secondary" disabled={!url.trim()}>
        Continue
        <ArrowRight className="h-4 w-4" aria-hidden="true" />
      </Button>
    </form>
  );
}

interface RepoChoice {
  owner: string;
  name: string;
  fullName: string;
  defaultBranch: string;
  isPrivate: boolean;
}

function ImportGitRepository({
  workspaceId,
  onSelect,
}: {
  workspaceId: string;
  onSelect: (repo: RepoChoice) => void;
}) {
  const connection = useGitHubConnection(workspaceId);
  const configured = connection.data?.configured ?? false;
  const connected = connection.data?.connected ?? false;
  const login = connection.data?.connection?.githubLogin ?? "";
  const repos = useRepositories(workspaceId, connected);
  const [filter, setFilter] = useState("");

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const list = repos.data ?? [];
    if (!q) return list;
    return list.filter((r) => r.fullName.toLowerCase().includes(q));
  }, [repos.data, filter]);

  if (connection.isLoading) {
    return <p className="text-sm text-muted-foreground">Checking GitHub connection…</p>;
  }

  if (!configured) {
    return (
      <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
        GitHub isn&apos;t configured on this server. Paste a public repository URL above, or set{" "}
        <code>GITHUB_OAUTH_CLIENT_ID</code> and <code>GITHUB_OAUTH_CLIENT_SECRET</code> to import from a connected
        account.
      </div>
    );
  }

  if (!connected) {
    return (
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">
          Connect your GitHub account to pick a repository. You&apos;ll authorize on GitHub and return here.
        </p>
        <Button
          type="button"
          disabled={!workspaceId}
          onClick={() =>
            window.location.assign(
              `/api/github/connect?workspace_id=${encodeURIComponent(workspaceId)}&return_to=${encodeURIComponent("/projects/new")}`,
            )
          }
        >
          <GitFork className="h-4 w-4" aria-hidden="true" />
          Connect GitHub
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <label className="flex min-w-0 flex-1 items-center gap-2 rounded-md border border-input bg-card px-3 shadow-sm">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Search repositories…"
            className="h-9 min-w-0 flex-1 border-0 bg-transparent p-0 text-sm text-foreground outline-none placeholder:text-muted-foreground"
          />
        </label>
        {login && <span className="shrink-0 text-xs text-muted-foreground">{login}</span>}
      </div>

      {repos.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-11 w-full" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <p className="px-1 py-6 text-center text-sm text-muted-foreground">
          {repos.data?.length ? "No repositories match your search." : "No repositories found for this account."}
        </p>
      ) : (
        <ul className="max-h-80 divide-y divide-border overflow-y-auto rounded-md border border-border">
          {filtered.map((r) => (
            <li key={r.fullName} className="flex items-center justify-between gap-3 px-3 py-2">
              <span className="flex min-w-0 items-center gap-1.5 text-sm text-foreground">
                <span className="truncate font-medium">{r.fullName}</span>
                {r.isPrivate && <Lock className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden="true" />}
              </span>
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  onSelect({
                    owner: r.owner,
                    name: r.name,
                    fullName: r.fullName,
                    defaultBranch: r.defaultBranch,
                    isPrivate: r.isPrivate,
                  })
                }
              >
                Import
              </Button>
            </li>
          ))}
        </ul>
      )}

      {(repos.data?.length ?? 0) >= 100 && (
        <p className="text-xs text-muted-foreground">
          Showing the 100 most recently updated repositories. Use search to find others.
        </p>
      )}
    </div>
  );
}
