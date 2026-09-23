import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { parseAsString, useQueryState } from "nuqs";
import { Hash, Plus, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { safeExternalHttpUrl } from "@/lib/safe-external-url";
import { ConfirmDialog } from "@/pages/remote-identity-providers/ConfirmDialog";
import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";
import { useBeginSlackDirectoryConnectionMutation } from "@gram/client/react-query/beginSlackDirectoryConnection.js";
import { useDisconnectSlackDirectoryConnectionMutation } from "@gram/client/react-query/disconnectSlackDirectoryConnection.js";
import {
  invalidateAllSlackDirectoryConnections,
  useSlackDirectoryConnections,
} from "@gram/client/react-query/slackDirectoryConnections.js";
import {
  inlineError,
  SESSION_SECURITY,
} from "../identity-provider/identityProviderQueries";

const outcomes: Record<string, string> = {
  connected: "Slack workspace connected.",
  cancelled:
    "Slack authorization was cancelled. Your connections have not changed.",
  invalid_state:
    "This authorization link expired or belongs to another session. Connect Slack again.",
  wrong_workspace:
    "That is a different Slack workspace. Reconnect and choose the workspace shown here.",
  connection_changed:
    "The workspace state changed during authorization. Try connecting again.",
  authorization_failed:
    "Slack authorization failed. Try connecting again, or contact your administrator.",
  unavailable:
    "Slack connections are unavailable or your access changed. Return to Identity and try again.",
};

function connectionError(code?: string): string {
  if (code === "authorization_expired")
    return "Slack authorization expired. Reconnect to restore access.";
  if (code === "credential_unavailable")
    return "Stored credentials are unavailable. Reconnect this workspace.";
  return "Authorize this workspace to restore access.";
}
function statusVariant(
  status: SlackDirectoryConnection["status"],
): "success" | "warning" | "neutral" {
  if (status === "connected") return "success";
  if (status === "reconnect_required") return "warning";
  return "neutral";
}
function statusLabel(status: SlackDirectoryConnection["status"]): string {
  if (status === "connected") return "Connected";
  if (status === "disconnected") return "Disconnected";
  return "Reconnect required";
}

export function SlackWorkspaces(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <SlackWorkspacesContent />
    </RequireScope>
  );
}
function SlackWorkspacesContent(): JSX.Element {
  const queryClient = useQueryClient();
  const query = useSlackDirectoryConnections(undefined, SESSION_SECURITY, {
    retry: false,
    throwOnError: false,
  });
  const [result, setResult] = useQueryState("slack_result", parseAsString);
  const [outcome, setOutcome] = useState(result);
  useEffect(() => {
    if (!result) return;
    setOutcome(result);
    void setResult(null);
    void invalidateAllSlackDirectoryConnections(queryClient);
  }, [result, setResult, queryClient]);
  const [selected, setSelected] = useState<SlackDirectoryConnection | null>(
    null,
  );
  const [redirectError, setRedirectError] = useState<string | null>(null);
  const begin = useBeginSlackDirectoryConnectionMutation({
    onError: inlineError,
  });
  const disconnect = useDisconnectSlackDirectoryConnectionMutation({
    onError: () => {
      void invalidateAllSlackDirectoryConnections(queryClient);
    },
    onSuccess: () => {
      setSelected(null);
      toast.success("Slack workspace disconnected");
      void invalidateAllSlackDirectoryConnections(queryClient);
    },
  });
  const connections = query.data?.connections ?? [];
  const configured = query.data?.authorizationConfigured === true;
  const start = (connectionId?: string) => {
    setRedirectError(null);
    begin.mutate(
      {
        request: { beginSlackDirectoryConnectionRequestBody: { connectionId } },
        security: SESSION_SECURITY,
      },
      {
        onSuccess: (response) => {
          const destination = safeExternalHttpUrl(response.authorizationUrl);
          if (
            !destination ||
            new URL(destination).origin !== "https://slack.com" ||
            new URL(destination).pathname !== "/oauth/v2/authorize"
          ) {
            setRedirectError(
              "Slack returned an invalid authorization link. Please try again.",
            );
            return;
          }
          window.location.assign(destination);
        },
      },
    );
  };
  const close = () => {
    if (!disconnect.isPending) {
      setSelected(null);
      disconnect.reset();
    }
  };
  const notice =
    outcome && Object.hasOwn(outcomes, outcome) ? outcomes[outcome] : undefined;

  return (
    <section className="max-w-4xl space-y-6" aria-label="Slack workspaces">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-xl space-y-2">
          <Heading variant="h3">Slack workspaces</Heading>
          <Text muted small>
            Authorize the Slack workspaces your organization uses. You can
            connect more than one workspace.
          </Text>
        </div>
        <Button
          onClick={() => start()}
          disabled={!configured || begin.isPending || query.isPending}
        >
          <Button.LeftIcon>
            <Plus className="size-4" aria-hidden="true" />
          </Button.LeftIcon>
          <Button.Text>
            {begin.isPending ? "Opening Slack…" : "Connect Slack"}
          </Button.Text>
        </Button>
      </div>

      {notice && (
        <Alert
          variant={outcome === "connected" ? "success" : "info"}
          dismissible
          onDismiss={() => setOutcome(null)}
        >
          {notice}
        </Alert>
      )}
      <ApiErrorAlert error={query.error ?? begin.error} />
      {redirectError && <Alert variant="error">{redirectError}</Alert>}
      {query.isError && (
        <Button
          variant="secondary"
          onClick={() => {
            void query.refetch();
          }}
        >
          Try again
        </Button>
      )}
      {query.data && !configured && (
        <Alert variant="info" dismissible={false}>
          Ask your deployment administrator to configure the Slack directory app
          before connecting a workspace.
        </Alert>
      )}

      {query.isPending && (
        <div
          role="status"
          className="text-muted-foreground flex items-center gap-2 py-8 text-sm"
        >
          <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
          Loading workspaces…
        </div>
      )}
      {query.data && connections.length === 0 && (
        <InlineEmptyState
          icon="hash"
          heading="Connect your first workspace"
          description="Choose a workspace in Slack and approve access to its member directory."
        />
      )}
      {connections.length > 0 && (
        <ul
          className="border-border divide-border divide-y border"
          aria-label="Slack workspaces"
        >
          {connections.map((connection) => (
            <li
              key={connection.id}
              className="flex flex-wrap items-center justify-between gap-4 p-5"
            >
              <div className="flex min-w-0 items-center gap-4">
                <Hash
                  className="text-muted-foreground size-5 shrink-0"
                  aria-hidden="true"
                />
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-3">
                    <h3 className="break-words font-medium">
                      {connection.workspaceName || connection.workspaceId}
                    </h3>
                    <Badge variant={statusVariant(connection.status)}>
                      {statusLabel(connection.status)}
                    </Badge>
                  </div>
                  <p className="text-muted-foreground font-mono text-xs">
                    {connection.workspaceId}
                  </p>
                  {connection.status === "reconnect_required" && (
                    <p className="text-muted-foreground text-sm">
                      {connectionError(connection.lastErrorCode)}
                    </p>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={!configured || begin.isPending}
                  onClick={() => start(connection.id)}
                >
                  Reconnect
                </Button>
                {connection.status !== "disconnected" && (
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() => {
                      disconnect.reset();
                      setSelected(connection);
                    }}
                  >
                    Disconnect
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      <p className="text-muted-foreground max-w-2xl text-sm">
        Slack access is limited to reading workspace members and their email
        addresses.
      </p>
      <ConfirmDialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) close();
        }}
        title={`Disconnect ${selected?.workspaceName || selected?.workspaceId || "workspace"}?`}
        description="Speakeasy will stop using this connection. The app stays installed in Slack, and workspace history is kept. You can reconnect later."
        confirmLabel="Disconnect workspace"
        isPending={disconnect.isPending}
        error={disconnect.error?.message}
        onConfirm={() => {
          if (!selected) return;
          const current = connections.find(
            (connection) => connection.id === selected.id,
          );
          if (current && current.generation !== selected.generation) {
            disconnect.reset();
            setSelected(current);
            toast.info("The connection changed. Review it and confirm again.");
            return;
          }
          disconnect.mutate({
            request: {
              disconnectSlackDirectoryConnectionRequestBody: {
                id: selected.id,
                generation: selected.generation,
              },
            },
            security: SESSION_SECURITY,
          });
        }}
      />
    </section>
  );
}
