import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import type {
  McpServer,
  RemoteSessionIssuer,
} from "@gram/client/models/components";
import {
  useRemoteSessionIssuers,
  useUserSessionIssuer,
} from "@gram/client/react-query/index.js";
import { useMemo, useState } from "react";
import { AttachRemoteIdentityProviderSheet } from "./AttachRemoteIdentityProviderSheet";
import { DeleteRemoteIdentityProviderDialog } from "./DeleteRemoteIdentityProviderDialog";
import { EmptyAuthenticationState } from "./EmptyAuthenticationState";
import { UserSessionsSection } from "./UserSessionsSection";
import { ModifyRemoteIdentityProviderSheet } from "./ModifyRemoteIdentityProviderSheet";
import { RemoteIdentityProvidersTable } from "./RemoteIdentityProvidersTable";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import { useProtectedResourceMetadata } from "./useProtectedResourceMetadata";

export function AuthenticationTab({ mcpServer }: { mcpServer: McpServer }) {
  const userSessionIssuerId = mcpServer.userSessionIssuerId;
  const issuerConfigured = !!userSessionIssuerId;

  const { data: userSessionIssuer, isLoading: isLoadingUserSessionIssuer } =
    useUserSessionIssuer({ id: userSessionIssuerId }, undefined, {
      enabled: issuerConfigured,
    });

  // Probe the remote MCP server's protected-resource metadata so the empty
  // state can offer "Start With Discovered Configuration". The probe runs
  // server-side under guardian.Policy — see useProtectedResourceMetadata.
  const { status: probeStatus, metadata: protectedResourceMetadata } =
    useProtectedResourceMetadata(
      mcpServer.remoteMcpServerId,
      !issuerConfigured,
    );

  const authorizationServer =
    protectedResourceMetadata?.authorizationServers?.[0];

  // Pull project-scope remote_session_issuers + the clients paired with the
  // current user_session_issuer (when configured). The clients list tells us
  // which issuers are "associated" with this server.
  // NOTE(AGE-2494): listRemoteSessionIssuers is project-scope only — there's
  // no API surface for org-level records yet. When AGE-2494 lands, augment
  // the selectable list with org-scope rows.
  const { data: issuersResult, isLoading: isLoadingIssuers } =
    useRemoteSessionIssuers();
  const allIssuers = useMemo(
    () => issuersResult?.result.items ?? [],
    [issuersResult],
  );

  const { items: allClients, isLoading: isLoadingClients } =
    useAllRemoteSessionClients(
      { userSessionIssuerId },
      { enabled: issuerConfigured },
    );

  const associatedIssuerIds = useMemo(
    () => new Set(allClients.map((client) => client.remoteSessionIssuerId)),
    [allClients],
  );

  const associatedIssuers = useMemo<RemoteSessionIssuer[]>(
    () => allIssuers.filter((issuer) => associatedIssuerIds.has(issuer.id)),
    [allIssuers, associatedIssuerIds],
  );

  const selectableIssuers = useMemo<RemoteSessionIssuer[]>(
    () => allIssuers.filter((issuer) => !associatedIssuerIds.has(issuer.id)),
    [allIssuers, associatedIssuerIds],
  );

  const [sheetOpen, setSheetOpen] = useState(false);
  const [sheetInitialUrl, setSheetInitialUrl] = useState<string | undefined>(
    undefined,
  );

  const openSheet = (initialIssuerUrl?: string) => {
    setSheetInitialUrl(initialIssuerUrl);
    setSheetOpen(true);
  };

  // Delete + Modify dialog state. Keep the issuer around for one render after
  // close so the dialog/sheet body finishes its closing animation against the
  // row that triggered it.
  const [deleteTarget, setDeleteTarget] = useState<RemoteSessionIssuer | null>(
    null,
  );
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [modifyTarget, setModifyTarget] = useState<RemoteSessionIssuer | null>(
    null,
  );
  const [modifyOpen, setModifyOpen] = useState(false);

  const handleEdit = (issuer: RemoteSessionIssuer) => {
    setModifyTarget(issuer);
    setModifyOpen(true);
  };

  const handleDelete = (issuer: RemoteSessionIssuer) => {
    setDeleteTarget(issuer);
    setDeleteOpen(true);
  };

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      {!issuerConfigured ? (
        <section>
          <Heading variant="h4" className="mb-3">
            Get Started
          </Heading>
          <Type muted small className="mb-4">
            Configure an upstream identity provider so MCP clients authenticate
            against it before reaching this server.
          </Type>
          <EmptyAuthenticationState
            probeStatus={probeStatus}
            hasDiscoveredAuthorizationServer={!!authorizationServer}
            onUseDiscovered={() => openSheet(authorizationServer)}
            onStartManual={() => openSheet(undefined)}
          />
        </section>
      ) : isLoadingUserSessionIssuer || !userSessionIssuer ? (
        <Type muted small>
          Loading authentication configuration…
        </Type>
      ) : (
        <>
          <UserSessionsSection userSessionIssuer={userSessionIssuer} />
          <RemoteIdentityProvidersTable
            associatedIssuers={associatedIssuers}
            isLoading={isLoadingIssuers || isLoadingClients}
            onAdd={() => openSheet(undefined)}
            onEdit={handleEdit}
            onDelete={handleDelete}
            // TODO(AGE-2554): remove this gate when the runtime resolver
            // supports multiple remote_session_clients per
            // user_session_issuer. Today ResolveOneAccessToken enforces
            // len(clients) == 1 as an invariant; a second attach reaches a
            // state where MCP requests fail with CodeUnexpected.
            attachDisabledReason={
              associatedIssuers.length > 0
                ? "Multiple identity providers per server are not yet supported."
                : undefined
            }
          />
        </>
      )}

      <AttachRemoteIdentityProviderSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        mcpServer={mcpServer}
        userSessionIssuer={userSessionIssuer ?? null}
        selectableIssuers={selectableIssuers}
        initialIssuerUrl={sheetInitialUrl}
      />

      {deleteTarget && userSessionIssuerId && (
        <DeleteRemoteIdentityProviderDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          userSessionIssuerId={userSessionIssuerId}
          issuer={deleteTarget}
        />
      )}

      {modifyTarget && userSessionIssuer && (
        <ModifyRemoteIdentityProviderSheet
          open={modifyOpen}
          onOpenChange={setModifyOpen}
          userSessionIssuer={userSessionIssuer}
          issuer={modifyTarget}
        />
      )}
    </div>
  );
}
