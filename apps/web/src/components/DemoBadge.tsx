import { FlaskConical } from "lucide-react";

import { useDemoStore } from "../lib/demo";

// Shown only in dev builds while demo mode is on, so it's always obvious when the
// dashboard is displaying prototype fixtures instead of real data.
export function DemoBadge() {
  const demo = useDemoStore((s) => s.demo);
  if (!demo || !import.meta.env.DEV) {
    return null;
  }
  return (
    <span className="inline-flex h-7 items-center gap-1.5 rounded-md border border-warning/20 bg-warning/10 px-2.5 text-xs font-medium text-warning">
      <FlaskConical className="h-3.5 w-3.5" aria-hidden="true" />
      Demo data
    </span>
  );
}
