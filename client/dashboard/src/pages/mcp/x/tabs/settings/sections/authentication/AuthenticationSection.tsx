import { Badge } from "@/components/ui/Badge";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useMemo, useState, type ReactNode } from "react";
import { SettingsInlineEmptyState } from "../../SettingsInlineEmptyState";
import { SettingsSection } from "@/components/detail/settings-section";
import { AttachRemoteIdentityProviderSheet } from "./AttachRemoteIdentityProviderSheet";
import { AuthenticationSetupActions } from "./AuthenticationSetupActions";
import { type AuthTarget, useMcpServerAuthTarget } from "./authTarget";
import { DeleteRemoteIdentityProviderDialog } from "./DeleteRemoteIdentityProviderDialog";
import { ModifyRemoteIdentityProviderSheet } from "./ModifyRemoteIdentityProviderSheet";
import { RemoteIdentityProvidersField } from "./RemoteIdentityProvidersField";
import { UserSessionDurationField } from "./UserSessionDurationField";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import {
  type ProtectedResourceProbeStatus,
  useProtectedResourceMetadata,
} from "./useProtectedResourceMetadata";

export const MCP_AUTHENTICATION_SECTION_ID = "authentication";

/**
 * Chrome wrapper for the remote/tunneled MCP server settings tab. The
 * target-agnostic body below also mounts on the toolset detail page inside
 * that page's own section chrome.
 */
export function AuthenticationSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  const target = useMcpServerAuthTarget(mcpServer);

  return (
    <>
      <SettingsSection id={MCP_AUTHENTICATION_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Authentication</SettingsSection.Title>
          <SettingsSection.Description>
            {isUnproxied
              ? "Speakeasy doesn't manage authentication for unproxied servers."
              : "Configure user sessions and, when required, upstream identity providers for clients connecting to this server."}
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            {isUnproxied ? (
              <UnproxiedAuthenticationNotice />
            ) : (
              <AuthenticationSectionBody target={target} />
            )}
          </SettingsSection.Body>
          {isUnproxied ? null : (
            <SettingsSection.Footer>
              <SettingsSection.FooterHint>
                Authentication changes apply to new client connections.
              </SettingsSection.FooterHint>
            </SettingsSection.Footer>
          )}
        </SettingsSection.Panel>
      </SettingsSection>
    </>
  );
}

function UnproxiedAuthenticationNotice(): JSX.Element {
  return (
    <Field>
      <FieldLabel>Authentication</FieldLabel>
      <div>
        <Badge variant="success">Not applicable</Badge>
      </div>
      <FieldDescription>
        The customer connects directly using the vendor&apos;s own credentials —
        there&apos;s nothing for Speakeasy to configure here.
      </FieldDescription>
    </Field>
  );
}

/**
 * The auth configuration surface: identity-provider setup or the manage
 * fields, plus the attach/modify/delete overlays. Chrome-free so both the
 * remote server settings tab and the toolset detail page can mount it.
 */
