import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ArrowUpRight } from "lucide-react";
import * as React from "react";
import { Link } from "react-router";

/**
 * A panel on an identity page. Every panel shows one subsystem's slice of the
 * person and names the page that owns that data, so the identity page reads as
 * a junction rather than as a second copy of the tool it links to.
 */
export function IdentityPanel({
  title,
  handoffLabel,
  handoffHref,
  footer,
  loading = false,
  loadingRows,
  loadingVariant,
  children,
  className,
  contentClassName,
}: {
  title: string;
  /** The page this panel continues on, e.g. "Audit Logs". */
  handoffLabel?: string;
  handoffHref?: string;
  /** One line saying what the slice is, e.g. "7 of 412 project findings". */
  footer?: React.ReactNode;
  /**
   * Whether this panel's own request is still in flight.
   *
   * Every panel reads a different endpoint, so they land at different times.
   * Without this a pending panel renders its empty state — "no findings in
   * this window" — which is a claim, not a wait, and it is the wrong one often
   * enough to matter. The footer goes with the body: it counts what the body
   * shows, and there is nothing to count yet.
   */
  loading?: boolean;
  loadingRows?: number;
  loadingVariant?: IdentityPanelSkeletonVariant;
  children: React.ReactNode;
  className?: string;
  /**
   * Padding for the body. Rows bring their own (they run full-bleed to the
   * panel edge so their separators do too); anything else — a chart, a chip
   * list — needs its own inset or it sits flush against the border.
   */
  contentClassName?: string;
}): React.JSX.Element {
  return (
    <section
      aria-busy={loading}
      className={cn("bg-card border-border flex flex-col border", className)}
    >
      <header className="border-border flex items-center justify-between gap-3 border-b px-4 py-3">
        <h3 className="text-sm font-medium">{title}</h3>
        {handoffLabel && handoffHref && (
          <Link
            to={handoffHref}
            className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 text-xs"
          >
            Open in {handoffLabel}
            <ArrowUpRight className="size-3" />
          </Link>
        )}
      </header>
      <div className={cn("flex-1", loading ? undefined : contentClassName)}>
        {loading ? (
          <IdentityPanelSkeleton rows={loadingRows} variant={loadingVariant} />
        ) : (
          children
        )}
      </div>
      {footer && !loading && (
        <footer className="border-border border-t px-4 py-2.5">
          <Text variant="small" muted className="text-xs">
            {footer}
          </Text>
        </footer>
      )}
    </section>
  );
}

/** Which shape a pending panel stands in for: a list of rows, or a chart. */
export type IdentityPanelSkeletonVariant = "rows" | "block";

/**
 * The placeholder a pending panel shows in place of its body, shaped like what
 * is coming: rows keep the separators and the trailing column so the panel
 * does not reflow when the data lands, and a chart panel gets one block rather
 * than rows it would never have drawn.
 */
export function IdentityPanelSkeleton({
  rows = 3,
  variant = "rows",
}: {
  rows?: number;
  variant?: IdentityPanelSkeletonVariant;
}): React.JSX.Element {
  if (variant === "block") {
    return (
      <div aria-hidden="true" className="flex flex-col gap-3 px-4 py-4">
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-2/3" />
      </div>
    );
  }

  return (
    <div aria-hidden="true">
      {Array.from({ length: rows }, (_, index) => (
        <div
          key={index}
          className="border-border flex items-center gap-3 border-b px-4 py-3 last:border-b-0"
        >
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <Skeleton className="h-3.5 w-1/2" />
            <Skeleton className="h-3 w-1/3" />
          </div>
          <Skeleton className="h-3 w-12 shrink-0" />
        </div>
      ))}
    </div>
  );
}

/** A row inside a panel: what happened, its detail, and a trailing value. */
export function IdentityPanelRow({
  title,
  detail,
  trailing,
  accent,
}: {
  title: React.ReactNode;
  detail?: React.ReactNode;
  trailing?: React.ReactNode;
  /** Draws the leading marker that flags a row as needing attention. */
  accent?: "destructive" | "warning";
}): React.JSX.Element {
  return (
    <div className="border-border flex items-center gap-3 border-b px-4 py-3 last:border-b-0">
      {accent && (
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            accent === "destructive" ? "bg-destructive" : "bg-warning",
          )}
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">{title}</div>
        {detail && (
          <div className="text-muted-foreground truncate text-xs">{detail}</div>
        )}
      </div>
      {trailing && (
        <div className="text-muted-foreground shrink-0 font-mono text-xs">
          {trailing}
        </div>
      )}
    </div>
  );
}

export function IdentityPanelEmpty({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div className="px-4 py-6">
      <Text variant="small" muted className="text-xs">
        {children}
      </Text>
    </div>
  );
}
