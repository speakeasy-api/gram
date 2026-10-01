import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { useEffect, useState } from "react";
import { TypeToConfirmField } from "./TypeToConfirmField";
import { removeConfirmation } from "./removeConfirmation";

export function RemoveSubjectDialog({
  admission,
  onOpenChange,
  onConfirm,
  isPending,
}: {
  /** The machine being removed; the dialog is open while this is set. */
  admission: WorkloadAdmission | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: (admission: WorkloadAdmission) => void;
  isPending: boolean;
}): JSX.Element {
  const [typed, setTyped] = useState("");

  // A fresh confirmation for every machine, so text typed for one never
  // unlocks the removal of the next.
  useEffect(() => {
    setTyped("");
  }, [admission?.id]);

  const expected = admission ? removeConfirmation(admission) : "";
  // Exact first: the server stores a subject as supplied, so one admitted
  // through the API can carry outer whitespace and must still be confirmable.
  // The trimmed comparison forgives a stray space around a pasted subject.
  const confirmed =
    admission !== null && (typed === expected || typed.trim() === expected);

  return (
    <Dialog
      open={admission !== null}
      onOpenChange={(next) => {
        if (!next && isPending) return;
        onOpenChange(next);
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Remove this machine's access?</Dialog.Title>
          <Dialog.Description>
            The machine can no longer exchange its identity token for a Gram
            session.
          </Dialog.Description>
        </Dialog.Header>
        {admission && (
          <div className="space-y-4 py-2">
            <Alert variant="warning" alignTop>
              Sessions it already holds are not revoked. To let it back in,
              allow access for it again with an agent.
            </Alert>
            <TypeToConfirmField
              id="remove-subject-confirm"
              label="Type the machine's subject to confirm"
              expected={expected}
              value={typed}
              onChange={setTyped}
            />
          </div>
        )}
        <Dialog.Footer>
          <Button
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="destructive-primary"
            disabled={!confirmed || isPending}
            onClick={() => {
              if (admission && confirmed) onConfirm(admission);
            }}
          >
            <Button.Text>
              {isPending ? "Removing…" : "Remove machine"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