export function AuthenticationSectionBody({
  target,
  additionalSetupAction,
}: {
  target: AuthTarget;
  additionalSetupAction?: ReactNode;
}): JSX.Element {
  const userSessionIssuerId = target.userSessionIssuerId ?? undefined;
  const issuerConfigured = !!userSessionIssuerId;

  const {
    data: userSessionIssuer,
    isLoading: isLoadingUserSessionIssuer,
    isError: isUserSessionIssuerError,
  } = useUserSessionIssuer({ id: userSessionIssuerId }, undefined, {
    enabled: issuerConfigured,
  });

  // listRemoteSessionIssuers returns this project's own issuers, inherited
  // organization-level ones (same org), and inherited platform issuers from the
  // shared catalog, so the selectable list spans all three tiers. A client can
  // be attached to any of them; only project-owned issuer metadata is editable
  // here.
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

  // Remote MCP servers receive a user-session issuer when they are created,
  // before any upstream OAuth client is attached. Keep protected-resource
  // discovery available in that recovery state so providers that advertise
  // scopes only in RFC 9728 metadata can still be configured manually.
  const shouldProbeProtectedResource =
    !!target.remoteMcpServerId &&
    (!issuerConfigured || (!isLoadingClients && allClients.length === 0));
  const { status: probeStatus, metadata: protectedResourceMetadata } =
    useProtectedResourceMetadata(
      target.remoteMcpServerId,
      shouldProbeProtectedResource,
    );
  const authorizationServer =
    protectedResourceMetadata?.authorizationServers?.[0];
  const protectedResourceScopes =
    protectedResourceMetadata?.scopesSupported ?? [];

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
  const [sheetInitialUrl, setSheetInitialUrl] = useState<string | undefined>();
  const [sheetInitialScopes, setSheetInitialScopes] = useState<string[]>();

  const openSheet = (initialIssuerUrl?: string, initialScopes?: string[]) => {
    setSheetInitialUrl(initialIssuerUrl);
    setSheetInitialScopes(initialScopes);
    setSheetOpen(true);
  };

  // Keep targets mounted for one render after close so exit animations retain
  // the row that triggered them.
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

  let authenticationFields: ReactNode;
  if (!issuerConfigured) {
    authenticationFields = (
      <IdentityProviderSetupField
        probeStatus={probeStatus}
        hasDiscoveredAuthorizationServer={!!authorizationServer}
        onUseDiscovered={() =>
          openSheet(authorizationServer, protectedResourceScopes)
        }
        onStartManual={() => openSheet(undefined)}
        additionalAction={additionalSetupAction}
      />
    );
  } else if (isLoadingUserSessionIssuer) {
    authenticationFields = <AuthenticationLoadingField />;
  } else if (isUserSessionIssuerError || !userSessionIssuer) {
    authenticationFields = <AuthenticationLoadErrorField />;
  } else {
    authenticationFields = (
      <>
        <UserSessionDurationField userSessionIssuer={userSessionIssuer} />
        <RemoteIdentityProvidersField
          associatedIssuers={associatedIssuers}
          isLoading={
            isLoadingIssuers || isLoadingClients || probeStatus === "loading"
          }
          onAdd={() => openSheet(authorizationServer, protectedResourceScopes)}
          onEdit={handleEdit}
          onDelete={handleDelete}
        />
      </>
    );
  }

  return (
    <>
      <FieldGroup className="gap-6">{authenticationFields}</FieldGroup>

      <AttachRemoteIdentityProviderSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        target={target}
        userSessionIssuer={userSessionIssuer ?? null}
        selectableIssuers={selectableIssuers}
        initialIssuerUrl={sheetInitialUrl}
        initialScopes={sheetInitialScopes}
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
    </>
  );
}

function IdentityProviderSetupField({
  probeStatus,
  hasDiscoveredAuthorizationServer,
  onUseDiscovered,
  onStartManual,
  additionalAction,
}: {
  probeStatus: ProtectedResourceProbeStatus;
  hasDiscoveredAuthorizationServer: boolean;
  onUseDiscovered: () => void;
  onStartManual: () => void;
  additionalAction?: ReactNode;
}) {
  return (
    <Field>
      <FieldLabel>Identity Provider</FieldLabel>
      <SettingsInlineEmptyState
        title="No authentication configured"
        description="Configure an upstream identity provider so MCP clients authenticate before reaching this server."
        action={
          <AuthenticationSetupActions
            probeStatus={probeStatus}
            hasDiscoveredAuthorizationServer={hasDiscoveredAuthorizationServer}
            onUseDiscovered={onUseDiscovered}
            onStartManual={onStartManual}
            additionalAction={additionalAction}
          />
        }
      />
      <FieldDescription>
        Clients authenticate through this provider before they can use server
        functionality.
      </FieldDescription>
    </Field>
  );
}

function AuthenticationLoadingField() {
  return (
    <Field>
      <FieldLabel>Authentication</FieldLabel>
      <Text muted small>
        Loading authentication configuration...
      </Text>
    </Field>
  );
}

function AuthenticationLoadErrorField() {
  return (
    <Field>
      <FieldLabel>Authentication</FieldLabel>
      <FieldError>
        Failed to load the authentication configuration. Refresh the page to try
        again.
      </FieldError>
    </Field>
  );
}
