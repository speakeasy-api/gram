import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { useEffect, useState } from "react";
import { TypeToConfirmField } from "./TypeToConfirmField";
import { withdrawConfirmation } from "./withdrawConfirmation";

export function WithdrawSubjectDialog({
  admission,
  onOpenChange,
  onConfirm,
  isPending,
}: {
  /** The machine being withdrawn; the dialog is open while this is set. */
  admission: WorkloadAdmission | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: (admission: WorkloadAdmission) => void;
  isPending: boolean;
}): JSX.Element {
  const [typed, setTyped] = useState("");

  // A fresh confirmation for every machine, so text typed for one never
  // unlocks the withdrawal of the next.
  useEffect(() => {
    setTyped("");
  }, [admission?.id]);

  const expected = admission ? withdrawConfirmation(admission) : "";
  const confirmed = admission !== null && typed.trim() === expected;

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
          <Dialog.Title>Withdraw this machine?</Dialog.Title>
          <Dialog.Description>
            The machine can no longer exchange its identity token for a Gram
            session.
          </Dialog.Description>
        </Dialog.Header>
        {admission && (
          <div className="space-y-4 py-2">
            <Alert variant="warning" alignTop>
              Sessions it already holds are not revoked. To allow it again,
              allow it with an agent as before.
            </Alert>
            <TypeToConfirmField
              id="withdraw-subject-confirm"
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
              {isPending ? "Withdrawing…" : "Withdraw machine"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
