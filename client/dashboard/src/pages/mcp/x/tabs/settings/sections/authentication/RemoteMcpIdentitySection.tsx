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
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
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
import { toast } from "sonner";
import { SettingsSection } from "@/components/detail/settings-section";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import type { AuthTarget } from "./authTarget";
import {
  deriveRemoteMcpIdentityMode,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
  type RemoteMcpIdentityMode,
} from "./remoteMcpIdentity";

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
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canWrite = !rbacLoading && hasScope("mcp:write", target.resourceId);
  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );
  const {
    items: clients,
    isLoading: clientsLoading,
    isError: clientsError,
    error: clientsQueryError,
  } = useAllRemoteSessionClients(
    { userSessionIssuerId: target.userSessionIssuerId ?? undefined },
    { enabled: !!target.userSessionIssuerId },
  );
  const { data: issuersResult } = useRemoteSessionIssuers();
  const siblingsQuery = useMcpServers({ remoteMcpServerId }, undefined, {
    enabled: remoteMcpServerId !== "",
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

  useEffect(() => {
    if (actualMode) setSelectedMode(actualMode);
  }, [actualMode]);

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
      mode === "user" ||
      actualMode === "user" ||
      identityReadOnly ||
      passThroughAuthorization
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
    if (!authorizationHeader || !canWrite || rbacLoading) return;
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

  const providerNames = (issuersResult?.result.items ?? [])
    .filter((issuer) =>
      clients.some((client) => client.remoteSessionIssuerId === issuer.id),
    )
    .map((issuer) => issuer.name?.trim() || issuer.issuer);
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
                credential used by the others. Manage it from the backing Remote
                MCP source when source management is available.
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
                        disabled={actualMode !== "user"}
                        title="User"
                      >
                        {actualMode === "user"
                          ? "Each user authorizes access with their own account."
                          : "Requires provider setup that is not available here yet."}
                      </RadioCard>
                      <RadioCard
                        value="agent"
                        disabled={!!passThroughAuthorization}
                        title="Agent"
                      >
                        Every user shares one static Authorization credential.
                      </RadioCard>
                      <RadioCard value="none" title="None">
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
                providerNames={providerNames}
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
              <Alert variant="info" dismissible={false}>
                {passThroughAuthorization
                  ? "No static identity is configured, but the legacy pass-through Authorization header can still send a credential upstream."
                  : "Requests to the upstream server will not include an Authorization credential."}
              </Alert>
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
              disabled={saving || !canWrite || rbacLoading}
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
    </>
  );
}

function UserIdentityDetails({
  configured,
  providerNames,
}: {
  configured: boolean;
  providerNames: string[];
}): JSX.Element {
  if (!configured) {
    return (
      <Alert variant="info" dismissible={false}>
        User Identity setup is read-only in this release. Existing linked
        providers can be viewed here, but a provider cannot be registered from
        this page yet.
      </Alert>
    );
  }

  return (
    <div className="border p-4">
      <div className="mb-2 flex items-center justify-between gap-3">
        <Text className="font-medium">Linked identity provider</Text>
        <Badge variant="information">
          <Badge.Text>User Identity</Badge.Text>
        </Badge>
      </div>
      <Text muted small>
        {providerNames.length > 0
          ? providerNames.join(", ")
          : "A remote identity provider is linked to this server."}
      </Text>
      <Text muted small className="mt-2">
        Provider editing will be added separately. This configuration is
        currently read-only.
      </Text>
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
