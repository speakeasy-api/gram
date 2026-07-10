import * as React from "react";

import { cn } from "@/lib/utils";

/** @public — part of the component's prop API. */
export type DetailListOrientation = "stacked" | "inline";

const DetailListOrientationContext =
  React.createContext<DetailListOrientation>("stacked");

export interface DetailListProps extends React.ComponentProps<"dl"> {
  /**
   * "stacked": mono eyebrow label above a sans value, tiled in a grid.
   * "inline": label left (muted sans), value right-aligned, one row per item.
   */
  orientation?: DetailListOrientation;
}

function DetailListRoot({
  orientation = "stacked",
  className,
  children,
  ...props
}: DetailListProps): React.JSX.Element {
  return (
    <DetailListOrientationContext.Provider value={orientation}>
      <dl
        className={cn(
          "grid",
          orientation === "stacked" && "grid-cols-2 gap-x-8 gap-y-5",
          orientation === "inline" &&
            "grid-cols-[max-content_1fr] items-baseline gap-x-6 gap-y-2",
          className,
        )}
        {...props}
      >
        {children}
      </dl>
    </DetailListOrientationContext.Provider>
  );
}

/** @public — part of the component's prop API. */
export interface DetailListItemProps extends React.ComponentProps<"div"> {
  label: React.ReactNode;
  value: React.ReactNode;
}

/**
 * A single label/value pair. Renders as a stacked block (its own grid cell)
 * in "stacked" mode, or as a `dt`/`dd` row (via `display: contents`, so the
 * two cells join the parent `dl`'s grid columns) in "inline" mode — either
 * way alignment comes from the parent grid, not manual spacing.
 */
function DetailListItem({
  label,
  value,
  className,
  ...props
}: DetailListItemProps): React.JSX.Element {
  const orientation = React.useContext(DetailListOrientationContext);

  if (orientation === "inline") {
    return (
      <div className={cn("contents", className)} {...props}>
        <dt className="text-muted-foreground font-sans text-sm">{label}</dt>
        <dd className="text-right font-sans text-sm">{value}</dd>
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-1", className)} {...props}>
      <dt className="font-mono text-xs uppercase tracking-[0.08em] text-muted">
        {label}
      </dt>
      <dd className="font-sans text-sm">{value}</dd>
    </div>
  );
}

DetailListRoot.Item = DetailListItem;

export { DetailListRoot as DetailList };
