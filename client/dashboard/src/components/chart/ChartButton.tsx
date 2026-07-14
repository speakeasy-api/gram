import { cn } from "@/lib/utils";
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
        "text-muted-foreground hover:text-foreground hover:bg-accent/75 flex h-6 min-w-6 shrink-0 items-center justify-center gap-1 p-1.5 text-xs transition-colors",
        className,
      )}
      aria-label={ariaLabel}
    >
      {children}
    </button>
  );
}
