import { Badge } from "@/components/ui/Badge";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useMemo, useState, type ReactNode } from "react";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { AuthRow } from "./AuthRow";
import { SettingsSection } from "@/components/detail/settings-section";
import { AttachRemoteIdentityProviderSheet } from "./AttachRemoteIdentityProviderSheet";
import { AuthenticationSetupActions } from "./AuthenticationSetupActions";
import { type AuthTarget, useMcpServerAuthTarget } from "./authTarget";
import { DeleteRemoteIdentityProviderDialog } from "./DeleteRemoteIdentityProviderDialog";
import { ModifyRemoteIdentityProviderSheet } from "./ModifyRemoteIdentityProviderSheet";
import { RemoteIdentityProvidersField } from "./RemoteIdentityProvidersField";
import { UserIdentitySessionControls } from "./UserIdentitySessionControls";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import {
  type ProtectedResourceProbeStatus,
  useProtectedResourceMetadata,
} from "./useProtectedResourceMetadata";
import { RemoteMcpIdentitySectionBody } from "./RemoteMcpIdentitySection";

export const MCP_AUTHENTICATION_SECTION_ID = "authentication";

function authenticationSectionDescription(
  isUnproxied: boolean,
  isRemoteMcp: boolean,
): string {
  if (isUnproxied) {
    return "Speakeasy doesn't manage authentication for unproxied servers.";
  }
  if (isRemoteMcp) {
    return "Choose whether upstream requests act as each user, one shared agent, or no identity.";
  }
  return "Who may connect to this server and how they sign in. Changes take effect on new connections.";
}

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
  const isRemoteMcp = !!mcpServer.remoteMcpServerId;
  const target = useMcpServerAuthTarget(mcpServer);

  return (
    <SettingsSection id={MCP_AUTHENTICATION_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>
          {isRemoteMcp ? "Identity" : "Authentication"}
        </SettingsSection.Title>
        <SettingsSection.Description>
          {authenticationSectionDescription(isUnproxied, isRemoteMcp)}
        </SettingsSection.Description>
      </SettingsSection.Header>
      {isUnproxied ? (
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <UnproxiedAuthenticationNotice />
          </SettingsSection.Body>
        </SettingsSection.Panel>
      ) : (
        <AuthenticationSectionBody target={target} />
      )}
    </SettingsSection>
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
 * rows, plus the attach/modify/delete overlays. It owns its own panel (the
 * rows share one bordered surface and its dividers) but not the section
 * heading, so each shell supplies only the header above it.
 */
export function AuthenticationSectionBody({
  target,
  additionalSetupAction,
}: {
  target: AuthTarget;
  additionalSetupAction?: ReactNode;
}): JSX.Element {
  if (target.kind === "remote-mcp") {
    return <RemoteMcpIdentitySectionBody target={target} />;
  }

  return (
    <StandardAuthenticationSectionBody
      target={target}
      additionalSetupAction={additionalSetupAction}
    />
  );
}

function StandardAuthenticationSectionBody({
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
        <UserIdentitySessionControls userSessionIssuer={userSessionIssuer} />
        <RemoteIdentityProvidersField
          associatedIssuers={associatedIssuers}
          allowAdditionalProviders={!!target.multipleProviders}
          projectId={target.projectId}
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
      {/* No footer hint: the section description above already says these
          changes take effect on new connections. */}
      <SettingsSection.Panel>
        <div className="divide-y">{authenticationFields}</div>
      </SettingsSection.Panel>

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
    <AuthRow
      label="Identity provider"
      hint="Nobody can be identified here until a provider vouches for them."
    >
      <InlineEmptyState
        icon="key-round"
        heading="Set up authentication"
        description="Require MCP clients to authenticate through an upstream identity provider before reaching this server."
        className="py-8"
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
    </AuthRow>
  );
}

function AuthenticationLoadingField() {
  return (
    <AuthRow label="Authentication">
      <Text muted small>
        Loading…
      </Text>
    </AuthRow>
  );
}

function AuthenticationLoadErrorField() {
  return (
    <AuthRow label="Authentication">
      <FieldError>
        Failed to load the authentication configuration. Refresh the page to try
        again.
      </FieldError>
    </AuthRow>
  );
}
