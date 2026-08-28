import { cn } from "@/lib/utils";
import * as React from "react";
import { NavLink } from "react-router";

export type IdentityRailItem = {
  key: string;
  title: string;
  href: string;
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
    <nav
      aria-label="This identity"
      className={cn("flex flex-col gap-1", className)}
    >
      <p className="text-eyebrow mb-2">This identity</p>
      {items.map((item) => (
        <NavLink
          key={item.key}
          to={item.href}
          end
          className={({ isActive }) =>
            cn(
              "border-l px-3 py-1.5 text-sm transition-colors",
              isActive
                ? "border-foreground text-foreground font-medium"
                : "border-border text-muted-foreground hover:text-foreground hover:border-foreground/40",
            )
          }
        >
          {item.title}
        </NavLink>
      ))}
    </nav>
  );
}
