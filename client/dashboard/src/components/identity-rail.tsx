import { cn } from "@/lib/utils";
import * as React from "react";
import { Link } from "react-router";

export type IdentityRailItem = {
  key: string;
  title: string;
  href: string;
  active: boolean;
};

/**
 * The identity page's own navigation, in the page rather than in the app
 * sidebar: the person is the subject of the page, and the sections below are
 * theirs, not the product's.
 *
 * Items are routes, not anchors — each sub-page loads only its own queries and
 * is linkable on its own.
 */
export function IdentityRail({
  items,
  className,
}: {
  items: IdentityRailItem[];
  className?: string;
}): React.JSX.Element {
  return (
    <nav aria-label="This identity" className={cn("flex flex-col", className)}>
      <p className="text-eyebrow mb-2 hidden pl-3 lg:block">This identity</p>
      {items.map((item) => (
        <Link
          key={item.key}
          to={item.href}
          aria-current={item.active ? "page" : undefined}
          className={cn(
            // Every side is declared on every item, transparent where it is
            // not drawn: the open section gains three edges, and an item that
            // only grows them when selected moves the whole rail by a pixel
            // each time you change section.
            "shrink-0 border-b-2 px-3 py-1.5 text-sm whitespace-nowrap transition-colors",
            "lg:border-t lg:border-r lg:border-b lg:border-l-2",
            item.active
              ? // The open section lifts off the recessed ground as a card:
                // the heavy left edge says which one it is, the three light
                // edges close it. In dark mode --card is the ground itself,
                // so the lighter --accent step carries it, matching the table
                // row hover.
                cn(
                  "bg-card dark:bg-accent/70 text-foreground",
                  "border-b-foreground lg:border-l-foreground",
                  "lg:border-t-border lg:border-r-border lg:border-b-border",
                )
              : cn(
                  "text-muted-foreground hover:text-foreground",
                  // Hover darkens only the edge that is actually drawn at this
                  // breakpoint. Colouring it on every side lit up the bottom
                  // and right rules of the rail layout too, which read as half
                  // a box appearing around the row.
                  "border-b-border hover:border-b-foreground/40",
                  "lg:border-l-border lg:hover:border-l-foreground/40",
                  "lg:border-t-transparent lg:border-r-transparent lg:border-b-transparent",
                  "lg:hover:border-t-transparent lg:hover:border-r-transparent lg:hover:border-b-transparent",
                ),
          )}
        >
          {item.title}
        </Link>
      ))}
    </nav>
  );
}
