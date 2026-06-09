import type {
  ButtonHTMLAttributes,
  HTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
} from "react";

import { cn } from "../lib/cn";

export type DataStatus = "live" | "prototype" | "planned";

const statusLabels: Record<DataStatus, string> = {
  live: "Live API",
  prototype: "Prototype",
  planned: "Planned",
};

const statusClasses: Record<DataStatus, string> = {
  live: "border-emerald-200 bg-emerald-50 text-emerald-700",
  prototype: "border-violet-200 bg-violet-50 text-violet-700",
  planned: "border-zinc-200 bg-zinc-100 text-zinc-600",
};

export function DataBadge({ status }: { status: DataStatus }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[11px] font-medium leading-none",
        statusClasses[status],
      )}
    >
      {statusLabels[status]}
    </span>
  );
}

export function Badge({
  children,
  className,
  tone = "neutral",
}: {
  children: ReactNode;
  className?: string;
  tone?: "neutral" | "green" | "amber" | "red" | "blue" | "purple";
}) {
  const tones = {
    neutral: "border-zinc-200 bg-zinc-50 text-zinc-600",
    green: "border-emerald-200 bg-emerald-50 text-emerald-700",
    amber: "border-amber-200 bg-amber-50 text-amber-700",
    red: "border-red-200 bg-red-50 text-red-700",
    blue: "border-blue-200 bg-blue-50 text-blue-700",
    purple: "border-violet-200 bg-violet-50 text-violet-700",
  };

  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[11px] font-medium leading-none",
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Button({
  className,
  variant = "primary",
  size = "md",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md" | "icon";
}) {
  const variants = {
    primary:
      "border-zinc-950 bg-zinc-950 text-white shadow-sm hover:bg-zinc-800 disabled:border-zinc-300 disabled:bg-zinc-300",
    secondary:
      "border-zinc-200 bg-white text-zinc-900 shadow-sm hover:bg-zinc-50 disabled:text-zinc-400",
    ghost:
      "border-transparent bg-transparent text-zinc-600 hover:bg-zinc-100 hover:text-zinc-950 disabled:text-zinc-400",
    danger:
      "border-red-200 bg-red-50 text-red-700 shadow-sm hover:bg-red-100 disabled:text-red-300",
  };
  const sizes = {
    sm: "h-8 gap-1.5 px-2.5 text-xs",
    md: "h-9 gap-2 px-3 text-sm",
    icon: "h-9 w-9 justify-center p-0",
  };

  return (
    <button
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md border font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-80",
        sizes[size],
        variants[variant],
        className,
      )}
      {...props}
    />
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "h-10 w-full rounded-md border border-zinc-200 bg-white px-3 text-sm text-zinc-950 shadow-sm outline-none transition placeholder:text-zinc-400 focus:border-blue-500 focus:ring-2 focus:ring-blue-100 disabled:bg-zinc-50 disabled:text-zinc-400",
        className,
      )}
      {...props}
    />
  );
}

export function Select({
  className,
  children,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        "h-9 w-full rounded-md border border-zinc-200 bg-white px-3 text-sm text-zinc-950 shadow-sm outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100",
        className,
      )}
      {...props}
    >
      {children}
    </select>
  );
}

export function Panel({
  className,
  children,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <section
      className={cn(
        "rounded-xl border border-zinc-200 bg-white shadow-[0_1px_2px_rgba(24,24,27,0.04),0_10px_30px_rgba(24,24,27,0.03)]",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  );
}

export function PanelHeader({
  title,
  description,
  status,
  action,
}: {
  title: string;
  description?: string;
  status?: DataStatus;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 border-b border-zinc-100 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h2 className="truncate text-sm font-semibold text-zinc-950">{title}</h2>
          {status && <DataBadge status={status} />}
        </div>
        {description && <p className="mt-1 text-xs leading-5 text-zinc-500">{description}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

export function StatusDot({
  tone = "neutral",
  label,
}: {
  tone?: "green" | "amber" | "red" | "blue" | "purple" | "neutral";
  label?: string;
}) {
  const tones = {
    green: "bg-emerald-500",
    amber: "bg-amber-500",
    red: "bg-red-500",
    blue: "bg-blue-500",
    purple: "bg-violet-500",
    neutral: "bg-zinc-400",
  };

  return (
    <span className="inline-flex items-center gap-2">
      <span className={cn("h-2 w-2 rounded-full", tones[tone])} />
      {label && <span className="text-xs text-zinc-600">{label}</span>}
    </span>
  );
}

export function EmptyState({
  title,
  body,
  status,
}: {
  title: string;
  body: string;
  status?: DataStatus;
}) {
  return (
    <div className="rounded-md border border-dashed border-zinc-200 bg-zinc-50 px-4 py-6 text-center">
      <div className="flex items-center justify-center gap-2">
        <p className="text-sm font-medium text-zinc-900">{title}</p>
        {status && <DataBadge status={status} />}
      </div>
      <p className="mx-auto mt-1 max-w-md text-xs leading-5 text-zinc-500">{body}</p>
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-zinc-100", className)} />;
}

export function MetricCard({
  label,
  value,
  detail,
  status,
  trend,
  icon,
  accent = "blue",
}: {
  label: string;
  value: string;
  detail: string;
  status: DataStatus;
  trend?: ReactNode;
  icon?: ReactNode;
  accent?: "blue" | "green" | "amber" | "purple";
}) {
  const accents = {
    blue: "from-blue-500 to-sky-400",
    green: "from-emerald-500 to-teal-400",
    amber: "from-amber-500 to-orange-400",
    purple: "from-violet-500 to-indigo-400",
  };

  return (
    <Panel className="group relative overflow-hidden p-4 transition hover:-translate-y-0.5 hover:border-zinc-300 hover:shadow-[0_12px_34px_rgba(24,24,27,0.08)]">
      <div className={cn("absolute inset-x-0 top-0 h-0.5 bg-gradient-to-r", accents[accent])} />
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {icon && (
            <span className="grid h-7 w-7 shrink-0 place-items-center rounded-md border border-zinc-200 bg-zinc-50 text-zinc-600">
              {icon}
            </span>
          )}
          <p className="truncate text-xs font-medium text-zinc-500">{label}</p>
        </div>
        <DataBadge status={status} />
      </div>
      <div className="mt-3 flex items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-2xl font-semibold text-zinc-950">{value}</p>
          <p className="mt-1 text-xs leading-5 text-zinc-500">{detail}</p>
        </div>
        {trend && <div className="shrink-0">{trend}</div>}
      </div>
    </Panel>
  );
}

export function MiniBars({ values }: { values: number[] }) {
  return (
    <div className="flex h-10 items-end gap-1" aria-hidden="true">
      {values.map((value, index) => (
        <span
          key={`${value}-${index}`}
          className="w-1.5 rounded-t bg-blue-500/70"
          style={{ height: `${Math.max(8, value)}%` }}
        />
      ))}
    </div>
  );
}
