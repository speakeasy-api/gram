import { Button } from "@speakeasy-api/moonshine";
import type { JSX } from "react";

/** Sticky bar shown above a selectable list/table once at least one row is selected.
 * New for AIS-321 — no bulk-select affordance existed anywhere in the dashboard before
 * "mark false positive" needed one. */
export function BulkActionBar({
  selectedCount,
  actionLabel,
  onAction,
  onClear,
}: {
  selectedCount: number;
  actionLabel: string;
  onAction: () => void;
  onClear: () => void;
}): JSX.Element {
  return (
    <div className="bg-muted/60 flex items-center justify-between gap-3 border-b px-5 py-2 text-sm">
      <span className="font-medium">{selectedCount} selected</span>
      <div className="flex items-center gap-2">
        <Button variant="tertiary" size="sm" onClick={onClear}>
          Clear
        </Button>
        <Button variant="primary" size="sm" onClick={onAction}>
          {actionLabel}
        </Button>
      </div>
    </div>
  );
}
