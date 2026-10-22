import { IdentityLink } from "@/components/identity-link";
import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import type { Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import {
  invalidateAllOrganizationRemoteSessionClientSessions,
  useOrganizationRemoteSessionClientSessions,
} from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
import { useRefreshOrganizationRemoteSessionMutation } from "@gram/client/react-query/refreshOrganizationRemoteSession.js";
import { useRevokeOrganizationRemoteSessionMutation } from "@gram/client/react-query/revokeOrganizationRemoteSession.js";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { Icon } from "@/components/ui/Icon";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { useMemo, useState } from "react";
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
  // An agent id off a session is not permission to open that agent, so the
  // roster the management API is willing to return decides which rows link.
  const agents = useReadableAgents(
    sessionItems.some((session) => session.subjectUrn.startsWith("agent:")),
  );
  const agentsByUrn = useMemo(
    () =>
      new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((agent) => [
          `agent:${agent.id}`,
          agent,
        ]),
      ),
    [agents.data, agents.isError],
  );

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
        <Text className="text-destructive py-8 text-center">
          Failed to load sessions.
        </Text>
      ) : !isLoading && sessionItems.length === 0 ? (
        <Text muted className="py-8 text-center">
          No active sessions for this client.
        </Text>
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
                    // The agent rows draw their own face through
                    // ActingPrincipal, so the row's icon slot stays empty for
                    // them rather than stamping a person beside a machine.
                    session.subjectUrn.startsWith("agent:") ? null : (
                      <Icon
                        name="user"
                        className="text-muted-foreground h-5 w-5"
                      />
                    )
                  }
                >
                  <td className="px-3 py-3">
                    <Text small as="div" className="break-all">
                      {session.subjectUrn.startsWith("agent:") ? (
                        // No authorizer on a RemoteSession, so the agent
                        // stands alone — but it is named and linked now,
                        // where it used to fall through to a raw urn. Linked
                        // only once the roster returns it: an agent the
                        // viewer may not read would offer a page they
                        // cannot open.
                        <ActingPrincipal
                          agent={{
                            id: session.subjectUrn.slice("agent:".length),
                            name:
                              agentsByUrn.get(session.subjectUrn)?.name ??
                              session.subjectDisplayName ??
                              undefined,
                          }}
                          linked={agentsByUrn.has(session.subjectUrn)}
                        />
                      ) : (
                        <IdentityLink
                          // Session subjects can also be anonymous, which
                          // names no subject the identity resolver can
                          // address; those stay plain text.
                          identifier={
                            session.subjectUrn.startsWith("user:") ||
                            session.subjectUrn.startsWith("apikey:")
                              ? { urn: session.subjectUrn }
                              : null
                          }
                        >
                          {session.subjectDisplayName ??
                            session.subjectEmail ??
                            session.subjectUrn}
                        </IdentityLink>
                      )}
                    </Text>
                  </td>
                  <td className="px-3 py-3">
                    <Text small muted>
                      {formatTimestamp(session.createdAt)}
                    </Text>
                  </td>
                  <td className="px-3 py-3">
                    <Text small muted>
                      {formatTimestamp(session.refreshExpiresAt)}
                    </Text>
                  </td>
                  <td className="px-3 py-3">
                    <Text small muted>
                      {formatTimestamp(session.accessExpiresAt)}
                    </Text>
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
