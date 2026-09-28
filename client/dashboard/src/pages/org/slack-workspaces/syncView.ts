import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";

export function syncInProgress(connection: SlackDirectoryConnection): boolean {
  return ["queued", "running", "retrying"].includes(connection.syncStatus);
}
