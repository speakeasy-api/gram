import { Button } from "@speakeasy-api/moonshine";
import { useEffect, useRef, type JSX, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export interface BulkAction {
  label: string;
  onClick: () => void;
  variant?: "primary" | "secondary" | "tertiary";
  /** e.g. a spinner while an async suggestion is in flight. */
  leftIcon?: ReactNode;
  disabled?: boolean;
}

/** Bar shown above a selectable list/table once at least one row is selected.
 * New for AIS-321 — no bulk-select affordance existed anywhere in the dashboard before
 * "mark false positive" needed one.
 *
 * Callers must always mount this (not `{selectedCount > 0 && <BulkActionBar .../>}`)
 * — an actual mount/unmount snaps the table below it up and down instantly. Instead
 * this collapses its own height to zero via the CSS grid `0fr -> 1fr` technique
 * (the outer grid row is what animates; the inner `overflow-hidden` wrapper clips
 * the content during the transition), so toggling selection animates smoothly
 * instead of both reserving dead space when idle AND snapping when active.
 *
 * Both current callers (RiskEvents.tsx, RiskOverviewCategoryDetail.tsx) already
 * render this outside the selectable list's own scrolling container, so it stays
 * visible while the list itself scrolls with no extra work — that's the property
 * that actually matters when reviewing several selected rows. `sticky top-0` is
 * additionally set for the rarer case of the whole page scrolling past it, but
 * verify it actually takes effect before relying on it in a new context: at least
 * one of Gram's shared page layouts wraps content in a Tailwind `@container`
 * element, and `container-type` implicitly applies CSS containment, which breaks
 * `position: sticky` for any descendant trying to stick relative to a scrolling
 * ancestor *outside* that boundary. */
export function BulkActionBar({
  selectedCount,
  actions,
  onClear,
}: {
  selectedCount: number;
  /** One or two actions (e.g. "Mark as false positive" and "Setup exclusion
   * rule"), rendered side by side in the order given. */
  actions: BulkAction[];
  onClear: () => void;
}): JSX.Element {
  const visible = selectedCount > 0;
  const containerRef = useRef<HTMLDivElement>(null);

  // aria-hidden must never contain the focused element — e.g. pressing Enter
  // on "Clear" fires onClear synchronously, dropping selectedCount to 0 and
  // making this div aria-hidden on the very next render while its own button
  // still has DOM focus. Blur it so focus lands somewhere assistive tech can
  // see instead of vanishing into a hidden subtree.
  useEffect(() => {
    if (visible) return;
    const active = document.activeElement;
    if (
      active instanceof HTMLElement &&
      containerRef.current?.contains(active)
    ) {
      active.blur();
    }
  }, [visible]);

  return (
    <div
      className={cn(
        "sticky top-0 z-10 grid transition-[grid-template-rows] duration-200 ease-out",
        visible ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
      )}
    >
      <div
        ref={containerRef}
        aria-hidden={!visible}
        className="min-h-0 overflow-hidden"
      >
        <div className="bg-muted/60 flex items-center justify-between gap-3 border-b px-5 py-2 text-sm">
          <span className="font-medium">{selectedCount} selected</span>
          <div className="flex items-center gap-2">
            <Button
              variant="tertiary"
              size="sm"
              onClick={onClear}
              tabIndex={visible ? 0 : -1}
            >
              Clear
            </Button>
            {actions.map((action) => (
              <Button
                key={action.label}
                variant={action.variant ?? "primary"}
                size="sm"
                onClick={action.onClick}
                disabled={action.disabled}
                tabIndex={visible ? 0 : -1}
              >
                {action.leftIcon && (
                  <Button.LeftIcon>{action.leftIcon}</Button.LeftIcon>
                )}
                <Button.Text>{action.label}</Button.Text>
              </Button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
