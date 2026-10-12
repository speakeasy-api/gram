import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { JSX } from "react";
import type { AccessRow } from "./accessRows";

/**
 * Confirms taking a principal off one server, for the case where the button
 * does not do what it appears to: the access comes from a rule covering every
 * server, so this writes an exception instead. An exception against a role or
 * everyone does not reach people given this server by name, whose own rule
 * outranks it. Both are worth saying before it lands rather than
 * after, in terms of who ends up able to do what.
 */
export function RemoveAudienceDialog({
  row,
  keptBy,
  serverName,
  pending,
  onConfirm,
  onClose,
}: {
  row: AccessRow;
  /** People who keep access through their own rule here. */
  keptBy: string[];
  serverName?: string;
  pending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  const server = serverName ?? "this server";
  const isRole = row.kind === "role";
  // Removing a group takes the server from anyone it covers, except the
  // people given it here by name.
  const isGroup = isRole || row.kind === "everyone";
  // "Everyone in Everyone" is not a sentence: the everyone row already names
  // the group, so it takes the verb directly.
  // Anyone kept individually is named below, so the summary must not claim
  // the whole group loses the server.
  const loses =
    isGroup && keptBy.length > 0
      ? "loses this server: connecting, viewing and managing, except anyone given it individually."
      : "loses this server: connecting, viewing and managing.";
  const groupPhrase = (rest: string) =>
    row.kind === "everyone"
      ? `Everyone ${rest}`
      : isRole
        ? `Everyone in ${row.displayName} ${rest}`
        : `${row.displayName} ${rest}`;

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
          <Dialog.Description>{groupPhrase(loses)}</Dialog.Description>
        </Dialog.Header>

        <ul className="text-muted-foreground list-disc space-y-1 py-2 pl-5 text-sm">
          <li>Other servers are unaffected.</li>
          {isGroup && (
            // The preflight worth naming: access given to someone by name
            // outranks this block, so it takes less than the row suggests.
            <li>{keptSentence(keptBy)}</li>
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

/** Most names listed before the rest are counted instead. */
const KEPT_NAMES_SHOWN = 3;

/** Who keeps this server through access given to them by name. */
function keptSentence(keptBy: string[]): string {
  if (keptBy.length === 0) {
    return "People given this server individually keep that access.";
  }
  const verb = keptBy.length === 1 ? "keeps" : "keep";
  return `${listNames(keptBy)} ${verb} the access given to them individually.`;
}

/** "A", "A and B", "A, B and C", or "A, B, C and 2 others". */
function listNames(names: string[]): string {
  const shown = names.slice(0, KEPT_NAMES_SHOWN);
  const rest = names.length - shown.length;
  if (rest > 0) {
    return `${shown.join(", ")} and ${rest} ${rest === 1 ? "other" : "others"}`;
  }
  if (shown.length === 1) return shown[0]!;
  return `${shown.slice(0, -1).join(", ")} and ${shown.at(-1)}`;
}
