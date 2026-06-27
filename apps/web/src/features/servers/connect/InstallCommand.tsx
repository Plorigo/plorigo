import { toast } from "sonner";

import { Button } from "@/components/ui";

// copyInstallCommand copies the one-time install command and confirms via a toast. The
// command embeds a single-use token — that's intentional for the manual path (the user runs
// it themselves); it is never written to a setup-run log.
function copyInstallCommand(text: string) {
  void navigator.clipboard.writeText(text).then(
    () => toast.success("Install command copied"),
    () => toast.error("Could not copy to clipboard"),
  );
}

// InstallCommandBlock renders the one-time install command responsively: the command WRAPS
// (break-all) instead of stretching the dialog past the viewport on small screens, with the
// copy button as the primary affordance.
export function InstallCommandBlock({
  installCommand,
  expiresAt,
}: {
  installCommand: string;
  expiresAt: string;
}) {
  return (
    <div className="min-w-0 space-y-4">
      <pre className="min-w-0 max-w-full overflow-x-auto whitespace-pre-wrap break-all rounded-md border border-border bg-muted p-3 text-xs text-foreground">
        {installCommand}
      </pre>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="text-xs text-muted-foreground">
          One-time token; expires {new Date(expiresAt).toLocaleString()}.
        </span>
        <Button size="sm" variant="secondary" onClick={() => copyInstallCommand(installCommand)}>
          Copy command
        </Button>
      </div>
    </div>
  );
}
