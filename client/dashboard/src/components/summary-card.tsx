import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

/**
 * SummaryCard — a titled panel with a built-in loading/empty/content state,
 * used on overview/dashboard pages to hold a chart, a mini-list, or a summary.
 *
 * Overview pages each re-defined a local `DashboardChartCard` (title bar +
 * loading spinner + empty branch + children). This is the shared version:
 * white `bg-card` sheet, hairline border, an eyebrow title with an optional
 * action, and a divider above the body.
 *
 *   <SummaryCard title="Top tools" action={<ViewAllLink/>} isLoading={q.isPending}
 *               isEmpty={rows.length === 0} empty="No tool activity yet">
 *     <ToolChart data={rows} />
 *   </SummaryCard>
 */
export function SummaryCard({
  title,
  action,
  isLoading = false,
  isEmpty = false,
  empty,
  children,
  bodyClassName,
  className,
}: {
  title: ReactNode;
  /** Right-aligned control in the header (a link, a select, a toggle). */
  action?: ReactNode;
  isLoading?: boolean;
  isEmpty?: boolean;
  /** Shown when `isEmpty` and not loading. String or a custom node. */
  empty?: ReactNode;
  children: ReactNode;
  bodyClassName?: string;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("bg-card flex flex-col border", className)}>
      <div className="flex items-center justify-between gap-2 px-5 py-3">
        <div className="text-eyebrow">{title}</div>
        {action != null && <div className="shrink-0">{action}</div>}
      </div>
      <div className={cn("border-t p-5", bodyClassName)}>
        <SummaryCardBody isLoading={isLoading} isEmpty={isEmpty} empty={empty}>
          {children}
        </SummaryCardBody>
      </div>
    </div>
  );
}

function SummaryCardBody({
  isLoading,
  isEmpty,
  empty,
  children,
}: {
  isLoading: boolean;
  isEmpty: boolean;
  empty?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  if (isLoading) {
    return <Skeleton className="h-40 w-full" />;
  }
  if (isEmpty) {
    if (typeof empty === "string" || empty == null) {
      return (
        <div className="flex h-40 items-center justify-center">
          <Text small muted>
            {empty ?? "Nothing to show yet"}
          </Text>
        </div>
      );
    }
    return <>{empty}</>;
  }
  return <>{children}</>;
}
