import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import type { Action } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import {
  invalidateAllOrganizationRemoteSessionClientSessions,
  useOrganizationRemoteSessionClientSessions,
} from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
import { useRefreshOrganizationRemoteSessionMutation } from "@gram/client/react-query/refreshOrganizationRemoteSession.js";
import { useRevokeOrganizationRemoteSessionMutation } from "@gram/client/react-query/revokeOrganizationRemoteSession.js";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RevokeAllSessionsDialog } from "../../clientDialogs";
import { formatTimestamp } from "./formatTimestamp";

export function SessionsTab({ clientId }: { clientId: string }): JSX.Element {
  const queryClient = useQueryClient();
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const { data, isLoading, isError } =
    useOrganizationRemoteSessionClientSessions({
      clientId,
    });
  const sessionItems = data?.result.items ?? [];
  const [showRevokeAll, setShowRevokeAll] = useState(false);

  const revoke = useRevokeOrganizationRemoteSessionMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
        refetchType: "all",
      });
      toast.success("Session revoked");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to revoke session",
      );
    },
  });

  const refresh = useRefreshOrganizationRemoteSessionMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
        refetchType: "all",
      });
      toast.success("Session refreshed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to refresh session",
      );
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <RequireScope scope="org:admin" level="component">
          <Button
            variant="destructive-primary"
            size="sm"
            onClick={() => setShowRevokeAll(true)}
            disabled={sessionItems.length === 0}
          >
            <Button.Text>Revoke all sessions</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {isError ? (
        <Type className="text-destructive py-8 text-center">
          Failed to load sessions.
        </Type>
      ) : !isLoading && sessionItems.length === 0 ? (
        <Type muted className="py-8 text-center">
          No active sessions for this client.
        </Type>
      ) : (
        <DotTable
          headers={[
            { label: "Identity" },
            { label: "Created" },
            { label: "Refresh expires" },
            { label: "Access expires" },
            { label: "" },
          ]}
        >
          {sessionItems.map((session: RemoteSession) => {
            const actions: Action[] = [
              ...(session.hasRefreshToken
                ? [
                    {
                      label: "Refresh now",
                      disabled: refresh.isPending,
                      onClick: () =>
                        refresh.mutate({ request: { id: session.id } }),
                    },
                  ]
                : []),
              {
                label: "Revoke session",
                destructive: true,
                onClick: () => revoke.mutate({ request: { id: session.id } }),
              },
            ];
            return (
              <TableRowContextMenu
                key={session.id}
                actions={canManage ? actions : []}
              >
                <DotRow
                  icon={
                    <Icon
                      name="user"
                      className="text-muted-foreground h-5 w-5"
                    />
                  }
                >
                  <td className="px-3 py-3">
                    <Type small as="div" className="break-all">
                      {session.subjectDisplayName ??
                        session.subjectEmail ??
                        session.subjectUrn}
                    </Type>
                  </td>
                  <td className="px-3 py-3">
                    <Type small muted>
                      {formatTimestamp(session.createdAt)}
                    </Type>
                  </td>
                  <td className="px-3 py-3">
                    <Type small muted>
                      {formatTimestamp(session.refreshExpiresAt)}
                    </Type>
                  </td>
                  <td className="px-3 py-3">
                    <Type small muted>
                      {formatTimestamp(session.accessExpiresAt)}
                    </Type>
                  </td>
                  <td className="px-3 py-3 text-right">
                    <RequireScope scope="org:admin" level="section">
                      <div onClick={(e) => e.stopPropagation()}>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="tertiary" size="sm">
                              <Button.LeftIcon>
                                <MoreHorizontal className="h-4 w-4" />
                              </Button.LeftIcon>
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            {actions.map((action, index) => (
                              <DropdownMenuItem
                                key={index}
                                disabled={action.disabled}
                                onClick={() => action.onClick()}
                              >
                                {action.label}
                              </DropdownMenuItem>
                            ))}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </RequireScope>
                  </td>
                </DotRow>
              </TableRowContextMenu>
            );
          })}
        </DotTable>
      )}

      {showRevokeAll && (
        <RevokeAllSessionsDialog
          clientId={clientId}
          onClose={() => setShowRevokeAll(false)}
        />
      )}
    </div>
  );
}
