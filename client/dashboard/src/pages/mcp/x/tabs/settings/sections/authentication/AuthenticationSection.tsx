import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllMcpServerAuthentication,
  useMcpServerAuthentication,
} from "@gram/client/react-query/mcpServerAuthentication.js";
import { useSetMcpServerUpstreamAuthRequirementMutation } from "@gram/client/react-query/setMcpServerUpstreamAuthRequirement.js";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import type { ServerClientAssociation } from "@gram/client/models/components/serverclientassociation.js";
import { useServerClientAssociations } from "@gram/client/react-query/serverClientAssociations.js";
import { Badge } from "@/components/ui/Badge";
import { GatewayAuthorizationServersField } from "@/pages/mcp/gateway/GatewayMemberSignInField";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Skeleton } from "@/components/ui/Skeleton";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
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
import { SharedAuthorizationServerField } from "./SharedAuthorizationServerField";
import { UserSessionIssuerField } from "./UserSessionIssuerField";
import { useSelectableUserSessionIssuers } from "./useSelectableUserSessionIssuers";
import { UserIdentitySessionControls } from "./UserIdentitySessionControls";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { SetupStepBanner } from "./SetupStepBanner";
import { UpstreamCredentialsSection } from "./UpstreamCredentialsSection";

export const MCP_AUTHENTICATION_SECTION_ID = "authentication";

