import { cn } from "@/components/ui/moonshine";
import { ReactNode } from "react";

export type ChartButtonProps = {
  onClick: () => void;
  children: ReactNode;
  ariaLabel: string;
  className?: string;
};

export function ChartButton({
  onClick,
  children,
  className,
  ariaLabel,
}: ChartButtonProps): ReactNode {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "text-muted-foreground hover:text-foreground hover:bg-accent/75 shrink-0 rounded-md p-1.5 flex transition-colors min-w-6 h-6 items-center justify-center text-xs gap-1",
        className,
      )}
      aria-label={ariaLabel}
    >
      {children}
    </button>
  );
}
