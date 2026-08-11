import type { ReactNode } from "react";

// Shared section shell for the Platform Admin pages: a text-eyebrow overline,
// optional description, and a white hairline-bordered card for the controls.
export function AdminSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section>
      <h2 className="text-eyebrow">{title}</h2>
      {description && (
        <p className="text-muted-foreground mt-1 max-w-prose text-sm">
          {description}
        </p>
      )}
      <div className="border-border bg-card mt-3 border">{children}</div>
    </section>
  );
}

// A single control row inside an AdminSection card. Rows stack with hairline
// dividers via the parent's divide utilities; keep each row's interactive
// element (Switch, Button, Input) in `action`.
export function AdminRow({
  label,
  description,
  action,
  children,
}: {
  label: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  children?: ReactNode;
}): JSX.Element {
  return (
    <div className="px-4 py-3">
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          <div className="text-foreground text-sm font-medium">{label}</div>
          {description && (
            <div className="text-muted-foreground mt-0.5 text-sm">
              {description}
            </div>
          )}
        </div>
        {action && <div className="flex shrink-0 items-center">{action}</div>}
      </div>
      {children}
    </div>
  );
}
