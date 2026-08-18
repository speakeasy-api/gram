import { SidebarMenu, SidebarMenuItem } from "@/components/ui/Sidebar";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { Fragment } from "react";

// Deterministic label widths so the placeholder reads like a real nav list
// without shifting between renders.
const LABEL_WIDTHS = [
  "w-3/5",
  "w-4/5",
  "w-1/2",
  "w-2/3",
  "w-3/4",
  "w-1/2",
  "w-3/5",
];

/**
 * Placeholder shown in the sidebar while RBAC grants are loading (e.g. right
 * after switching projects, when the query cache is cleared). Keeps the nav's
 * shape so it doesn't collapse/flash to empty before the gated items resolve.
 *
 * Row geometry mirrors NavButton/CollapsibleNavGroup exactly — h-8 (py-1.5
 * around a text-sm line box) with a size-4 icon at px-2 — so swapping the
 * skeleton for the real nav doesn't shift anything. Callers pass their own
 * menu spacing and divider position to match the list they stand in for.
 */
export function SidebarNavSkeleton({
  rows = 7,
  divideAfter,
  className,
}: {
  rows?: number;
  /** Render the nav's group divider after this many rows. */
  divideAfter?: number;
  /** Menu spacing, matched to the sidebar this stands in for. */
  className?: string;
}): JSX.Element {
  return (
    <SidebarMenu
      aria-hidden
      className={cn("gap-1 px-2 group-data-[collapsible=icon]:px-0", className)}
    >
      {Array.from({ length: rows }).map((_, i) => (
        <Fragment key={i}>
          <SidebarMenuItem>
            <div className="flex h-8 items-center gap-2 px-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
              <Skeleton className="size-4 shrink-0" />
              <Skeleton
                className={cn(
                  "h-3.5 group-data-[collapsible=icon]:hidden",
                  LABEL_WIDTHS[i % LABEL_WIDTHS.length],
                )}
              />
            </div>
          </SidebarMenuItem>
          {divideAfter === i + 1 && (
            <li aria-hidden="true" className="my-2 px-1">
              <div className="border-border border-t" />
            </li>
          )}
        </Fragment>
      ))}
    </SidebarMenu>
  );
}
