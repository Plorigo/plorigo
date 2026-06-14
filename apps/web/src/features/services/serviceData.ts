import type { Service } from "@/gen/controlplane/v1/services_pb";

// slugify mirrors the server's service-slug derivation closely enough for a live preview:
// lowercase, non-alphanumerics collapse to single hyphens, trimmed. It's a hint only — the
// backend stays authoritative for the stored slug.
export function slugify(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// sourceLabel is the compact one-line description of where a service deploys from: the
// repo's full name (git), the image ref (image), or the template id (template).
export function sourceLabel(service: Pick<Service, "sourceKind" | "fullName" | "repo" | "imageRef" | "templateId">): string {
  if (service.sourceKind === "git") return service.fullName || service.repo || "git repository";
  if (service.sourceKind === "template") return service.templateId || service.imageRef || "template";
  return service.imageRef || "image";
}

// A service's URL is its public route (public services) or its internal DNS alias
// (private services), reachable by sibling services at host:port.
export function internalUrl(service: Pick<Service, "internalHost" | "containerPort">): string {
  return `http://${service.internalHost}:${service.containerPort}`;
}

export function isPublic(service: Pick<Service, "visibility">): boolean {
  return service.visibility !== "private";
}
