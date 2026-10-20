import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";

interface DeleteConfigurationDialogProps {
  open: boolean;
  kind: "signal" | "sensor";
  name: string;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

export function DeleteConfigurationDialog({
  open,
  kind,
  name,
  pending,
  onOpenChange,
  onConfirm,
}: DeleteConfigurationDialogProps): JSX.Element {
  const isSignal = kind === "signal";
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!pending) onOpenChange(next);
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete {kind}</Dialog.Title>
          <Dialog.Description>
            Delete &ldquo;{name}&rdquo;? This action cannot be undone.
          </Dialog.Description>
        </Dialog.Header>
        <Alert variant="warning" alignTop>
          {isSignal
            ? "This shared signal will be detached from every sensor that uses it. Surviving sensor order will be compacted, and those sensors may become empty or incomplete drafts."
            : "Deleting this sensor removes its ordered memberships. Shared signals remain available to other sensors."}
        </Alert>
        <Dialog.Footer>
          <Button
            type="button"
            variant="secondary"
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive-primary"
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? "Deleting…" : `Delete ${kind}`}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
