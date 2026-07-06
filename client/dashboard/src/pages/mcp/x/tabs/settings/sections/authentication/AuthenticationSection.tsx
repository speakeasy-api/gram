import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Type } from "@/components/ui/type";
import type {
  McpServer,
  RemoteSessionIssuer,
} from "@gram/client/models/components";
import {
  useRemoteSessionIssuers,
  useUserSessionIssuer,
} from "@gram/client/react-query/index.js";
import { useMemo, useState, type ReactNode } from "react";
import { SettingsInlineEmptyState } from "../../SettingsInlineEmptyState";
import { SettingsSection } from "../../SettingsSection";
import { AttachRemoteIdentityProviderSheet } from "./AttachRemoteIdentityProviderSheet";
import { AuthenticationSetupActions } from "./AuthenticationSetupActions";
import { DeleteRemoteIdentityProviderDialog } from "./DeleteRemoteIdentityProviderDialog";
import { McpServerSessionsPanel } from "./McpServerSessionsPanel";
import { ModifyRemoteIdentityProviderSheet } from "./ModifyRemoteIdentityProviderSheet";
import { RemoteIdentityProvidersField } from "./RemoteIdentityProvidersField";
import { UserSessionDurationField } from "./UserSessionDurationField";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import {
  type ProtectedResourceProbeStatus,
  useProtectedResourceMetadata,
} from "./useProtectedResourceMetadata";

export const MCP_AUTHENTICATION_SECTION_ID = "authentication";

export function AuthenticationSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const userSessionIssuerId = mcpServer.userSessionIssuerId;
  const issuerConfigured = !!userSessionIssuerId;

  const {
    data: userSessionIssuer,
    isLoading: isLoadingUserSessionIssuer,
    isError: isUserSessionIssuerError,
  } = useUserSessionIssuer({ id: userSessionIssuerId }, undefined, {
    enabled: issuerConfigured,
  });

  // Probe the remote MCP server's protected-resource metadata so setup can
  // offer discovery when the server advertises OAuth metadata.
  const { status: probeStatus, metadata: protectedResourceMetadata } =
    useProtectedResourceMetadata(
      mcpServer.remoteMcpServerId,
      !issuerConfigured,
    );
  const authorizationServer =
    protectedResourceMetadata?.authorizationServers?.[0];

  // listRemoteSessionIssuers returns both this project's issuers and inherited
  // organization-level ones (project_id IS NULL, same org), so the selectable
  // list spans organizational and project-scoped providers.
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
  const [sheetInitialUrl, setSheetInitialUrl] = useState<string | undefined>();

  const openSheet = (initialIssuerUrl?: string) => {
    setSheetInitialUrl(initialIssuerUrl);
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
        onUseDiscovered={() => openSheet(authorizationServer)}
        onStartManual={() => openSheet(undefined)}
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
          isLoading={isLoadingIssuers || isLoadingClients}
          onAdd={() => openSheet(undefined)}
          onEdit={handleEdit}
          onDelete={handleDelete}
        />
      </>
    );
  }

  return (
    <>
      <SettingsSection id={MCP_AUTHENTICATION_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Authentication</SettingsSection.Title>
          <SettingsSection.Description>
            Configure the upstream identity provider and user session settings
            for clients connecting to this server.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <FieldGroup className="gap-6">{authenticationFields}</FieldGroup>
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Authentication changes apply to new client connections.
            </SettingsSection.FooterHint>
          </SettingsSection.Footer>
        </SettingsSection.Panel>

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
      </SettingsSection>
      <McpServerSessionsPanel mcpServer={mcpServer} />
    </>
  );
}

function IdentityProviderSetupField({
  probeStatus,
  hasDiscoveredAuthorizationServer,
  onUseDiscovered,
  onStartManual,
}: {
  probeStatus: ProtectedResourceProbeStatus;
  hasDiscoveredAuthorizationServer: boolean;
  onUseDiscovered: () => void;
  onStartManual: () => void;
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
      <Type muted small>
        Loading authentication configuration...
      </Type>
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
