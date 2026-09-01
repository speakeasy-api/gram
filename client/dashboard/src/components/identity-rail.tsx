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
            "shrink-0 border-b-2 px-3 py-1.5 text-sm whitespace-nowrap transition-colors lg:border-b-0 lg:border-l-2",
            item.active
              ? // The open section lifts off the recessed ground. In dark
                // mode --card is the ground itself, so the lighter --accent
                // step carries it, matching the table row hover.
                "bg-card dark:bg-accent/70 border-foreground text-foreground"
              : "border-border text-muted-foreground hover:text-foreground hover:border-foreground/40",
          )}
        >
          {item.title}
        </Link>
      ))}
    </nav>
  );
}
