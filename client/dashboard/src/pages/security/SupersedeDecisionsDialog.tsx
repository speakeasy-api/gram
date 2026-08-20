import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import type { ShadowMCPDecisionConflict } from "./policy-shadow-mcp-setup";

/**
 * Confirms a policy URL-list save that contradicts recorded access
 * decisions. The server refuses such a save outright unless it carries the
 * supersede confirmation, so this dialog is the only path to a contradicting
 * save — cancelling leaves both the list and the decisions untouched.
 */
export function SupersedeDecisionsDialog({
  conflicts,
  saving,
  onCancel,
  onConfirm,
}: {
  conflicts: ShadowMCPDecisionConflict[] | null;
  saving: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  const open = conflicts !== null && conflicts.length > 0;
  const count = conflicts?.length ?? 0;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && saving) return;
        if (!nextOpen) onCancel();
      }}
    >
      <Dialog.Content closeable={!saving}>
        <Dialog.Header>
          <Dialog.Title>Supersede recorded decisions?</Dialog.Title>
          <Dialog.Description>
            This change contradicts {count === 1 ? "a" : count} recorded access
            {count === 1 ? " decision" : " decisions"}. Saving supersedes{" "}
            {count === 1 ? "it" : "them"}: the review history and rationale are
            kept, but the {count === 1 ? "decision stops" : "decisions stop"}{" "}
            being enforced until someone re-decides.
          </Dialog.Description>
        </Dialog.Header>
        <ul className="max-h-60 space-y-2 overflow-y-auto">
          {conflicts?.map((conflict) => (
            <li
              key={conflict.canonicalServerUrl}
              className="flex items-center justify-between gap-3"
            >
              <Text small className="truncate">
                {conflict.serverName || conflict.canonicalServerUrl}
              </Text>
              {conflict.decision === "approved" ? (
                <Badge variant="success">Approved</Badge>
              ) : (
                <Badge variant="destructive">Denied</Badge>
              )}
            </li>
          ))}
        </ul>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onCancel} disabled={saving}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="destructive-primary"
            onClick={onConfirm}
            disabled={saving}
          >
            <Button.Text>
              {saving ? "Saving…" : "Supersede and save"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
