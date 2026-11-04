import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { UNKNOWN_COST_EXPLANATION } from "./claudeUsage";

/**
 * Stands in for a cost the agent never sent. A $0.00 there would read as a
 * free session; the dash says there is no figure, and the tooltip says why.
 */
export function UnknownCost({
  className,
  focusable = true,
}: {
  className?: string;
  /** False inside a control that already takes focus, such as a row button. */
  focusable?: boolean;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={UNKNOWN_COST_EXPLANATION}>
      <span
        tabIndex={focusable ? 0 : undefined}
        aria-label={UNKNOWN_COST_EXPLANATION}
        className={cn("cursor-help tabular-nums", className)}
      >
        —
      </span>
    </SimpleTooltip>
  );
}
