import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { useEffect, useState } from "react";
import { TypeToConfirmField } from "./TypeToConfirmField";

function machinesPhrase(count: number): string {
  return count === 1 ? "The machine" : `All ${count} machines`;
}

export function WithdrawIssuerDialog({
  issuer,
  open,
  onOpenChange,
  onConfirm,
  isPending,
  machineCount,
}: {
  issuer: WorkloadIssuer | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (issuer: WorkloadIssuer) => void;
  isPending: boolean;
  /** How many machines are allowed under the platform, all withdrawn with it. */
  machineCount: number;
}): JSX.Element {
  const [typed, setTyped] = useState("");

  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);

  // The issuer URL rather than the name: it is what an assertion's iss claim
  // is matched against, so it names the trust being withdrawn exactly.
  const expected = issuer?.issuer ?? "";
  const confirmed = issuer !== undefined && typed.trim() === expected;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && isPending) return;
        onOpenChange(next);
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Stop trusting this platform?</Dialog.Title>
          <Dialog.Description>
            Gram stops accepting this platform&apos;s identity tokens.
          </Dialog.Description>
        </Dialog.Header>
        {issuer && (
          <div className="space-y-4 py-2">
            <Alert variant="warning" alignTop>
              {machineCount === 0
                ? "No machines are allowed under it, so nothing else changes. Existing sessions are not revoked."
                : `${machinesPhrase(machineCount)} allowed under it ${machineCount === 1 ? "is" : "are"} withdrawn too, and registering the platform again will not bring ${machineCount === 1 ? "it" : "them"} back. Existing sessions are not revoked.`}
            </Alert>
            <TypeToConfirmField
              id="withdraw-issuer-confirm"
              label="Type the platform's issuer URL to confirm"
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
              if (issuer && confirmed) onConfirm(issuer);
            }}
          >
            <Button.Text>
              {isPending ? "Withdrawing…" : "Stop trusting"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
