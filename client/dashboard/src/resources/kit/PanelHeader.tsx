import type { ReactNode } from "react";

/**
 * The one page header the kit draws: area eyebrow over a thin serif title,
 * one line of purpose, and the single primary action on the right.
 */
export function PanelHeader({
  area,
  title,
  description,
  action,
}: {
  area: string;
  title: string;
  description?: string;
  action?: ReactNode;
}): JSX.Element {
  return (
    <header className="flex flex-wrap items-end gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="text-eyebrow">{area}</div>
        <h1 className="text-display-sm font-thin break-words">{title}</h1>
        {description && (
          <p className="text-muted-foreground max-w-prose">{description}</p>
        )}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </header>
  );
}
