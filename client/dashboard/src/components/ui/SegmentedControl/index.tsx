import { Fragment, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { SimpleTooltip } from "@/components/ui/Tooltip";

// The segment button's own styling. Base + inactive are exported so a
// `trailing` control (e.g. an overflow dropdown trigger) can sit in the track
// without re-deriving them; the active state stays internal, since only a real
// segment is ever active.
export const SEGMENT_BASE =
  "flex h-full items-center px-3 font-mono text-xs tracking-[0.08em] uppercase transition-colors";
export const SEGMENT_INACTIVE =
  "text-muted-foreground hover:text-foreground bg-transparent";
const SEGMENT_ACTIVE = "bg-primary text-primary-foreground";

/**
 * A two-or-more option segmented toggle: a bordered track of mono uppercase
 * segments with the active option filled as a solid ink block. Used for
 * in-place mode switches like
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
        "border-border bg-card divide-border inline-flex h-10 shrink-0 items-center divide-x border",
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

/**
 * ToggleButton — a single inline segment (billing chart granularity, usage-unit
 * toggle). Same mono/uppercase idiom as a SegmentedControl segment, with the
 * active option filling as a solid ink block. Lives here so the segment idiom
 * has one home; use SegmentedControl for a full option group.
 */
export function ToggleButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 font-mono text-xs tracking-[0.08em] uppercase transition-colors",
        active
          ? "bg-primary text-primary-foreground"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}
