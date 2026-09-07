import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { SIGNAL_DISMISS_CAP } from "./collect-findings";

/**
 * Confirmation for signal-level suppression: names the number of findings
 * actually collected (which can differ from the signal counts — see the
 * scan-time vs message-time note in SignalDrawer) and warns when the collection
 * hit the cap.
 */
export function SuppressFindingsDialog({
  results,
  subject,
  onCancel,
  onConfirm,
}: {
  /** Findings pending suppression; null keeps the dialog closed. */
  results: RiskResult[] | null;
  /** What the findings belong to, e.g. "this rule" or "3 selected signals". */
  subject: string;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  const count = results?.length ?? 0;
  const capped = count >= SIGNAL_DISMISS_CAP;
  return (
    <Dialog
      open={results !== null}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <Dialog.Content>
        <Dialog.Title>Suppress findings?</Dialog.Title>
        <Dialog.Description>
          {count === 0
            ? `No findings for ${subject}; there is nothing to suppress.`
            : `This suppresses ${count.toLocaleString()} ${
                count === 1 ? "finding" : "findings"
              } for ${subject}, regardless of the selected time window — they drop out of the risk score and every finding count.`}
          {capped &&
            ` Only the first ${SIGNAL_DISMISS_CAP.toLocaleString()} findings can be suppressed at once; run this again if more remain.`}
        </Dialog.Description>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onCancel}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="primary" disabled={count === 0} onClick={onConfirm}>
            <Button.Text>Suppress</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
