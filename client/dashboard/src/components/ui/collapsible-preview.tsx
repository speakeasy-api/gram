// oxlint-disable react/only-export-components -- the toggle ships with the hook that drives it
import { cn } from "@/lib/utils";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";

/**
 * Hiding the tail of a list: the state, the slice, and what counts as
 * collapsible. Every list that hides one shares this, so the rule that a list
 * at exactly the preview count shows no toggle is decided once.
 */
export function useCollapsedPreview<T>(
  items: T[],
  previewCount: number,
): {
  collapsible: boolean;
  expanded: boolean;
  toggle: () => void;
  visible: T[];
} {
  const [expanded, setExpanded] = useState(false);
  const collapsible = items.length > previewCount;

  return {
    collapsible,
    expanded,
    toggle: () => setExpanded((current) => !current),
    visible: collapsible && !expanded ? items.slice(0, previewCount) : items,
  };
}

/**
 * The control that reveals a hidden tail. The chrome around it differs — a
 * chip in a wrap of scopes, a footer row under a framed list, a row beneath a
 * table — but the accessible contract and the collapse wording are the same
 * wherever it appears, so they live here rather than in each caller.
 */
export function MoreToggle({
  expanded,
  onToggle,
  collapsedLabel,
  controlId,
  className,
}: {
  expanded: boolean;
  onToggle: () => void;
  /** What the control says while the tail is hidden. */
  collapsedLabel: string;
  /**
   * The id of the region this toggle expands, for aria-controls — without it
   * a screen reader hears "expanded" with nothing saying expanded *what*.
   */
  controlId?: string;
  className?: string;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-expanded={expanded}
      aria-controls={controlId}
      onClick={onToggle}
      className={cn(
        "text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs",
        className,
      )}
    >
      {expanded ? "Show fewer" : collapsedLabel}
      {expanded ? (
        <ChevronUp className="size-3" />
      ) : (
        <ChevronDown className="size-3" />
      )}
    </button>
  );
}
