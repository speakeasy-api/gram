import { Button } from "@/components/ui/button";

/**
 * Shared modal footer bar: red DELETE on the left, Cancel + Save on the right.
 * Save is wired via callback (consumer typically calls form.handleSubmit).
 * Delete shows a confirm() before firing.
 */
export function ModalActions({
  onCancel,
  onSave,
  onDelete,
  canSave = true,
  saving = false,
  deleting = false,
  saveLabel = "Save",
  deleteLabel = "Delete",
  confirmDelete = true,
}: {
  onCancel: () => void;
  onSave: () => void;
  onDelete: () => void;
  canSave?: boolean;
  saving?: boolean;
  deleting?: boolean;
  saveLabel?: string;
  deleteLabel?: string;
  /** Whether to surface a window.confirm before firing onDelete. */
  confirmDelete?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Button
        type="button"
        variant="destructive"
        onClick={() => {
          if (!confirmDelete || confirm(`${deleteLabel}? This is permanent.`)) {
            onDelete();
          }
        }}
        disabled={deleting}
      >
        {deleting ? "Deleting…" : deleteLabel}
      </Button>
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="button" onClick={onSave} disabled={!canSave || saving}>
          {saving ? "Saving…" : saveLabel}
        </Button>
      </div>
    </div>
  );
}
