import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { invalidateAllUserSessions } from "@gram/client/react-query/userSessions.js";

import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { clientDocumentOrigin } from "@/lib/user-session-client-source";
import { UserSessionClientsList } from "./UserSessionClientsList";
import { UserSessionsList } from "./UserSessionsList";

/**
 * Body of the Clients and Sessions tab, shared verbatim by the toolset-backed
 * and mcp_servers-backed MCP server detail pages so the two cannot drift.
 *
 * Both listings are scoped to the server's user_session_issuer. One issuer can
 * back more than one MCP server, so the sessions and clients shown here are the
 * issuer's, not strictly this server's — SessionRow surfaces the gating issuer
 * slug per row for that reason.
 */
export function ClientsAndSessionsTab({
  issuerId,
}: {
  issuerId: string | undefined;
}): JSX.Element {
  const queryClient = useQueryClient();
  // The filter remembers which issuer it was set against. Navigating between
  // two MCP servers reuses this component instance rather than remounting it
  // (and when the target server is already in the query cache there is no
  // loading gap to unmount through), so a filter keyed only by client id would
  // survive onto the next server and silently scope its sessions to a client
  // that belongs to a different issuer. Derived during render rather than
  // reset in an effect, so there is no stale first paint.
  const [filter, setFilter] = useState<{
    issuerId: string;
    client: UserSessionClient;
  } | null>(null);
  // Compare on `filter &&` rather than `filter?.issuerId === issuerId`: with
  // no filter set and no issuer attached, the optional-chained form compares
  // undefined to undefined, takes the truthy branch, and dereferences null.
  const sessionFilter =
    filter && filter.issuerId === issuerId ? filter.client : null;
  // client_name is client-chosen; pair it with the unspoofable document origin
  // so the chip cannot be made to read like a client it isn't.
  const filterOrigin = sessionFilter
    ? clientDocumentOrigin(sessionFilter)
    : null;

  const selectClient = (client: UserSessionClient) => {
    if (!issuerId) return;
    setFilter({ issuerId, client });
  };

  // Revoking a client cascades to every session it issued, so the sessions
  // list above has to be refetched too or it keeps offering Revoke actions for
  // sessions the backend already deleted. Only the filter pointing at the
  // revoked client is cleared; revoking some other client leaves the current
  // drill-down where the operator put it.
  const onClientRevoked = (revoked: UserSessionClient) => {
    setFilter((current) =>
      current?.client.id === revoked.id ? null : current,
    );
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
  };

  if (!issuerId) {
    return (
      <Text muted small>
        This server isn&apos;t gated by a session issuer, so no clients can
        register against it and no sessions can be established. Configure
        authentication to start accepting OAuth connections.
      </Text>
    );
  }

  return (
    <Stack gap={6}>
      <Stack gap={3}>
        <Stack gap={1}>
          <Text variant="subheading">Sessions</Text>
          <Text small muted>
            Active sessions clients hold via this server's session issuer,
            established over OAuth. An issuer can gate more than one server, so
            each row names the issuer that gated it.
          </Text>
        </Stack>
        {sessionFilter && (
          <div className="flex items-center gap-2">
            <Text small muted>
              Filtered to {sessionFilter.clientName}
              {filterOrigin ? ` (${filterOrigin})` : ""}
            </Text>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setFilter(null)}
            >
              Clear filter
            </Button>
          </div>
        )}
        <UserSessionsList issuerId={issuerId} clientId={sessionFilter?.id} />
      </Stack>

      <Stack gap={3}>
        <Stack gap={1}>
          <Text variant="subheading">Clients</Text>
          <Text small muted>
            OAuth clients registered against this server's session issuer,
            whether they registered up front (DCR) or identified themselves with
            a metadata document (CIMD).
          </Text>
        </Stack>
        <UserSessionClientsList
          issuerId={issuerId}
          onViewSessions={selectClient}
          onClientRevoked={onClientRevoked}
        />
      </Stack>
    </Stack>
  );
}
