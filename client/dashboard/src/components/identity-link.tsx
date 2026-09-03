import type { IdentityRef } from "@/lib/identity-urn";
import { withIdentityWindow } from "@/lib/identity-urn";
import { useIdentityHrefBuilder } from "@/lib/useIdentityHref";
import { cn } from "@/lib/utils";
import { ArrowUpRight } from "lucide-react";
import * as React from "react";
import { Link, useLocation } from "react-router";

/**
 * The single-reference form of `useIdentityHrefBuilder`, carrying the window
 * the reader has open onto the destination.
 */
function useIdentityHref(
  identifier: IdentityRef | null | undefined,
): string | null {
  const href = useIdentityHrefBuilder()(identifier);
  const { search } = useLocation();
  return href ? withIdentityWindow(href, search) : null;
}

/**
 * The explicit way into a person's identity page, for surfaces where someone
 * is the subject rather than a cell in a list — a profile hero, a detail
 * header, a sheet. Says where it goes, so it does not depend on a reader
 * guessing that a name is clickable.
 */
export function ViewUserProfileLink({
  identifier,
  className,
  label = "View user profile",
}: {
  identifier: IdentityRef | null | undefined;
  className?: string;
  label?: string;
}): React.JSX.Element | null {
  const href = useIdentityHref(identifier);
  if (!href) return null;

  return (
    <Link
      to={href}
      onClick={(event) => event.stopPropagation()}
      className={cn(
        "text-muted-foreground hover:text-foreground hover:border-foreground/30 border-border inline-flex shrink-0 items-center gap-1 border px-2 py-1 text-xs whitespace-nowrap transition-colors",
        className,
      )}
    >
      {label}
      <ArrowUpRight className="size-3" />
    </Link>
  );
}

/**
 * Renders a person reference as a link to their identity page. Falls back to
 * plain text when the surface has no identifier to key on, so callers can use
 * it unconditionally instead of branching at every call site.
 */
export function IdentityLink({
  identifier,
  children,
  className,
  "aria-label": ariaLabel,
}: {
  identifier: IdentityRef | null | undefined;
  children: React.ReactNode;
  className?: string;
  /**
   * Accessible name, for a link whose visible text is a bare verb. A column of
   * rows each announcing "View" gives a screen reader no way to tell them
   * apart.
   */
  "aria-label"?: string;
}): React.JSX.Element {
  const href = useIdentityHref(identifier);

  if (!href) {
    return <span className={className}>{children}</span>;
  }

  return (
    <Link
      to={href}
      aria-label={ariaLabel}
      // Person references sit inside clickable rows on several surfaces; the
      // link must win over the row rather than firing both.
      onClick={(event) => event.stopPropagation()}
      // A rest-state affordance, not hover-only: these sit in table cells and
      // inside rows that are themselves clickable, where an invisible link is
      // undiscoverable. Solid rather than dotted — dotted already means
      // "definition/tooltip" elsewhere in this app.
      className={cn(
        "decoration-foreground/30 hover:decoration-foreground underline underline-offset-4",
        className,
      )}
    >
      {children}
    </Link>
  );
}
