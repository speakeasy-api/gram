import { toast } from "sonner";

/** A success toast with an "Undo" action button. No prior call site in the dashboard
 * combined sonner's `action` option with an undo affordance — every existing toast is
 * a plain success/error message. */
export function showUndoToast(message: string, onUndo: () => void): void {
  toast.success(message, {
    action: {
      label: "Undo",
      onClick: onUndo,
    },
  });
}
