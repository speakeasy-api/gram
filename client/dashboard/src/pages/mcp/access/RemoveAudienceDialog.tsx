import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { JSX } from "react";
import type { AccessRow } from "./accessRows";

/**
 * Confirms taking a principal off one server, for the case where the button
 * does not do what it appears to: the access comes from a rule covering every
 * server, so this writes an exception instead — and an exception wins over
 * access given to someone individually. Both are worth saying before it lands
 * rather than after, in terms of who ends up able to do what.
 */
export function RemoveAudienceDialog({
  row,
  serverName,
  pending,
  onConfirm,
  onClose,
}: {
  row: AccessRow;
  serverName?: string;
  pending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  const server = serverName ?? "this server";
  const isRole = row.kind === "role";
  // A block beats a grant, so removing a group takes the server from anyone
  // it covers — including someone who was given it on their own.
  const isGroup = isRole || row.kind === "everyone";

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>
            Remove {row.displayName} from {server}?
          </Dialog.Title>
          <Dialog.Description>
            {isGroup
              ? `Everyone in ${row.displayName} loses this server: connecting, viewing and managing.`
              : `${row.displayName} loses this server: connecting, viewing and managing.`}
          </Dialog.Description>
        </Dialog.Header>

        <ul className="text-muted-foreground list-disc space-y-1 py-2 pl-5 text-sm">
          <li>Other servers are unaffected.</li>
          {isGroup && (
            // The surprise worth naming: this beats access someone was given
            // on their own, so it can take more than the row suggests.
            <li>
              Anyone in {row.displayName} loses this server even if they were
              also given it individually.
            </li>
          )}
        </ul>

        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose} disabled={pending}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="destructive-primary"
            onClick={onConfirm}
            disabled={pending}
          >
            <Button.Text>Remove</Button.Text>
          </Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
