import { MoreActions, type Action } from "@/components/ui/MoreActions";
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
 * Starts well clear of `leftOffsetPx` (the header row's own select-all
 * checkbox column) rather than the container's left edge, so that checkbox
 * stays fully uncovered and clickable — it already clears the selection
 * when all rows are checked, so the bar itself carries no separate "Clear"
 * control. Sized to its own content (not stretched to the table's full
 * width), a square hairline-bordered chip rather than a bar spanning the
 * whole row.
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
  leftOffsetPx,
  heightPx,
}: {
  selectedCount: number;
  /** One or two actions (e.g. "Mark as false positive" and "Setup exclusion
   * rule"), listed in the "Bulk actions" dropdown in the order given. */
  actions: BulkAction[];
  /** Width of the header row's own select-all checkbox column in pixels —
   * the bar starts immediately after it, leaving that checkbox uncovered
   * and clickable. */
  leftOffsetPx: number;
  /** The header row's own measured height in pixels (see
   * `useMeasuredHeight`) — the bar matches it exactly instead of sizing off
   * its own padding, so it reads as sitting *in* the header rather than
   * floating at some unrelated size. Falls back to the bar's own padding
   * when the header hasn't been measured yet (first paint). */
  heightPx?: number;
}): JSX.Element {
  const visible = selectedCount > 0;
  const containerRef = useRef<HTMLDivElement>(null);

  // A hidden-but-focused control must not silently vanish with focus still
  // inside it (e.g. the trigger loses visibility mid-async-action) — blur
  // so focus lands somewhere assistive tech can see.
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

  // Insets the bar a few px from the header row's own top/bottom edge — at
  // heightPx exactly (its prior sizing), the bar's shadow read as fused to
  // the table's own top border instead of floating above it.
  const verticalInset = 3;

  return (
    <div
      ref={containerRef}
      aria-hidden={!visible}
      style={{
        left: leftOffsetPx + 16,
        top: verticalInset,
        height: heightPx != null ? heightPx - verticalInset * 2 : undefined,
      }}
      className={cn(
        "bg-background absolute z-20 flex w-fit items-center gap-3 rounded-lg px-3 text-sm shadow-md",
        heightPx == null && "py-2",
        visible ? "visible" : "invisible",
      )}
    >
      <span className="font-medium whitespace-nowrap">
        {selectedCount} selected
      </span>
      <MoreActions
        triggerLabel="Bulk actions"
        triggerStyle={{ transitionProperty: "none" }}
        actions={actions.map((a): Action => ({
          label: a.label,
          onClick: a.onClick,
          disabled: a.disabled,
        }))}
      />
    </div>
  );
}
