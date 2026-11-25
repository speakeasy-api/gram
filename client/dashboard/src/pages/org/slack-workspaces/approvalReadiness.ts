import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";

/** The Slack permission approval request DMs need. */
export const APPROVAL_DM_SCOPE = "chat:write";

/**
 * Connected workspaces that cannot send approval request DMs until an admin
 * reinstalls the app. Slack grants permissions at install time only, so an
 * install from before approvals keeps working without this one.
 */
export function workspacesNeedingReinstall(
  connections: SlackDirectoryConnection[],
): SlackDirectoryConnection[] {
  return connections.filter(
    (connection) =>
      connection.status === "connected" &&
      connection.missingScopes.includes(APPROVAL_DM_SCOPE),
  );
}

/** Whether at least one connected workspace can send approval request DMs. */
export function canSendApprovalDMs(
  connections: SlackDirectoryConnection[],
): boolean {
  return connections.some(
    (connection) =>
      connection.status === "connected" &&
      !connection.missingScopes.includes(APPROVAL_DM_SCOPE),
  );
}
