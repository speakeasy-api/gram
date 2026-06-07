import { SidebarMenu, SidebarMenuItem } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

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
 */
export function SidebarNavSkeleton({ rows = 7 }: { rows?: number }) {
  return (
    <SidebarMenu
      aria-hidden
      className="gap-1 px-2 group-data-[collapsible=icon]:px-0"
    >
      {Array.from({ length: rows }).map((_, i) => (
        <SidebarMenuItem key={i}>
          <div className="flex items-center gap-2 rounded-lg px-2 py-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
            <Skeleton className="size-4 shrink-0 rounded" />
            <Skeleton
              className={cn(
                "h-3.5 group-data-[collapsible=icon]:hidden",
                LABEL_WIDTHS[i % LABEL_WIDTHS.length],
              )}
            />
          </div>
        </SidebarMenuItem>
      ))}
    </SidebarMenu>
  );
}
