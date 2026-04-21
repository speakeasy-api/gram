import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { CheckCircle2, XCircle } from "lucide-react";

import type { WizardState } from "./types";

export function ResultStep({
  state,
  onClose,
}: {
  state: Extract<WizardState, { step: "result" }>;
  onClose: () => void;
}) {
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        {state.success ? (
          <CheckCircle2 className="h-12 w-12 text-emerald-500" />
        ) : (
          <XCircle className="text-destructive h-12 w-12" />
        )}
        <Type className="text-center text-lg font-medium">
          {state.success ? "OAuth Configured" : "Configuration Failed"}
        </Type>
        <Type muted small className="max-w-md text-center">
          {state.message}
        </Type>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </Dialog.Footer>
    </>
  );
}
