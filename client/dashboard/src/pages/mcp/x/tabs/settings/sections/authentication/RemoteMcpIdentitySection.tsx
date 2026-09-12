import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import { IssuerLink } from "@/pages/remote-identity-providers/IssuerLink";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { SettingsSection } from "@/components/detail/settings-section";
import { ExplainerDialog } from "./AuthRow";
import { ConfigureRemoteMcpUserIdentitySheet } from "./ConfigureRemoteMcpUserIdentitySheet";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import type { AuthTarget } from "./authTarget";
import {
  deriveRemoteMcpIdentityMode,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
  type RemoteMcpIdentityMode,
} from "./remoteMcpIdentity";
import { useRemoteMcpAuthenticationProbe } from "./useRemoteMcpAuthenticationProbe";

const REDACTED_SECRET = "***";

type AgentCredentialType = "bearer" | "basic" | "manual" | "client-credentials";

function credentialTypeFromHeader(
  header: RemoteMcpServerHeader | undefined,
): AgentCredentialType {
  const value = header?.value ?? "";
  if (value.startsWith("Bearer ")) return "bearer";
  if (value.startsWith("Basic ")) return "basic";
  return "manual";
}

function encodeBasicCredential(username: string, password: string): string {
  const bytes = new TextEncoder().encode(`${username}:${password}`);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

export function RemoteMcpIdentitySectionBody({
  target,
}: {
  target: AuthTarget;
}): JSX.Element {
  const remoteMcpServerId = target.remoteMcpServerId ?? "";
  const queryClient = useQueryClient();
  const orgRoutes = useOrgRoutes();
  const routes = useRoutes();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canWrite = !rbacLoading && hasScope("mcp:write", target.resourceId);
  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const {
    items: clients,
    isLoading: clientsLoading,
    isError: clientsError,
    error: clientsQueryError,
  } = useAllRemoteSessionClients(
    { userSessionIssuerId: target.userSessionIssuerId ?? undefined },
    { enabled: !!target.userSessionIssuerId, throwOnError: false },
  );
  const issuersQuery = useRemoteSessionIssuers(undefined, undefined, {
    throwOnError: false,
  });
  const siblingsQuery = useMcpServers({ remoteMcpServerId }, undefined, {
    enabled: remoteMcpServerId !== "",
    throwOnError: false,
  });
  const linkedServers = (siblingsQuery.data?.mcpServers ?? []).filter(
    (server) => server.remoteMcpServerId === remoteMcpServerId,
  );
  const sharedSource = linkedServers.length > 1;
  const identityReadOnly =
    sharedSource ||
    siblingsQuery.isLoading ||
    siblingsQuery.isError ||
    !canWrite;
  const headers = headersQuery.data?.headers ?? [];
  const authorizationHeader = findStaticAuthorizationHeader(headers);
  const passThroughAuthorization = findPassThroughAuthorizationHeader(headers);
  const identityQueryError = headersQuery.isError || clientsError;
  const actualMode = identityQueryError
    ? null
    : deriveRemoteMcpIdentityMode(clients.length, headers);
  const loading =
    headersQuery.isLoading || clientsLoading || siblingsQuery.isLoading;
  const identityResolved = !loading && !identityQueryError;
  const [selectedMode, setSelectedMode] =
    useState<RemoteMcpIdentityMode>("none");
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [userIdentitySheetOpen, setUserIdentitySheetOpen] = useState(false);

  useEffect(() => {
    if (actualMode) setSelectedMode(actualMode);
  }, [actualMode]);

  const noneProbeStatus = useRemoteMcpAuthenticationProbe(
    remoteMcpServerId,
    identityResolved &&
      actualMode === "none" &&
      selectedMode === "none" &&
      !passThroughAuthorization,
  );
  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();
  const saving =
    createHeader.isPending || updateHeader.isPending || deleteHeader.isPending;

  const invalidateHeaders = async () => {
    await invalidateAllRemoteMcpServerHeaders(queryClient, {
      refetchType: "all",
    });
    const refreshed = await headersQuery.refetch();
    return !refreshed.isError && !!refreshed.data;
  };

  const handleModeChange = (next: string) => {
    const mode = next as RemoteMcpIdentityMode;
    if (
      actualMode === "user" ||
      identityReadOnly ||
      (passThroughAuthorization && mode !== "user")
    ) {
      return;
    }
    if (actualMode === "agent" && mode === "none") {
      setRemoveDialogOpen(true);
      return;
    }
    setSelectedMode(mode);
  };

  const removeAgentCredential = async () => {
    if (!authorizationHeader || !canWrite || rbacLoading || identityReadOnly) {
      return;
    }
    try {
      await deleteHeader.mutateAsync({
        request: { id: authorizationHeader.id },
      });
      const refreshed = await invalidateHeaders();
      if (!refreshed) {
        toast.warning(
          "Credential removed, but headers could not be refreshed.",
        );
      }
      setRemoveDialogOpen(false);
      setSelectedMode("none");
      toast.success("Agent Identity removed");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to remove Agent Identity",
      );
    }
  };

  const allIssuers = issuersQuery.data?.result.items ?? [];
  const associatedIssuerIds = new Set(
    clients.map((client) => client.remoteSessionIssuerId),
  );
  const associatedIssuers = allIssuers.filter((issuer) =>
    associatedIssuerIds.has(issuer.id),
  );
  const identityError = headersQuery.error ?? clientsQueryError;

  return (
    <>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Stack gap={5}>
            {sharedSource ? (
              <Alert variant="warning" dismissible={false}>
                This identity is shared by {linkedServers.length} MCP servers.
                Editing is disabled here so one server cannot change the
                credential used by the others. Manage linked providers and
                clients in{" "}
                <Link
                  className="font-medium underline underline-offset-2"
                  to={orgRoutes.remoteIdentityProviders.href()}
                >
                  Remote Identity Providers
                </Link>
                .
              </Alert>
            ) : null}

            {siblingsQuery.isError ? (
              <Alert variant="error" dismissible={false}>
                Could not verify whether this Remote MCP source is shared.
                Identity editing is disabled.
              </Alert>
            ) : null}

            {identityQueryError ? (
              <Alert variant="error" dismissible={false}>
                Could not determine the current identity configuration
                {identityError?.message ? `: ${identityError.message}` : "."}
              </Alert>
            ) : null}

            {issuersQuery.isError ? (
              <Alert variant="error" dismissible={false}>
                Remote identity providers could not be loaded. User Identity
                configuration is unavailable.
              </Alert>
            ) : null}

            {passThroughAuthorization ? (
              <Alert variant="warning" dismissible={false}>
                A legacy pass-through Authorization header is still configured.
                Remove it in Advanced Headers before selecting Agent Identity or
                relying on No Identity.
              </Alert>
            ) : null}

            <div>
              <Text variant="subheading" className="mb-1">
                Identity mode
              </Text>
              <Text muted small className="mb-4 max-w-3xl">
                User Identity connects each person with their own upstream
                account. Agent Identity sends one static credential for every
                request. No Identity has no static credential.
              </Text>
              <ExplainerDialog title="User Identity and Agent Identity">
                <Text muted small className="block">
                  User Identity asks each person to authorize with the upstream
                  provider. Their access tokens remain separate, so upstream
                  permissions and audit trails continue to identify that user.
                </Text>
                <Text muted small className="block">
                  Agent Identity sends one shared static Authorization
                  credential on every request. Use it only when the upstream
                  account is intentionally shared and does not need per-user
                  attribution.
                </Text>
              </ExplainerDialog>
              {identityQueryError ? (
                <Text muted small>
                  Identity mode is unavailable.
                </Text>
              ) : loading ? (
                <Text muted small>
                  Loading identity…
                </Text>
              ) : (
                <RequireScope
                  scope="mcp:write"
                  resourceId={target.resourceId}
                  level="component"
                  className="w-full"
                >
                  {({ disabled: scopeDisabled }) => (
                    <RadioCardGroup
                      orientation="horizontal"
                      value={selectedMode}
                      disabled={
                        identityReadOnly ||
                        scopeDisabled ||
                        actualMode === "user" ||
                        saving
                      }
                      onValueChange={handleModeChange}
                      className="grid-flow-row grid-cols-1 md:grid-flow-col md:grid-cols-none"
                    >
                      <RadioCard
                        value="user"
                        disabled={actualMode === "agent"}
                        title="User"
                      >
                        Each user authorizes access with their own account.
                      </RadioCard>
                      <RadioCard
                        value="agent"
                        disabled={!!passThroughAuthorization}
                        title="Agent"
                      >
                        Every user shares one static Authorization credential.
                      </RadioCard>
                      <RadioCard
                        value="none"
                        disabled={!!passThroughAuthorization}
                        title="None"
                      >
                        Connect without a static upstream identity.
                      </RadioCard>
                    </RadioCardGroup>
                  )}
                </RequireScope>
              )}
            </div>

            {identityResolved && selectedMode === "user" ? (
              <UserIdentityDetails
                configured={actualMode === "user"}
                issuers={associatedIssuers}
                clients={clients}
                isLoading={issuersQuery.isLoading}
                isError={issuersQuery.isError}
                disabled={identityReadOnly || issuersQuery.isError}
                onConfigure={() => setUserIdentitySheetOpen(true)}
                manageHref={orgRoutes.remoteIdentityProviders.href()}
                inspectHref={routes.mcp.x.inspect.href(target.resourceId)}
                clientHref={(issuerId, clientId) =>
                  orgRoutes.remoteIdentityProviders.clientDetail.href(
                    issuerId,
                    clientId,
                  )
                }
              />
            ) : null}
            {identityResolved && selectedMode === "agent" ? (
              <AgentIdentityForm
                remoteMcpServerId={remoteMcpServerId}
                authorizationHeader={authorizationHeader}
                disabled={identityReadOnly || !!passThroughAuthorization}
                resourceId={target.resourceId}
                onSaved={invalidateHeaders}
                createHeader={createHeader}
                updateHeader={updateHeader}
              />
            ) : null}
            {identityResolved && selectedMode === "none" ? (
              <NoIdentityNotice
                passThroughAuthorization={!!passThroughAuthorization}
                probeStatus={noneProbeStatus}
              />
            ) : null}
          </Stack>
        </SettingsSection.Body>
      </SettingsSection.Panel>

      <Dialog open={removeDialogOpen} onOpenChange={setRemoveDialogOpen}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Switch to No Identity?</Dialog.Title>
            <Dialog.Description>
              This removes the static Authorization credential from the Remote
              MCP source. Requests will no longer authenticate upstream.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={saving}
              onClick={() => setRemoveDialogOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={saving || identityReadOnly}
              onClick={() => void removeAgentCredential()}
            >
              {saving ? (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {saving ? "Removing" : "Remove credential"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>

      <ConfigureRemoteMcpUserIdentitySheet
        open={userIdentitySheetOpen}
        onOpenChange={setUserIdentitySheetOpen}
        target={target}
        issuers={allIssuers}
        initialProviderId={clients[0]?.remoteSessionIssuerId}
      />
    </>
  );
}

function NoIdentityNotice({
  passThroughAuthorization,
  probeStatus,
}: {
  passThroughAuthorization: boolean;
  probeStatus: ReturnType<typeof useRemoteMcpAuthenticationProbe>;
}): JSX.Element {
  if (passThroughAuthorization) {
    return (
      <Alert variant="info" dismissible={false}>
        No static identity is configured, but the legacy pass-through
        Authorization header can still send a credential upstream.
      </Alert>
    );
  }
  if (probeStatus === "authentication-required") {
    return (
      <Alert variant="warning" dismissible={false}>
        The upstream server reported that authentication is required. No
        Identity sends no Authorization credential, so requests may fail until
        User or Agent Identity is configured.
      </Alert>
    );
  }
  return (
    <Alert variant="info" dismissible={false}>
      Requests to the upstream server will not include an Authorization
      credential.
    </Alert>
  );
}

function UserIdentityDetails({
  configured,
  issuers,
  clients,
  isLoading,
  isError,
  disabled,
  onConfigure,
  manageHref,
  inspectHref,
  clientHref,
}: {
  configured: boolean;
  issuers: RemoteSessionIssuer[];
  clients: RemoteSessionClient[];
  isLoading: boolean;
  isError: boolean;
  disabled: boolean;
  onConfigure: () => void;
  manageHref: string;
  inspectHref: string;
  clientHref: (issuerId: string, clientId: string) => string;
}): JSX.Element {
  if (!configured) {
    return (
      <div className="border p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <Text className="font-medium">Remote Identity Provider</Text>
            <Text muted small className="mt-1">
              Choose an existing provider and configure its OAuth client for
              per-user upstream authorization.
            </Text>
          </div>
          <Button
            variant="secondary"
            disabled={disabled || isLoading}
            onClick={onConfigure}
          >
            <Button.Text>{isLoading ? "Checking…" : "Configure"}</Button.Text>
          </Button>
        </div>
        <Text muted small className="mt-3">
          Create and manage providers from{" "}
          <Link className="text-primary hover:underline" to={manageHref}>
            Remote Identity Providers
          </Link>
          .
        </Text>
      </div>
    );
  }

  return (
    <div className="border p-4">
      <div className="mb-2 flex items-center justify-between gap-3">
        <Text className="font-medium">Linked identity provider</Text>
        <div className="flex items-center gap-2">
          <Badge variant="information">
            <Badge.Text>User Identity</Badge.Text>
          </Badge>
          <Button
            variant="secondary"
            size="sm"
            disabled={disabled || isLoading}
            onClick={onConfigure}
          >
            <Button.Text>Change</Button.Text>
          </Button>
        </div>
      </div>
      <Stack gap={3}>
        {issuers.map((issuer) => {
          const issuerClients = clients.filter(
            (client) => client.remoteSessionIssuerId === issuer.id,
          );
          return (
            <div key={issuer.id} className="border p-3">
              <Text small className="font-medium">
                <IssuerLink issuer={issuer} />
              </Text>
              <Text muted mono variant="small" className="break-all">
                {issuer.issuer}
              </Text>
              {issuerClients.map((client) => (
                <Text key={client.id} muted small className="mt-2 block">
                  Client:{" "}
                  <Link
                    className="text-primary hover:underline"
                    to={clientHref(issuer.id, client.id)}
                  >
                    {remoteSessionClientDisplayName(client)}
                  </Link>
                  {` · ${client.userSessionIssuerIds.length} connection${client.userSessionIssuerIds.length === 1 ? "" : "s"}`}
                </Text>
              ))}
            </div>
          );
        })}
        {isError ? (
          <Text small className="text-destructive">
            Remote identity providers could not be loaded.
          </Text>
        ) : isLoading ? (
          <Text muted small>
            Loading identity provider…
          </Text>
        ) : issuers.length === 0 ? (
          <Text muted small>
            A remote identity provider is linked to this server.
          </Text>
        ) : null}
        <Text muted small>
          Try it: Connect on the{" "}
          <Link className="text-primary hover:underline" to={inspectHref}>
            Inspect tab
          </Link>
          . Manage this configuration in{" "}
          <Link className="text-primary hover:underline" to={manageHref}>
            Remote Identity Providers
          </Link>
          .
        </Text>
      </Stack>
    </div>
  );
}

function AgentIdentityForm({
  remoteMcpServerId,
  authorizationHeader,
  disabled,
  resourceId,
  onSaved,
  createHeader,
  updateHeader,
}: {
  remoteMcpServerId: string;
  authorizationHeader: RemoteMcpServerHeader | undefined;
  disabled: boolean;
  resourceId: string;
  onSaved: () => Promise<boolean>;
  createHeader: ReturnType<typeof useCreateRemoteMcpServerHeaderMutation>;
  updateHeader: ReturnType<typeof useUpdateRemoteMcpServerHeaderMutation>;
}): JSX.Element {
  const bearerTokenId = useId();
  const basicUsernameId = useId();
  const basicPasswordId = useId();
  const manualValueId = useId();
  const [credentialType, setCredentialType] = useState<AgentCredentialType>(
    () => credentialTypeFromHeader(authorizationHeader),
  );
  const initialValue = authorizationHeader?.value ?? "";
  const [bearerToken, setBearerToken] = useState(
    initialValue.startsWith("Bearer ") ? initialValue.slice(7) : "",
  );
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [manualValue, setManualValue] = useState(
    credentialTypeFromHeader(authorizationHeader) === "manual"
      ? initialValue
      : "",
  );

  let authorizationValue = "";
  if (credentialType === "bearer" && bearerToken) {
    authorizationValue = `Bearer ${bearerToken}`;
  } else if (credentialType === "basic" && username && password) {
    authorizationValue = `Basic ${encodeBasicCredential(username, password)}`;
  } else if (credentialType === "manual") {
    authorizationValue = manualValue;
  }

  const saving = createHeader.isPending || updateHeader.isPending;
  const canSave =
    !disabled &&
    !saving &&
    credentialType !== "client-credentials" &&
    authorizationValue.trim() !== "";

  const handleSave = async () => {
    if (!canSave) return;
    try {
      if (authorizationHeader) {
        const preserveRedacted = authorizationValue === REDACTED_SECRET;
        await updateHeader.mutateAsync({
          request: {
            updateServerHeaderForm: {
              id: authorizationHeader.id,
              name: "Authorization",
              isRequired: true,
              isSecret: true,
              value: preserveRedacted ? undefined : authorizationValue,
            },
          },
        });
      } else {
        await createHeader.mutateAsync({
          request: {
            createServerHeaderForm: {
              remoteMcpServerId,
              name: "Authorization",
              isRequired: true,
              isSecret: true,
              value: authorizationValue,
            },
          },
        });
      }
      const refreshed = await onSaved();
      if (!refreshed) {
        toast.warning("Credential saved, but headers could not be refreshed.");
        return;
      }
      setBearerToken("");
      setUsername("");
      setPassword("");
      setManualValue("");
      toast.success("Agent Identity updated");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update Agent Identity",
      );
    }
  };

  return (
    <div className="border p-4">
      <Text className="mb-1 font-medium">Authorization credential</Text>
      <Text muted small className="mb-4">
        This credential is stored as a secret on the backing Remote MCP source.
      </Text>
      <RadioCardGroup
        orientation="horizontal"
        value={credentialType}
        disabled={disabled || saving}
        onValueChange={(value) =>
          setCredentialType(value as AgentCredentialType)
        }
        className="mb-4 grid-flow-row grid-cols-1 md:grid-flow-col md:grid-cols-none"
      >
        <RadioCard value="bearer" title="Bearer" />
        <RadioCard value="basic" title="Basic" />
        <RadioCard value="manual" title="Manual" />
        <RadioCard
          value="client-credentials"
          disabled
          title={
            <span className="flex items-center gap-2">
              Client Credentials
              <Badge variant="neutral" size="sm">
                <Badge.Text>Coming soon</Badge.Text>
              </Badge>
            </span>
          }
        />
      </RadioCardGroup>

      {credentialType === "bearer" ? (
        <div>
          <label htmlFor={bearerTokenId} className="mb-1 block text-sm">
            Bearer token
          </label>
          <Input
            id={bearerTokenId}
            value={bearerToken}
            onChange={setBearerToken}
            placeholder="Token"
            type="password"
            disabled={disabled}
          />
        </div>
      ) : null}
      {credentialType === "basic" ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label htmlFor={basicUsernameId} className="mb-1 block text-sm">
              Basic username
            </label>
            <Input
              id={basicUsernameId}
              value={username}
              onChange={setUsername}
              placeholder="Username"
              disabled={disabled}
            />
          </div>
          <div>
            <label htmlFor={basicPasswordId} className="mb-1 block text-sm">
              Basic password
            </label>
            <Input
              id={basicPasswordId}
              value={password}
              onChange={setPassword}
              placeholder="Password"
              type="password"
              disabled={disabled}
            />
          </div>
        </div>
      ) : null}
      {credentialType === "manual" ? (
        <div>
          <label htmlFor={manualValueId} className="mb-1 block text-sm">
            Authorization value
          </label>
          <Input
            id={manualValueId}
            value={manualValue}
            onChange={setManualValue}
            placeholder="Custom Authorization value"
            type="password"
            disabled={disabled}
          />
        </div>
      ) : null}

      <div
        className="bg-muted mt-4 border p-3"
        role="status"
        aria-label="Authorization preview"
      >
        <Text muted small className="mb-1">
          Authorization preview
        </Text>
        <code className="block truncate text-sm">
          {credentialType === "bearer"
            ? "Authorization: Bearer [redacted]"
            : credentialType === "basic"
              ? "Authorization: Basic [redacted]"
              : "Authorization: [redacted]"}
        </code>
      </div>

      <RequireScope scope="mcp:write" resourceId={resourceId} level="component">
        <Button
          variant="primary"
          className="mt-4"
          disabled={!canSave}
          onClick={() => void handleSave()}
        >
          {saving ? (
            <Button.LeftIcon>
              <Loader2 aria-hidden="true" className="size-4 animate-spin" />
            </Button.LeftIcon>
          ) : null}
          <Button.Text>{saving ? "Saving" : "Save credential"}</Button.Text>
        </Button>
      </RequireScope>
    </div>
  );
}
