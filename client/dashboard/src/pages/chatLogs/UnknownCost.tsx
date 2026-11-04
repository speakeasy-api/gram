import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { UNKNOWN_COST_EXPLANATION } from "./claudeUsage";

/**
 * Stands in for a cost the agent never sent. A $0.00 there would read as a
 * free session; the dash says there is no figure, and the tooltip says why.
 */
export function UnknownCost({
  className,
}: {
  className?: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={UNKNOWN_COST_EXPLANATION}>
      <span
        tabIndex={0}
        aria-label={UNKNOWN_COST_EXPLANATION}
        className={cn("cursor-help tabular-nums", className)}
      >
        —
      </span>
    </SimpleTooltip>
  );
}
