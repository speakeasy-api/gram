import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { SIGNAL_DISMISS_CAP } from "./collect-findings";

/**
 * Confirmation for signal-level "mark false positive": names the number of
 * findings actually collected (which can differ from the signal counts — see
 * the scan-time vs message-time note in SignalDrawer) and warns when the
 * collection hit the cap.
 */
export function DismissFindingsDialog({
  results,
  subject,
  onCancel,
  onConfirm,
}: {
  /** Findings pending dismissal; null keeps the dialog closed. */
  results: RiskResult[] | null;
  /** What the findings belong to, e.g. "this rule" or "3 selected signals". */
  subject: string;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  const capped = (results?.length ?? 0) >= SIGNAL_DISMISS_CAP;
  return (
    <Dialog
      open={results !== null}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <Dialog.Content>
        <Dialog.Title>Mark as false positive?</Dialog.Title>
        <Dialog.Description>
          This marks {results?.length.toLocaleString()}{" "}
          {results?.length === 1 ? "finding" : "findings"} for {subject} in the
          selected window as false positives.
          {capped &&
            " There are more findings than can be marked at once; run this again to continue."}
        </Dialog.Description>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onCancel}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="primary" onClick={onConfirm}>
            <Button.Text>Mark false positive</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
