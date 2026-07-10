import { RequireScope } from "@/components/require-scope";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Type } from "@/components/ui/type";
import { toastError } from "@/lib/toast-error";
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
} from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, User } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RevokeAllSessionsDialog } from "../../clientDialogs";
import { formatTimestamp } from "./formatTimestamp";

export function SessionsTab({ clientId }: { clientId: string }): JSX.Element {
  const queryClient = useQueryClient();
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
      toastError(error, "Failed to revoke session");
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
      toastError(error, "Failed to refresh session");
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
          {sessionItems.map((session: RemoteSession) => (
            <DotRow
              key={session.id}
              icon={<User className="text-muted-foreground h-5 w-5" />}
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
                        {session.hasRefreshToken && (
                          <DropdownMenuItem
                            disabled={refresh.isPending}
                            onClick={() =>
                              refresh.mutate({
                                request: {
                                  id: session.id,
                                },
                              })
                            }
                          >
                            Refresh now
                          </DropdownMenuItem>
                        )}
                        <DropdownMenuItem
                          onClick={() =>
                            revoke.mutate({
                              request: {
                                id: session.id,
                              },
                            })
                          }
                        >
                          Revoke session
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </RequireScope>
              </td>
            </DotRow>
          ))}
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
