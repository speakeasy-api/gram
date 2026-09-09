import { ChevronRight } from "lucide-react";
import React from "react";
import { Link } from "react-router";

import { cn } from "@/lib/utils";

/** One crumb in a trail. */
export interface BreadcrumbItem {
  /** Where the crumb links. Ignored when `isCurrentPage` or `disableLink`. */
  url: string;
  display: string;
  /** The page being viewed — rendered as plain text, not a link. */
  isCurrentPage?: boolean;
  /** A real page the reader may not open (no scope), or no page at all. */
  disableLink?: boolean;
  /** Label still loading: show a placeholder rather than flash a raw id. */
  pending?: boolean;
}

function Crumb({ item }: { item: BreadcrumbItem }): React.JSX.Element {
  if (item.pending) {
    return (
      <span
        aria-hidden="true"
        className="bg-muted inline-block h-3.5 w-20 animate-pulse align-middle"
      />
    );
  }
  if (item.isCurrentPage || item.disableLink) {
    return (
      <span
        className={item.isCurrentPage ? undefined : "text-muted-foreground"}
      >
        {item.display}
      </span>
    );
  }
  return (
    <Link
      to={item.url}
      className="text-muted-foreground hover:text-foreground trans underline-offset-4 hover:underline"
    >
      {item.display}
    </Link>
  );
}

export interface BreadcrumbProps {
  items: BreadcrumbItem[];
  className?: string;
  /** Rendered after the last crumb — e.g. a release-stage badge. */
  children?: React.ReactNode;
}

/**
 * A crumb trail: how the reader got to this page, and the way back up. The
 * caller owns which segments become crumbs; this only draws them.
 */
export function Breadcrumb({
  items,
  className,
  children,
}: BreadcrumbProps): React.JSX.Element {
  return (
    <nav
      aria-label="Breadcrumb"
      className={cn("flex items-center gap-2 normal-case", className)}
    >
      {items.map((item, index) => (
        <React.Fragment key={`${item.url}-${index}`}>
          <Crumb item={item} />
          {index < items.length - 1 && (
            <ChevronRight
              aria-hidden="true"
              className="text-muted-foreground/60 size-3.5 shrink-0"
            />
          )}
        </React.Fragment>
      ))}
      {children}
    </nav>
  );
}
