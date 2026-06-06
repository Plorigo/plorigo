import type { ReactNode } from "react";

// AuthShell is the centered card used by the login / register / reset screens.
export function AuthShell({
  title,
  children,
  footer,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <main className="mx-auto mt-16 max-w-sm p-8">
      <h1 className="text-2xl font-semibold">{title}</h1>
      <div className="mt-6">{children}</div>
      {footer && <div className="mt-4 text-sm text-gray-500">{footer}</div>}
    </main>
  );
}
