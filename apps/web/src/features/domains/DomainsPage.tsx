import { type FormEvent, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { useNavigate } from "@tanstack/react-router";
import { Copy, Globe2, Info, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";

import { PageHeader } from "@/components/PageHeader";
import { StatCard } from "@/components/StatCard";
import { Badge, Button, EmptyState, Input, Panel, PanelHeader, Select, Skeleton, StatusDot } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Domain } from "@/gen/controlplane/v1/domains_pb";
import type { Project } from "@/gen/controlplane/v1/projects_pb";
import type { Service } from "@/gen/controlplane/v1/services_pb";
import { domainClient } from "@/lib/clients";
import { errorMessage } from "@/lib/format";
import { useEffectiveProjectId } from "@/lib/projectScope";
import {
  useDomainsByProject,
  useDomainsByWorkspace,
  useProjects,
  useServicesByProject,
  useServicesByWorkspace,
} from "@/lib/queries";
import { statusTone } from "@/lib/status";
import { useWorkspaceStore } from "@/store";

export function DomainsPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const projectId = useEffectiveProjectId();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [hostname, setHostname] = useState("");
  const [serviceId, setServiceId] = useState("");
  const [busy, setBusy] = useState(false);
  const [verifying, setVerifying] = useState("");
  const [instructionDomain, setInstructionDomain] = useState<Domain | null>(null);

  const projectDomains = useDomainsByProject(projectId);
  const workspaceDomains = useDomainsByWorkspace(projectId ? "" : workspaceId);
  const projectServices = useServicesByProject(projectId);
  const workspaceServices = useServicesByWorkspace(projectId ? "" : workspaceId);
  const projects = useProjects(workspaceId);

  const domains = projectId ? projectDomains : workspaceDomains;
  const services = projectId ? projectServices : workspaceServices;
  const rows = useMemo(() => domains.data ?? [], [domains.data]);
  const serviceRows = useMemo(() => services.data ?? [], [services.data]);
  const serviceById = useMemo(() => new Map(serviceRows.map((s) => [s.id, s])), [serviceRows]);
  const projectById = useMemo(() => new Map((projects.data ?? []).map((p) => [p.id, p])), [projects.data]);
  const scopeProject = projectId ? projectById.get(projectId) : undefined;

  const activeCount = rows.filter((d) => d.status === "active").length;
  const verifiedCount = rows.filter((d) => d.status === "verified").length;
  const needsAction = rows.filter((d) => !["active", "verified"].includes(d.status)).length;
  const selectedServiceId = serviceRows.some((s) => s.id === serviceId) ? serviceId : serviceRows[0]?.id ?? "";
  const loading = domains.isLoading || services.isLoading;
  const error = errorMessage(domains.error);

  async function invalidate() {
    await queryClient.invalidateQueries({ queryKey: ["domains"] });
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`Copied ${label}`);
    } catch {
      toast.error(`Could not copy ${label}`);
    }
  }

  async function addDomain(e: FormEvent) {
    e.preventDefault();
    const host = hostname.trim();
    if (!host || !selectedServiceId || busy) return;
    setBusy(true);
    try {
      const created = await domainClient.createDomain({ serviceId: selectedServiceId, hostname: host });
      await invalidate();
      setHostname("");
      setDialogOpen(false);
      if (created.domain) setInstructionDomain(created.domain);
      toast.success("Domain added");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not add the domain");
    } finally {
      setBusy(false);
    }
  }

  async function verifyDomain(domain: Domain) {
    setVerifying(domain.id);
    try {
      const verified = await domainClient.verifyDomain({ id: domain.id });
      await invalidate();
      if (verified.domain && instructionDomain?.id === domain.id) setInstructionDomain(verified.domain);
      toast.success("Domain checked");
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not verify the domain");
    } finally {
      setVerifying("");
    }
  }

  async function deleteDomain(domain: Domain) {
    try {
      await domainClient.deleteDomain({ id: domain.id });
      await invalidate();
      if (instructionDomain?.id === domain.id) setInstructionDomain(null);
      toast.success(`Removed ${domain.hostname}`);
    } catch (err) {
      toast.error(err instanceof ConnectError ? err.message : "Could not remove the domain");
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Domains"
        description={
          projectId
            ? `Custom domains attached to services in ${scopeProject?.name ?? "this project"}.`
            : "Custom domains across this workspace, grouped by the service they route to."
        }
        actions={
          <Button size="sm" disabled={!selectedServiceId} onClick={() => setDialogOpen(true)}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add domain
          </Button>
        }
      />

      <div className="grid gap-4 md:grid-cols-3">
        <StatCard label="Custom domains" value={String(rows.length)} detail={projectId ? "In this project" : "In this workspace"} icon={Globe2} intent="info" accentBar />
        <StatCard label="Connected" value={String(activeCount)} detail={`${verifiedCount} verified and waiting`} icon={Globe2} intent="success" accentBar />
        <StatCard label="Needs action" value={String(needsAction)} detail="DNS, deployment, or route sync" icon={RefreshCw} intent={needsAction > 0 ? "warning" : "success"} accentBar />
      </div>

      {loading && <Skeleton className="h-56 w-full" />}
      {!loading && error && <EmptyState title="Couldn't load domains" body={error} />}
      {!loading && !error && rows.length === 0 && (
        <EmptyState
          title="No custom domains"
          body={
            serviceRows.length > 0
              ? "Add a domain, choose the service it should route to, then follow the DNS setup instructions."
              : "Create a service first. Domains are attached to services so Plorigo can route traffic to the right container."
          }
        />
      )}
      {!loading && !error && rows.length > 0 && (
        <Panel>
          <PanelHeader
            title="Custom domains"
            description={projectId ? "Domains for services in the selected project." : "All custom domains in the selected workspace."}
          />
          <div className="overflow-x-auto">
            <Table className="min-w-[900px] table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[30%]">Domain</TableHead>
                  <TableHead className="w-[24%]">Service</TableHead>
                  <TableHead className="w-[132px]">Status</TableHead>
                  <TableHead className="w-[132px]">Last checked</TableHead>
                  <TableHead className="w-[220px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((domain) => (
                  <DomainRow
                    key={domain.id}
                    domain={domain}
                    service={serviceById.get(domain.serviceId)}
                    project={projectById.get(domain.projectId)}
                    showProject={!projectId}
                    verifying={verifying === domain.id}
                    onInstructions={setInstructionDomain}
                    onVerify={verifyDomain}
                    onDelete={deleteDomain}
                    onOpenService={(service) => {
                      void navigate({
                        to: "/projects/$projectId/services/$serviceId",
                        params: { projectId: service.projectId, serviceId: service.id },
                      });
                    }}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        </Panel>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add custom domain</DialogTitle>
            <DialogDescription>Choose the service this hostname should route to. DNS setup opens after the domain is added.</DialogDescription>
          </DialogHeader>
          <form onSubmit={addDomain} className="space-y-4">
            <div>
              <span className="mb-1.5 block text-xs font-medium text-foreground">Domain</span>
              <Input
                value={hostname}
                onChange={(e) => setHostname(e.target.value)}
                placeholder="app.example.com"
                autoCapitalize="none"
                autoComplete="off"
                spellCheck={false}
              />
            </div>
            <div>
              <span className="mb-1.5 block text-xs font-medium text-foreground">Service</span>
              <Select value={selectedServiceId} onChange={(e) => setServiceId(e.target.value)} disabled={serviceRows.length === 0}>
                {serviceRows.map((service) => (
                  <option key={service.id} value={service.id}>
                    {serviceLabel(service, projectById)}
                  </option>
                ))}
                {serviceRows.length === 0 && <option value="">No services</option>}
              </Select>
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy || !hostname.trim() || !selectedServiceId}>
                {busy ? "Adding..." : "Add domain"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <DNSInstructionsDialog
        domain={instructionDomain}
        service={instructionDomain ? serviceById.get(instructionDomain.serviceId) : undefined}
        onClose={() => setInstructionDomain(null)}
        onCopy={copy}
      />
    </div>
  );
}

function DomainRow({
  domain,
  service,
  project,
  showProject,
  verifying,
  onInstructions,
  onVerify,
  onDelete,
  onOpenService,
}: {
  domain: Domain;
  service?: Service;
  project?: Project;
  showProject: boolean;
  verifying: boolean;
  onInstructions: (domain: Domain) => void;
  onVerify: (domain: Domain) => void;
  onDelete: (domain: Domain) => void;
  onOpenService: (service: Service) => void;
}) {
  return (
    <TableRow>
      <TableCell className="whitespace-normal align-top">
        <p className="truncate font-mono text-sm font-medium text-foreground">{domain.hostname}</p>
      </TableCell>
      <TableCell className="whitespace-normal align-top">
        {service ? (
          <button type="button" className="max-w-full text-left" onClick={() => onOpenService(service)}>
            <span className="block truncate text-sm font-medium text-foreground hover:underline">{service.name}</span>
            <span className="block truncate text-xs text-muted-foreground">
              {showProject ? project?.name ?? "Project" : service.visibility === "public" ? "Public service" : "Private service"}
            </span>
          </button>
        ) : (
          <span className="text-xs text-muted-foreground">Service unavailable</span>
        )}
      </TableCell>
      <TableCell className="align-top">
        <StatusDot tone={domainTone(domain.status)} label={formatDomainStatus(domain.status)} />
      </TableCell>
      <TableCell className="align-top text-muted-foreground">
        {domain.lastCheckedAt ? timeAgo(domain.lastCheckedAt) : "-"}
      </TableCell>
      <TableCell className="align-top">
        <div className="flex justify-end gap-2">
          {domain.status !== "active" && (
            <Button size="sm" variant="secondary" onClick={() => onInstructions(domain)}>
              <Info className="h-4 w-4" aria-hidden="true" />
              Setup
            </Button>
          )}
          <Button size="sm" variant="secondary" disabled={verifying} onClick={() => onVerify(domain)}>
            <RefreshCw className="h-4 w-4" aria-hidden="true" />
            {verifying ? "Checking..." : "Verify"}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => onDelete(domain)} aria-label={`Remove ${domain.hostname}`}>
            <Trash2 className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function DNSInstructionsDialog({
  domain,
  service,
  onClose,
  onCopy,
}: {
  domain: Domain | null;
  service?: Service;
  onClose: () => void;
  onCopy: (text: string, label: string) => void;
}) {
  const records = domain ? dnsRecords(domain) : [];

  return (
    <Dialog open={Boolean(domain)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>DNS setup</DialogTitle>
          <DialogDescription>
            {domain ? `Point ${domain.hostname} to ${service?.name ?? "this service"}, then verify the domain.` : "Point the domain to this service."}
          </DialogDescription>
        </DialogHeader>
        {domain && (
          <div className="space-y-4">
            {domain.statusMessage && (
              <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm leading-6 text-muted-foreground">
                {domain.statusMessage}
              </div>
            )}

            {records.length > 0 ? (
              <div className="rounded-lg border border-border">
                <div className="grid grid-cols-[84px_minmax(0,1fr)_minmax(0,1.4fr)] gap-2 border-b border-border px-3 py-2 text-xs font-medium text-muted-foreground">
                  <span>Type</span>
                  <span>Name</span>
                  <span>Value</span>
                </div>
                <div className="divide-y divide-border">
                  {records.map((record) => (
                    <div key={`${record.type}-${record.value}`} className="grid grid-cols-[84px_minmax(0,1fr)_minmax(0,1.4fr)] gap-2 px-3 py-2 text-sm">
                      <span>
                        <Badge tone="blue">{record.type}</Badge>
                      </span>
                      <button
                        type="button"
                        onClick={() => onCopy(record.name, "DNS name")}
                        className="min-w-0 truncate text-left font-mono text-muted-foreground hover:underline"
                      >
                        {record.name}
                      </button>
                      <button
                        type="button"
                        onClick={() => onCopy(record.value, "DNS value")}
                        className="flex min-w-0 items-center gap-1 text-left font-mono text-foreground hover:underline"
                      >
                        <span className="min-w-0 truncate">{record.value}</span>
                        <Copy className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm leading-6 text-muted-foreground">
                Deploy this public service first so Plorigo can create the generated domain target.
              </div>
            )}

            <div className="flex justify-end">
              <Button type="button" variant="secondary" onClick={onClose}>
                Done
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function dnsRecords(domain: Domain): Array<{ type: string; name: string; value: string }> {
  if (!domain.dnsRecordType || !domain.dnsRecordValue) {
    return [];
  }
  const values = domain.dnsRecordValue
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  if (domain.dnsRecordType === "A") {
    return values.map((value) => ({
      type: value.includes(":") ? "AAAA" : "A",
      name: domain.dnsRecordName || domain.hostname,
      value,
    }));
  }
  return values.map((value) => ({
    type: domain.dnsRecordType,
    name: domain.dnsRecordName || domain.hostname,
    value,
  }));
}

function serviceLabel(service: Service, projects: Map<string, Project>): string {
  const project = projects.get(service.projectId)?.name;
  return project ? `${project} / ${service.name}` : service.name;
}

function domainTone(status: string) {
  if (status === "pending_dns" || status === "verified") return "blue";
  if (status === "blocked") return "amber";
  return statusTone(status);
}

function formatDomainStatus(status: string): string {
  return status.replaceAll("_", " ");
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
