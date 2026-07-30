import { Button } from "@speakeasy-api/moonshine";
import type { JSX } from "react";
import { cn } from "@/lib/utils";

/** Bar shown above a selectable list/table once at least one row is selected.
 * New for AIS-321 — no bulk-select affordance existed anywhere in the dashboard before
 * "mark false positive" needed one.
 *
 * Callers must always mount this (not `{selectedCount > 0 && <BulkActionBar .../>}`)
 * so its height stays reserved in the layout. It fades its own content in/out via
 * opacity instead of mounting/unmounting, so selecting or clearing a row doesn't
 * shift every row in the table below it. */
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
  const visible = selectedCount > 0;
  return (
    <div
      aria-hidden={!visible}
      className={cn(
        "bg-muted/60 flex items-center justify-between gap-3 border-b px-5 py-2 text-sm transition-opacity",
        visible ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
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
        <Button
          variant="primary"
          size="sm"
          onClick={onAction}
          tabIndex={visible ? 0 : -1}
        >
          {actionLabel}
        </Button>
      </div>
    </div>
  );
}
