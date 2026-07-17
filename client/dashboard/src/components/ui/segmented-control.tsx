import { Fragment, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { SimpleTooltip } from "@/components/ui/tooltip";

// The segment button's own styling, exported so a `trailing` control (e.g. an
// overflow dropdown trigger) can sit in the track without re-deriving it.
export const SEGMENT_BASE =
  "flex h-full items-center rounded-sm border px-3 text-sm font-medium transition-colors";
export const SEGMENT_ACTIVE = "border-border bg-card text-foreground shadow-sm";
export const SEGMENT_INACTIVE =
  "text-muted-foreground hover:text-foreground border-transparent";

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
  trailing,
}: {
  value: T;
  onChange: (value: T) => void;
  options: { value: T; label: ReactNode; tooltip?: string }[];
  disabled?: boolean;
  className?: string;
  // An extra control pinned inside the track after the options — for a set too
  // long to segment, where the tail lives behind an overflow menu. Style it with
  // SEGMENT_BASE + SEGMENT_INACTIVE so it reads as one of the segments.
  trailing?: ReactNode;
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
            aria-pressed={active}
            onClick={() => onChange(option.value)}
            className={cn(
              SEGMENT_BASE,
              active ? SEGMENT_ACTIVE : SEGMENT_INACTIVE,
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
      {trailing}
    </div>
  );
}
