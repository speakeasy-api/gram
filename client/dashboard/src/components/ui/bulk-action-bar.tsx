import { Button } from "@speakeasy-api/moonshine";
import { MoreActions, type Action } from "@/components/ui/more-actions";
import { useEffect, useRef, type JSX } from "react";
import { cn } from "@/lib/utils";

export interface BulkAction {
  label: string;
  onClick: () => void;
  disabled?: boolean;
}

/** Compact "N selected · Bulk actions" toolbar shown above a selectable
 * list/table once at least one row is selected. New for AIS-321 — no
 * bulk-select affordance existed anywhere in the dashboard before "mark
 * false positive" needed one.
 *
 * Positioned absolutely over its own reserved-zero-height wrapper (the
 * caller must render this inside a `relative` container, directly above
 * the list/table) so toggling selection never adds or removes layout
 * height — it's not part of normal document flow at all, so there's no
 * space to reserve or collapse. `visible ? visible : invisible` (rather
 * than opacity/pointer-events) additionally drops the bar out of the tab
 * order for free while hidden, matching how a `hidden` element behaves.
 *
 * Both current callers (RiskEvents.tsx, RiskOverviewCategoryDetail.tsx)
 * already render this outside the selectable list's own scrolling
 * container, so it stays visible while the list itself scrolls with no
 * extra work — that's the property that actually matters when reviewing
 * several selected rows. `sticky top-0` is additionally set for the rarer
 * case of the whole page scrolling past it, but verify it actually takes
 * effect before relying on it in a new context: at least one of Gram's
 * shared page layouts wraps content in a Tailwind `@container` element,
 * and `container-type` implicitly applies CSS containment, which breaks
 * `position: sticky` for any descendant trying to stick relative to a
 * scrolling ancestor *outside* that boundary. */
export function BulkActionBar({
  selectedCount,
  actions,
  onClear,
  loading,
}: {
  selectedCount: number;
  /** One or two actions (e.g. "Mark as false positive" and "Setup exclusion
   * rule"), listed in the "Bulk actions" dropdown in the order given. */
  actions: BulkAction[];
  onClear: () => void;
  /** An action is running asynchronously (e.g. an AI suggestion) — shows a
   * spinner on the "Bulk actions" trigger itself, since the action that
   * triggered it is inside a menu that isn't necessarily open right now. */
  loading?: boolean;
}): JSX.Element {
  const visible = selectedCount > 0;
  const containerRef = useRef<HTMLDivElement>(null);

  // A hidden-but-focused control (e.g. Enter on "Clear" fires onClear
  // synchronously, dropping selectedCount to 0 on the very next render
  // while its own button still has DOM focus) must not silently vanish
  // with focus still inside it — blur so focus lands somewhere assistive
  // tech can see.
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
      ref={containerRef}
      aria-hidden={!visible}
      className={cn(
        "bg-background absolute inset-x-0 top-0 z-20 flex items-center gap-3 border-b px-5 py-2 text-sm shadow-sm",
        visible ? "visible" : "invisible",
      )}
    >
      <span className="font-medium">{selectedCount} selected</span>
      <Button variant="tertiary" size="sm" onClick={onClear}>
        Clear
      </Button>
      <MoreActions
        triggerLabel="Bulk actions"
        triggerLoading={loading}
        actions={actions.map(
          (a): Action => ({
            label: a.label,
            onClick: a.onClick,
            disabled: a.disabled,
          }),
        )}
      />
    </div>
  );
}
