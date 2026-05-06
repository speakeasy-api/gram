import { cn } from "@/lib/utils";
import { forwardRef, type ReactNode, type Ref } from "react";

export type CardGridColumns = "2xl" | "lg" | "auto";

const COLUMN_CLASSES: Record<CardGridColumns, string> = {
  "2xl": "grid grid-cols-1 gap-6 xl:grid-cols-2",
  lg: "grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3",
  auto: "grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4",
};

type CardGridProps = {
  children: ReactNode;
  columns?: CardGridColumns;
  className?: string;
};

/**
 * Standard responsive grid for resource cards on list pages
 * (MCP servers, plugins, environments, collections, etc).
 *
 * - "2xl" (default): 1 col, then 2 at xl breakpoint — for large feature cards
 * - "lg": 1 / 2 / 3 cols — for medium cards
 * - "auto": 1 / 2 / 3 / 4 cols — for compact cards
 */
export const CardGrid = forwardRef(function CardGrid(
  { children, columns = "2xl", className }: CardGridProps,
  ref: Ref<HTMLDivElement>,
) {
  return (
    <div ref={ref} className={cn(COLUMN_CLASSES[columns], className)}>
      {children}
    </div>
  );
});
