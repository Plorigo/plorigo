import type { ReactNode } from "react";
import { ArrowUpRight, CheckCircle2, ShieldCheck } from "lucide-react";

import { plorigoLogoBlack } from "../lib/brand";
import { Badge, DataBadge } from "./ui";

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
    <main className="grid min-h-screen bg-zinc-50 lg:grid-cols-[minmax(0,1fr)_minmax(440px,0.78fr)]">
      <section className="hidden border-r border-zinc-200 bg-white px-10 py-8 lg:flex lg:flex-col">
        <div className="flex items-center justify-between">
          <img src={plorigoLogoBlack} alt="Plorigo" className="h-8 w-auto" />
          <Badge tone="blue">Launch with control</Badge>
        </div>

        <div className="flex flex-1 items-center">
          <div className="w-full max-w-xl">
            <div className="mb-5 flex items-center gap-2 text-sm font-medium text-zinc-500">
              <ShieldCheck className="h-4 w-4 text-blue-600" aria-hidden="true" />
              Own-server deployments with visible recovery paths
            </div>
            <div className="rounded-lg border border-zinc-200 bg-zinc-50 p-3 shadow-sm">
              <div className="rounded-md border border-zinc-200 bg-white">
                <div className="flex items-center justify-between border-b border-zinc-100 px-4 py-3">
                  <div>
                    <p className="text-sm font-semibold text-zinc-950">storefront</p>
                    <p className="text-xs text-zinc-500">Production deployment</p>
                  </div>
                  <DataBadge status="prototype" />
                </div>
                <div className="grid gap-2 p-3">
                  {["Build detected", "Health checks passed", "Rollback target kept"].map(
                    (item) => (
                      <div
                        key={item}
                        className="flex items-center justify-between rounded-md border border-zinc-100 bg-zinc-50 px-3 py-2"
                      >
                        <span className="text-sm text-zinc-700">{item}</span>
                        <CheckCircle2 className="h-4 w-4 text-emerald-600" aria-hidden="true" />
                      </div>
                    ),
                  )}
                </div>
              </div>
            </div>
            <p className="mt-5 max-w-lg text-sm leading-6 text-zinc-500">
              A compact control plane for projects, environments, servers, secrets,
              deploys, logs, and production safeguards.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 text-xs text-zinc-500">
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
          Dashboard preview labels distinguish live backend from prototype UI.
        </div>
      </section>

      <section className="flex min-h-screen items-center justify-center px-5 py-10">
        <div className="w-full max-w-[420px]">
          <div className="mb-8 lg:hidden">
            <img src={plorigoLogoBlack} alt="Plorigo" className="h-8 w-auto" />
          </div>
          <div className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm sm:p-8">
            <h1 className="text-2xl font-semibold text-zinc-950">{title}</h1>
            {description && <p className="mt-2 text-sm leading-6 text-zinc-500">{description}</p>}
            <div className="mt-6">{children}</div>
            {footer && <div className="mt-5 text-sm text-zinc-500">{footer}</div>}
          </div>
        </div>
      </section>
    </main>
  );
}
