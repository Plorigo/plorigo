import type { ReactNode } from "react";
import { CheckCircle2, GitBranch, Rocket, ShieldCheck } from "lucide-react";

import { plorigoLogoBlack } from "../lib/brand";
import { Badge } from "./ui";

// AuthShell is the shared layout for public auth screens. Auth behavior stays in
// the individual pages; this component only owns presentation.
export function AuthShell({
  title,
  description,
  children,
  footer,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <main className="grid min-h-screen bg-background lg:grid-cols-[minmax(0,1fr)_minmax(440px,0.78fr)]">
      <section className="hidden border-r border-border bg-card px-10 py-8 lg:flex lg:flex-col">
        <div className="flex items-center justify-between">
          <img src={plorigoLogoBlack} alt="Plorigo" className="h-8 w-auto dark:invert" />
          <Badge tone="blue">Launch with control</Badge>
        </div>

        <div className="flex flex-1 items-center">
          <div className="w-full max-w-xl">
            <h2 className="text-3xl font-semibold tracking-tight text-foreground">
              Own-server deployments, without the DevOps pain.
            </h2>
            <p className="mt-3 max-w-lg text-sm leading-6 text-muted-foreground">
              A compact control plane for projects, environments, servers, secrets,
              deploys, logs, and production safeguards — running on infrastructure you own.
            </p>

            <div className="mt-8 rounded-xl border border-border bg-background p-3 shadow-card">
              <div className="rounded-lg border border-border bg-card">
                <div className="flex items-center justify-between border-b border-border px-4 py-3">
                  <div className="flex items-center gap-2">
                    <span className="grid h-8 w-8 place-items-center rounded-lg border border-info/20 bg-info/10 text-info">
                      <Rocket className="h-4 w-4" aria-hidden="true" />
                    </span>
                    <div>
                      <p className="text-sm font-semibold text-foreground">Production deploy</p>
                      <p className="text-xs text-muted-foreground">main · rollback target kept</p>
                    </div>
                  </div>
                  <Badge tone="green">Ready</Badge>
                </div>
                <div className="grid gap-2 p-3">
                  {[
                    "Build detected",
                    "Health checks passed",
                    "Route switched to new release",
                  ].map((item) => (
                    <div
                      key={item}
                      className="flex items-center justify-between rounded-md border border-border bg-background px-3 py-2"
                    >
                      <span className="text-sm text-foreground">{item}</span>
                      <CheckCircle2 className="h-4 w-4 text-success" aria-hidden="true" />
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <ShieldCheck className="h-3.5 w-3.5" aria-hidden="true" />
          Every scary action has a recovery path — rollbacks, backups, and an audit trail.
          <GitBranch className="ml-auto h-3.5 w-3.5" aria-hidden="true" />
        </div>
      </section>

      <section className="flex min-h-screen items-center justify-center px-5 py-10">
        <div className="w-full max-w-[420px]">
          <div className="mb-8 lg:hidden">
            <img src={plorigoLogoBlack} alt="Plorigo" className="h-8 w-auto dark:invert" />
          </div>
          <div className="rounded-xl border border-border bg-card p-6 shadow-card sm:p-8">
            <h1 className="text-2xl font-semibold text-foreground">{title}</h1>
            {description && <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>}
            <div className="mt-6">{children}</div>
            {footer && <div className="mt-5 text-sm text-muted-foreground">{footer}</div>}
          </div>
        </div>
      </section>
    </main>
  );
}
