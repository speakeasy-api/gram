import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { JSX } from "react";
import { lineLabels, type AccessRow, type ScopeKey } from "./accessRows";

/**
 * Confirms clearing or removing a role whose rule covers every server. A rule
 * cannot cover every server but one, so the change rewrites the role itself:
 * it names each of the project's other servers instead, and stops picking up
 * servers added later. That is a change to the role on every server, worth
 * saying before it lands.
 */
export function ConvertWildcardDialog({
  change,
  serverName,
  projectName,
  otherServers,
  coveredEveryProject,
  ready,
  pending,
  onConfirm,
  onClose,
}: {
  change: { row: AccessRow; lines: ScopeKey[]; action: "clear" | "remove" };
  serverName: string;
  projectName: string;
  /** The project's servers besides this one; undefined while loading. */
  otherServers: number | undefined;
  /** True when the role's rule reaches every project, not only this one. */
  coveredEveryProject: boolean;
  /** False until the role and the project's servers have loaded. */
  ready: boolean;
  pending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  const role = change.row.displayName;
  const servers =
    otherServers === undefined
      ? `the other servers in ${projectName}`
      : otherServers === 1
        ? `the 1 other server in ${projectName}`
        : `the ${otherServers} other servers in ${projectName}`;

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={
        change.action === "remove"
          ? `Remove ${role} from ${serverName}?`
          : `Clear ${role}'s ${lineLabels(change.lines)} setting on ${serverName}?`
      }
      description={`${role} is set for every server. To leave ${serverName} out, ${role} will be changed to name ${servers} individually instead.`}
      confirmLabel="Change to specific servers"
      pendingLabel="Changing…"
      confirmVariant="primary"
      isPending={pending}
      onConfirm={onConfirm}
      confirmDisabled={!ready}
    >
      <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
        <li>Servers added to {projectName} later will not be included.</li>
        {coveredEveryProject && (
          <li>
            Other projects keep access to every server. Projects created later
            will not be included.
          </li>
        )}
        <li>You can change this back in Role Manager.</li>
      </ul>
    </ConfirmDialog>
  );
}
