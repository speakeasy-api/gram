import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { AgentIdentityRow } from "./AgentIdentityRow";
import { useAgentCredentialDraft } from "./useAgentCredentialDraft";
import { AuthRow } from "./AuthRow";
import type { AuthTarget } from "./authTarget";
import {
  deriveRemoteMcpIdentityMode,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
  type RemoteMcpIdentityMode,
} from "./remoteMcpIdentity";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import { useRemoteMcpAuthenticationProbe } from "./useRemoteMcpAuthenticationProbe";
import { UserIdentityRow } from "./UserIdentityRow";
import { useUserIdentityDraft } from "./useUserIdentityDraft";

/**
 * The three identity modes, in the order AIM-230 fixes. Descriptions name the
 * upstream service rather than talking about "the upstream", so the choice
 * reads as a decision about Linear (or whatever this server fronts) rather
 * than about Speakeasy's plumbing.
 */
function identityCards(upstreamName: string) {
  return [
    {
      value: "user" as const,
      title: "User Identity",
      description: `Each user signs in to ${upstreamName} as themselves and keeps their own permissions.`,
    },
    {
      value: "agent" as const,
      title: "Agent Identity",
      description:
        "Every caller acts as one service account. Manage what it may do in the control plane.",
    },
    {
      value: "none" as const,
      title: "No Identity",
      description:
        "Speakeasy will manage no identity and users will manage their own static headers.",
    },
  ];
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
  const sourceQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    {
      enabled: remoteMcpServerId !== "",
    },
  );
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

  // The upstream this server fronts, by name — the identity copy is written
  // about the service, not about "the upstream server".
  const upstreamName =
    linkedServers[0]?.name?.trim() ||
    sourceQuery.data?.slug ||
    "the upstream service";

  const userDraft = useUserIdentityDraft({
    mcpServerId: target.resourceId,
    remoteMcpServerId,
    upstreamUrl: sourceQuery.data?.url,
    issuers: issuersResult?.result.items ?? [],
    linkedClients: clients,
    configured: actualMode === "user",
    enabled: identityResolved && selectedMode === "user",
  });
  const agentDraft = useAgentCredentialDraft({
    remoteMcpServerId,
    authorizationHeader,
    onSaved: invalidateHeaders,
    createHeader,
    updateHeader,
  });

  const handleModeChange = (next: string) => {
    const mode = next as RemoteMcpIdentityMode;
    if (actualMode === "user" || identityReadOnly || passThroughAuthorization) {
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

  const identityError = headersQuery.error ?? clientsQueryError;
  const cards = identityCards(upstreamName);

  // One Save for the whole section. What it commits depends on the selected
  // mode, and it disappears into a disabled state when there is nothing to do
  // rather than sprouting a button per sub-form.
  const canSave =
    selectedMode === "user"
      ? userDraft.canSave
      : selectedMode === "agent"
        ? agentDraft.canSave
        : false;
  const savePending =
    selectedMode === "user" ? userDraft.saving : agentDraft.saving || saving;

  let footerHint: string;
  if (selectedMode === "user") {
    switch (userDraft.status.kind) {
      case "pending":
        footerHint = "Registering…";
        break;
      case "done":
        footerHint = `Saved. New connections sign users in through ${upstreamName}.`;
        break;
      case "refused":
        footerHint = "Registration was refused. Choose a way forward above.";
        break;
      case "unreachable":
        footerHint =
          "Registration could not reach the provider. Save to retry.";
        break;
      case "idle":
        footerHint = "Save registers this server with the provider.";
        break;
    }
  } else if (selectedMode === "agent") {
    footerHint = agentDraft.canSave
      ? "Unsaved changes. New connections pick them up after save."
      : "Saved. New connections use this identity.";
  } else {
    footerHint = "No credential is sent upstream.";
  }

  return (
    <>
      <SettingsSection.Panel>
        <div className="divide-y">
          <div className="space-y-4 px-6 py-5">
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

            {passThroughAuthorization ? (
              <Alert variant="warning" dismissible={false}>
                A legacy pass-through Authorization header is still configured.
                Remove it in Advanced before selecting Agent Identity or relying
                on No Identity.
              </Alert>
            ) : null}

            {identityQueryError ? (
              <Text muted small>
                Identity is unavailable.
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
                    {cards.map((card) => (
                      <RadioCard
                        key={card.value}
                        value={card.value}
                        disabled={
                          (card.value === "user" && actualMode === "agent") ||
                          (card.value === "agent" && !!passThroughAuthorization)
                        }
                        title={card.title}
                      >
                        {card.description}
                      </RadioCard>
                    ))}
                  </RadioCardGroup>
                )}
              </RequireScope>
            )}
          </div>

          {identityResolved && selectedMode === "user" ? (
            <AuthRow
              label="Identity provider"
              hint={
                <>
                  Where users sign in. Speakeasy registers this server with it
                  for you.
                  <Link
                    to={orgRoutes.remoteIdentityProviders.href()}
                    className="text-muted-foreground hover:text-foreground mt-2 flex w-fit items-center gap-1 underline underline-offset-2"
                  >
                    Manage identity providers
                    <ArrowUpRight aria-hidden="true" className="size-3.5" />
                  </Link>
                </>
              }
            >
              <UserIdentityRow
                draft={userDraft}
                disabled={identityReadOnly}
                manageHref={orgRoutes.remoteIdentityProviders.href()}
                createHref={orgRoutes.remoteIdentityProviders.href()}
                inspectHref={mcpServerTabHref(routes, target.slug, "inspect")}
                onSwitchToAgent={() => setSelectedMode("agent")}
              />
            </AuthRow>
          ) : null}

          {identityResolved && selectedMode === "agent" ? (
            <AuthRow
              label="Agent credential"
              hint={`One credential every caller shares. Speakeasy sends it to ${upstreamName} as the Authorization header.`}
            >
              <AgentIdentityRow
                draft={agentDraft}
                disabled={identityReadOnly || !!passThroughAuthorization}
                upstreamName={upstreamName}
              />
            </AuthRow>
          ) : null}

          {identityResolved && selectedMode === "none" ? (
            <AuthRow
              label="No identity"
              hint="Speakeasy sends no Authorization credential. Configure pass-through or static headers under Advanced."
            >
              <NoIdentityNotice
                passThroughAuthorization={!!passThroughAuthorization}
                probeStatus={noneProbeStatus}
              />
            </AuthRow>
          ) : null}
        </div>

        {identityResolved && selectedMode !== "none" ? (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              {footerHint}
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <RequireScope
                scope="mcp:write"
                resourceId={target.resourceId}
                level="component"
              >
                <FooterSaveButton
                  pending={savePending}
                  disabled={!canSave || savePending || identityReadOnly}
                  onClick={() => {
                    if (selectedMode === "user") userDraft.save();
                    else void agentDraft.save();
                  }}
                />
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        ) : null}
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
    <Text muted small>
      Requests to the upstream server will not include an Authorization
      credential.
    </Text>
  );
}
