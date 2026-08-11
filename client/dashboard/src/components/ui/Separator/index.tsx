import { cn } from "@/lib/utils";
import { Orientation } from "@/components/ui/lib/types";

export interface SeparatorProps {
  orientation?: Orientation;
  className?: string;
}

export function Separator({
  orientation = "horizontal",
  className,
}: SeparatorProps): React.JSX.Element {
  return (
    <div
      className={cn(
        orientation === "horizontal"
          ? "h-[1px] w-full bg-border"
          : "h-full w-[1px] bg-border",
        className,
      )}
    />
  );
}
