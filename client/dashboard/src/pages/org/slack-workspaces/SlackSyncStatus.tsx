import { syncInProgress } from "./syncView";
import { LoaderCircle } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { HumanizeDateTime } from "@/lib/dates";
import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";
import { invalidateAllSlackDirectoryConnections } from "@gram/client/react-query/slackDirectoryConnections.js";
import { invalidateAllSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { useSyncSlackDirectoryMutation } from "@gram/client/react-query/syncSlackDirectory.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";

function syncFailure(code?: string): string {
  switch (code) {
    case "authorization_expired":
    case "reconnect_required":
    case "credential_unavailable":
      return "Reconnect this workspace before syncing again.";
    case "rate_limited":
      return "Slack limited requests. Try syncing again later.";
    case "directory_too_large":
      return "This directory exceeds the sync limit. Contact support.";
    case "invalid_directory":
      return "Slack returned an incomplete directory. Try syncing again.";
    case undefined:
    default:
      return "The sync could not finish. Try syncing again.";
  }
}

export function SlackSyncStatus({
  connection,
}: {
  connection: SlackDirectoryConnection;
}): JSX.Element {
  const running = syncInProgress(connection);
  const failed =
    connection.syncStatus === "failed" ||
    (!running &&
      connection.lastSyncFailedAt &&
      (!connection.lastFullSyncSucceededAt ||
        new Date(connection.lastSyncFailedAt) >
          new Date(connection.lastFullSyncSucceededAt)));
  let progress = "Waiting for the sync to start…";
  if (connection.syncStatus === "retrying") progress = "Retrying sync…";
  else if (connection.syncPhase === "waiting_for_slack")
    progress = "Waiting for Slack’s rate limit…";
  else if (connection.syncPhase === "publishing")
    progress = "Saving the complete directory…";
  else if (connection.syncStatus === "running")
    progress = "Reading workspace members…";

  return (
    <div className="text-muted-foreground space-y-1 text-sm">
      {connection.lastFullSyncSucceededAt ? (
        <p>
          Last full sync{" "}
          <HumanizeDateTime date={connection.lastFullSyncSucceededAt} /> ·{" "}
          {connection.memberCount.toLocaleString()} members observed
        </p>
      ) : (
        <p>Never synced</p>
      )}
      {connection.directoryStatus === "stale" && (
        <p>
          Saved directory from a previous or unavailable authorization.{" "}
          {connection.status === "connected"
            ? "Sync to refresh it."
            : "Reconnect and sync to refresh it."}
        </p>
      )}
      {running && (
        <div role="status" className="flex items-center gap-2">
          <LoaderCircle
            className="size-4 shrink-0 animate-spin"
            aria-hidden="true"
          />
          <span>
            {progress}{" "}
            {connection.syncPages
              ? `${connection.syncPages} pages read · ${connection.syncMembers ?? 0} members found`
              : ""}
          </span>
        </div>
      )}
      {connection.syncStatus === "unknown" && (
        <p role="status">
          Sync progress is unavailable. Refresh to check again.
        </p>
      )}
      {failed && (
        <p role="alert" className="text-destructive">
          {connection.lastSyncFailedAt && (
            <>
              Last attempt failed{" "}
              <HumanizeDateTime date={connection.lastSyncFailedAt} />.{" "}
            </>
          )}
          {syncFailure(connection.lastErrorCode)} The last complete directory is
          kept.
        </p>
      )}
    </div>
  );
}

export function SlackSyncButton({
  connection,
}: {
  connection: SlackDirectoryConnection;
}): JSX.Element {
  const client = useQueryClient();
  const sync = useSyncSlackDirectoryMutation({
    onSuccess: () => {
      toast.success("Directory sync requested");
      void invalidateAllSlackDirectoryConnections(client);
      void invalidateAllSlackDirectoryMembers(client);
    },
    onError: () => {
      void invalidateAllSlackDirectoryConnections(client);
    },
  });
  const running = syncInProgress(connection) || sync.isPending;
  return (
    <div className="space-y-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={connection.status !== "connected" || running}
        onClick={() =>
          sync.mutate({
            request: {
              syncSlackDirectoryRequestBody: {
                id: connection.id,
                generation: connection.generation,
              },
            },
            security: SESSION_SECURITY,
          })
        }
      >
        <Button.Text>{running ? "Syncing…" : "Sync members"}</Button.Text>
      </Button>
      <ApiErrorAlert error={sync.error} />
    </div>
  );
}