function authenticationSectionDescription(
  isUnproxied: boolean,
  isRemoteMcp: boolean,
  upstreamName: string,
): string {
  if (isUnproxied) {
    return "Speakeasy doesn't manage authentication for unproxied servers.";
  }
  if (isRemoteMcp) {
    return `How callers are identified to ${upstreamName}. Changes take effect on new connections.`;
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
  mcpUrl,
}: {
  mcpServer: McpServer;
  /** The URL clients connect to; undefined while it is unknown. */
  mcpUrl?: string;
}): JSX.Element {
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  const isRemoteMcp = !!mcpServer.remoteMcpServerId;
  const target = useMcpServerAuthTarget(mcpServer, mcpUrl);

  if (isRemoteMcp && !isUnproxied) {
    return (
      <div id={MCP_AUTHENTICATION_SECTION_ID}>
        <SetupStepBanner mcpServer={mcpServer} />
        <UpstreamCredentialsSection key={mcpServer.id} mcpServer={mcpServer} />
      </div>
    );
  }

  return (
    <SettingsSection id={MCP_AUTHENTICATION_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>
          {isRemoteMcp ? "Identity" : "Authentication"}
        </SettingsSection.Title>
        <SettingsSection.Description>
          {authenticationSectionDescription(
            isUnproxied,
            isRemoteMcp,
            mcpServer.name?.trim() || "the upstream service",
          )}
        </SettingsSection.Description>
      </SettingsSection.Header>
      {isUnproxied ? null : <SetupStepBanner mcpServer={mcpServer} />}
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
  // Remote targets configure upstream through the server-scoped typed panel.
  if (target.kind === "remote-mcp") return <></>;
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
  const effectiveIssuersQuery = useSelectableUserSessionIssuers(target);

  const {
    data: userSessionIssuer,
    isLoading: isLoadingUserSessionIssuer,
    isError: isUserSessionIssuerError,
  } = useUserSessionIssuer({ id: userSessionIssuerId }, undefined, {
    enabled: issuerConfigured,
  });

  const gatewayId = target.attachImpactTarget.metaMcpServerId;
  const serverId = target.mcpServerId ?? target.attachImpactTarget.toolsetId;
  const associations = useServerClientAssociations(
    { mcpServerId: serverId ?? "" },
    undefined,
    {
      enabled: !!serverId && (issuerConfigured || target.hosted === true),
      throwOnError: false,
    },
  );
  const attachedIssuerIds = useMemo(
    () => [
      ...new Set(
        associations.data?.associations.map(
          (item) => item.remoteSessionIssuerId,
        ) ?? [],
      ),
    ],
    [associations.data],
  );
  // Resolved by id, never by filtering the issuer listing: an attached issuer
  // can fall past any listing page, and filtering would then render a working
  // server as having no provider.
  const {
    items: associatedIssuers,
    isLoading: isLoadingAssociatedIssuers,
    isError: isAssociatedIssuersError,
  } = useRemoteSessionIssuersByIds(attachedIssuerIds, {
    enabled: issuerConfigured || target.hosted === true,
  });

  const [sheetOpen, setSheetOpen] = useState(false);

  // Keep targets mounted for one render after close so exit animations retain
  // the row that triggered them.
  type ProviderTarget = {
    issuer: RemoteSessionIssuer;
    association: ServerClientAssociation;
  };
  const [deleteTarget, setDeleteTarget] = useState<ProviderTarget | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [modifyTarget, setModifyTarget] = useState<ProviderTarget | null>(null);
  const [modifyOpen, setModifyOpen] = useState(false);

  const handleEdit = (
    issuer: RemoteSessionIssuer,
    association?: ServerClientAssociation,
  ) => {
    if (!association) return;
    setModifyTarget({ issuer, association });
    setModifyOpen(true);
  };

  const handleDelete = (
    issuer: RemoteSessionIssuer,
    association?: ServerClientAssociation,
  ) => {
    if (!association) return;
    setDeleteTarget({ issuer, association });
    setDeleteOpen(true);
  };

  const noUpstreamAction = target.hosted &&
    serverId &&
    !gatewayId &&
    associations.data?.associations.length === 0 && (
      <RequireScope scope="org:admin" level="section">
        <RequireScope
          scope="mcp:write"
          resourceId={serverId}
          projectId={target.projectId}
          level="section"
        >
          <HostedNoUpstreamAction
            key={serverId}
            serverId={serverId}
            invalidate={target.invalidate}
          />
        </RequireScope>
      </RequireScope>
    );

  const registrationField = (
    <RemoteIdentityProvidersField
      associatedIssuers={associatedIssuers}
      associations={associations.data?.associations}
      allowAdditionalProviders={false}
      projectId={target.projectId}
      isLoading={associations.isLoading || isLoadingAssociatedIssuers}
      isError={associations.isError || isAssociatedIssuersError}
      onAdd={() => setSheetOpen(true)}
      onEdit={handleEdit}
      onDelete={handleDelete}
      readOnly={
        userSessionIssuer?.projectId === "" &&
        (!target.hosted || associations.data?.associations.length !== 0)
      }
    />
  );

  let authenticationFields: ReactNode;
  if (!issuerConfigured && gatewayId) {
    authenticationFields = (
      <GatewayAuthorizationServersField metaMcpServerId={gatewayId} />
    );
  } else if (!issuerConfigured) {
    authenticationFields = (
      <>
        <IdentityProviderSetupField
          onStartManual={() => setSheetOpen(true)}
          additionalAction={additionalSetupAction}
        />
        {target.hosted &&
          (associations.data?.associations.length ?? 0) > 0 &&
          registrationField}
        {noUpstreamAction}
      </>
    );
  } else if (isLoadingUserSessionIssuer) {
    authenticationFields = <AuthenticationLoadingField />;
  } else if (isUserSessionIssuerError || !userSessionIssuer) {
    authenticationFields = <AuthenticationLoadErrorField />;
  } else {
    authenticationFields = (
      <>
        <SharedAuthorizationServerField
          userSessionIssuer={userSessionIssuer}
          mcpUrl={target.mcpUrl}
        />
        {gatewayId ? (
          <GatewayAuthorizationServersField metaMcpServerId={gatewayId} />
        ) : (
          registrationField
        )}
        {noUpstreamAction}
        {/* Last: its footer Save closes the panel. */}
        <UserIdentitySessionControls userSessionIssuer={userSessionIssuer} />
      </>
    );
  }

  return (
    <>
      {/* No footer hint: the section description above already says these
          changes take effect on new connections. */}
      <SettingsSection.Panel>
        <div className="divide-y">
          <UserSessionIssuerField
            target={target}
            issuers={effectiveIssuersQuery.issuers}
            isLoading={effectiveIssuersQuery.isLoading}
            isError={effectiveIssuersQuery.isError}
          />
          {authenticationFields}
        </div>
      </SettingsSection.Panel>

      {serverId && (
        <AttachRemoteIdentityProviderSheet
          open={sheetOpen}
          onOpenChange={setSheetOpen}
          target={target}
          userSessionIssuer={userSessionIssuer ?? null}
          excludedIssuerIds={attachedIssuerIds}
        />
      )}

      {deleteTarget && serverId && (
        <DeleteRemoteIdentityProviderDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          issuer={deleteTarget.issuer}
          mcpServerId={serverId}
          registrationId={deleteTarget.association.remoteSessionClientId}
          clientId={deleteTarget.association.clientId}
        />
      )}

      {modifyTarget && serverId && (
        <ModifyRemoteIdentityProviderSheet
          open={modifyOpen}
          onOpenChange={setModifyOpen}
          mcpServerId={serverId}
          issuer={modifyTarget.issuer}
          association={modifyTarget.association}
        />
      )}
    </>
  );
}

function IdentityProviderSetupField({
  onStartManual,
  additionalAction,
}: {
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
      <Skeleton label="Loading authentication" className="h-5 w-48" />
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

function HostedNoUpstreamAction({
  serverId,
  invalidate,
}: {
  serverId: string;
  invalidate: AuthTarget["invalidate"];
}): JSX.Element {
  // The legacy proxied setter is deprecated. This operation still owns the
  // exact hosted no-upstream decision; typed offer mutations cannot save it.
  const queryClient = useQueryClient();
  const authentication = useMcpServerAuthentication(
    { mcpServerId: serverId },
    undefined,
    { throwOnError: false },
  );
  const mutation = useSetMcpServerUpstreamAuthRequirementMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidate(queryClient),
        invalidateAllMcpServerAuthentication(queryClient),
      ]);
    },
  });
  const noUpstream =
    mutation.isSuccess || authentication.data?.status === "no_auth_required";
  return (
    <Field>
      <FieldLabel>Upstream authentication</FieldLabel>
      <FieldDescription>
        {noUpstream
          ? "No upstream authentication required. The server’s user sign-in settings are unchanged."
          : "Assign an authorization server above, or confirm that this server does not need upstream authentication. This does not turn off user sign-in."}
      </FieldDescription>
      {!noUpstream && (
        <Button
          type="button"
          variant="secondary"
          disabled={mutation.isPending || authentication.isLoading}
          onClick={() =>
            mutation.mutate({
              request: {
                setMcpServerUpstreamAuthRequirementRequestBody: {
                  mcpServerId: serverId,
                  requiresUpstreamAuth: false,
                },
              },
            })
          }
        >
          <Button.Text>No upstream authentication</Button.Text>
        </Button>
      )}
      {mutation.isError && (
        <FieldError>
          Could not save upstream authentication. Check that no registration is
          assigned to this server.
        </FieldError>
      )}
    </Field>
  );
}
