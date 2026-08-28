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
 * sidebar: the person is the subject of the page, but they are not a place in
 * the product, so the surrounding project nav stays where the reader left it.
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
      <p className="text-eyebrow mb-2 pl-3">This identity</p>
      {items.map((item) => (
        <Link
          key={item.key}
          to={item.href}
          aria-current={item.active ? "page" : undefined}
          className={cn(
            "border-l-2 px-3 py-1.5 text-sm transition-colors",
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
