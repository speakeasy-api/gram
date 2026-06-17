import { Fragment, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { SimpleTooltip } from "@/components/ui/tooltip";

/**
 * A two-or-more option segmented toggle (iOS-style): a bordered white track with
 * the active option raised as a white card. Used for in-place mode switches like
 * Tokens/Cost or Employees/Unknown-users, and sized (h-10) to line up inside the
 * page Toolbar. One shared component so every segmented toggle looks identical.
 */
export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  disabled,
  className,
}: {
  value: T;
  onChange: (value: T) => void;
  options: { value: T; label: ReactNode; tooltip?: string }[];
  disabled?: boolean;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "border-border bg-card inline-flex h-10 shrink-0 items-center gap-0.5 rounded-md border p-0.5",
        disabled && "cursor-not-allowed opacity-50",
        className,
      )}
    >
      {options.map((option) => {
        const active = option.value === value;
        const button = (
          <button
            type="button"
            disabled={disabled}
            onClick={() => onChange(option.value)}
            className={cn(
              "flex h-full items-center rounded-sm border px-3 text-sm font-medium transition-colors",
              active
                ? "border-border bg-card text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground border-transparent",
              disabled && "pointer-events-none",
            )}
          >
            {option.label}
          </button>
        );
        return option.tooltip ? (
          <SimpleTooltip key={option.value} tooltip={option.tooltip}>
            {button}
          </SimpleTooltip>
        ) : (
          <Fragment key={option.value}>{button}</Fragment>
        );
      })}
    </div>
  );
}
