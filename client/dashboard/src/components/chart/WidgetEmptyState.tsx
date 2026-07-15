import { Type } from "@/components/ui/type";
import { Icon } from "@/components/ui/icon";
import { cn } from "@/lib/utils";

/**
 * Shared empty state for analytics widgets (charts, ranked lists, tables).
 * Centralized so every "no data" message on a page renders at the same
 * size/weight instead of each widget improvising its own `<Type>` variant.
 */
export function WidgetEmptyState({
  message,
  className,
}: {
  message: string;
  className?: string;
}): React.JSX.Element {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 py-10 text-center",
        className,
      )}
    >
      <div className="bg-muted flex size-10 items-center justify-center rounded-full">
        <Icon
          name="chart-no-axes-column"
          className="text-muted-foreground size-5"
        />
      </div>
      <Type muted small>
        {message}
      </Type>
    </div>
  );
}
