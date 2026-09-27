import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useDetachUserSessionIssuerMutation } from "@gram/client/react-query/detachUserSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpRight,
  ChevronDown,
  Loader2,
  TriangleAlert,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { HeadersSection } from "../HeadersSection";
import { useCatalogHeaderSuggestions } from "../useCatalogHeaderSuggestions";
import { useHeaderDrafts } from "@/lib/remote-identity";
import { AgentIdentityRow } from "@/lib/remote-identity";
import { identityModeCards } from "@/lib/remote-identity";
import { useAgentCredentialDraft } from "@/lib/remote-identity";
import { AuthRow } from "./AuthRow";
import type { AuthTarget } from "./authTarget";
import {
  deriveIdentityMode,
  managedAuthorizationHeader,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
  type IdentityMode,
} from "@/lib/remote-identity";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import { useUpstreamProbe } from "@/lib/remote-identity";
import { UserIdentityRow } from "@/lib/remote-identity";
import { useUserIdentityDraft } from "@/lib/remote-identity";

export function RemoteMcpIdentitySectionBody({
  target,
}: {
  target: AuthTarget;
}): JSX.Element {
  const remoteMcpServerId = target.remoteMcpServerId ?? "";
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canWrite =
    !rbacLoading && hasScope("mcp:write", target.permissionResourceId);
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
  // Other servers backed by the same remote source. Their headers are these
  // rows, and #6524 removed the remote's own page, so this is the only place
  // to edit them: the change is named rather than locked.
  const siblingMcpServers = linkedServers.filter(
    (server) => server.id !== target.permissionResourceId,
  );
  // Identity stays locked on a shared source — one server must not repoint the
  // binding for its siblings — but headers do not inherit that lock.
  const headersReadOnly =
    siblingsQuery.isLoading || siblingsQuery.isError || !canWrite;
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
    : deriveIdentityMode(clients.length, headers);
  const loading =
    headersQuery.isLoading || clientsLoading || siblingsQuery.isLoading;
  const identityResolved = !loading && !identityQueryError;
  const [selectedMode, setSelectedMode] = useState<IdentityMode>("none");
  // Raised by Save when it would remove an existing identity, which is the
  // "after confirmation" AIM-230 asks for.
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    if (actualMode) setSelectedMode(actualMode);
  }, [actualMode]);

  // The header rows are part of this panel's one commit, so their state lives
  // here rather than inside the disclosure that renders them.
  const headerSuggestions = useCatalogHeaderSuggestions(
    remoteMcpServerId,
    !!headersQuery.data && headers.length === 0 && !identityReadOnly,
  );
  const headerDrafts = useHeaderDrafts({
    remoteMcpServerId,
    identity: {
      mode: selectedMode,
      managed: managedAuthorizationHeader(selectedMode, headers),
      isError: !!identityQueryError,
    },
    readOnly: headersReadOnly,
    suggestions: headerSuggestions,
  });

  const noneProbeStatus = useUpstreamProbe(
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
    mcpServerId: target.permissionResourceId,
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

  const detachIssuer = useDetachUserSessionIssuerMutation();

  // Picking a card only changes the draft. Nothing is written, removed or
  // confirmed until Save — the panel has one commit point and this is it.
  const handleModeChange = (next: string) => {
    if (identityReadOnly || passThroughAuthorization) return;
    setSelectedMode(next as IdentityMode);
  };

  // What leaving the current mode would destroy. Identity is derived, so
  // leaving User means unbinding the client and leaving Agent means deleting
  // the credential; both are what Save has to do, not the card click.
  const leavingUser = actualMode === "user" && selectedMode !== "user";
  const leavingAgent = actualMode === "agent" && selectedMode !== "agent";
  const destructive = leavingUser || leavingAgent;

  const detachUserIdentity = async (): Promise<boolean> => {
    const userSessionIssuerId = target.userSessionIssuerId;
    if (!userSessionIssuerId) return false;
    for (const linked of clients) {
      await detachIssuer.mutateAsync({
        // The generated detach request reuses the attach form's field name;
        // the endpoint it posts to is what distinguishes them.
        request: {
          attachUserSessionIssuerForm: {
            id: linked.id,
            userSessionIssuerId,
          },
        },
      });
    }
    await invalidateAllRemoteSessionClients(queryClient);
    return true;
  };

  const removeAgentCredential = async (): Promise<boolean> => {
    if (!authorizationHeader) return false;
    await deleteHeader.mutateAsync({ request: { id: authorizationHeader.id } });
    const refreshed = await invalidateHeaders();
    if (!refreshed) {
      toast.warning("Credential removed, but headers could not be refreshed.");
    }
    return true;
  };

  const performSave = async () => {
    if (!canWrite || rbacLoading) return;
    setConfirmOpen(false);
    try {
      if (leavingUser) await detachUserIdentity();
      if (leavingAgent) await removeAgentCredential();
      if (selectedMode === "agent") {
        await agentDraft.save();
      } else if (selectedMode === "user") {
        userDraft.save();
      } else if (destructive) {
        toast.success("Identity removed");
      }
      // Headers last: identity may have just written or removed the
      // Authorization row, and these rows are diffed against what the server
      // holds once that has landed.
      if (await headerDrafts.save()) {
        toast.success("Upstream headers updated");
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to save identity",
      );
    }
  };

  const identityError = headersQuery.error ?? clientsQueryError;
  // A server that answers with a challenge has no business on No Identity, so
  // that card says so rather than a banner underneath the choice.
  const noneWarning =
    noneProbeStatus === "authentication-required"
      ? "This server answers with an authentication challenge. With no identity configured, requests to it will keep failing — choose User or Agent Identity."
      : null;
  const cards = identityModeCards(upstreamName);

  // One Save for the whole section. What it commits depends on the selected
  // mode, and it disappears into a disabled state when there is nothing to do
  // rather than sprouting a button per sub-form.
  let identityCanSave: boolean;
  if (selectedMode === "user") {
    identityCanSave = userDraft.canSave;
  } else if (selectedMode === "agent") {
    // Moving to Agent needs a credential; without one there is nothing for
    // the mode to actually be.
    identityCanSave = agentDraft.canSave;
  } else {
    // No Identity commits only the removal it implies.
    identityCanSave = destructive;
  }
  // Rows that cannot be written stop the whole commit rather than letting the
  // identity half through and dropping the rest on the floor.
  const headersBlocked =
    headerDrafts.isDirty && headerDrafts.validationError !== null;
  const headersReason = headersBlocked && headerDrafts.reportErrors;
  const canSave = !headersBlocked && (identityCanSave || headerDrafts.isDirty);
  const savePending =
    detachIssuer.isPending ||
    saving ||
    headerDrafts.saving ||
    (selectedMode === "user" ? userDraft.saving : agentDraft.saving);

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
                  to={routes.remoteIdentityProviders.href()}
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
                Remove it in Custom Headers before selecting Agent Identity or
                relying on No Identity.
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
                resourceId={target.permissionResourceId}
                level="component"
                className="w-full"
              >
                {({ disabled: scopeDisabled }) => (
                  <RadioCardGroup
                    orientation="horizontal"
                    showIndicator={false}
                    value={selectedMode}
                    disabled={identityReadOnly || scopeDisabled || saving}
                    onValueChange={handleModeChange}
                    className="grid-flow-row grid-cols-1 md:grid-flow-col md:grid-cols-none"
                  >
                    {cards.map((card) => {
                      const warned = card.value === "none" && !!noneWarning;
                      return (
                        <RadioCard
                          key={card.value}
                          value={card.value}
                          // Only the pass-through conflict genuinely blocks a
                          // choice: that legacy header already occupies the
                          // Authorization name a static credential needs.
                          // Agent to User is fine — a linked client wins over
                          // a static credential, and AIM-230 expects the stale
                          // one to be left visible for cleanup.
                          disabled={
                            card.value === "agent" && !!passThroughAuthorization
                          }
                          leading={card.icon}
                          // Both states, so selecting the card does not swap
                          // the warning border back to the selected one.
                          className={cn(
                            warned &&
                              "border-warning-default has-data-[state=checked]:border-warning-default",
                          )}
                          title={
                            warned ? (
                              <span className="flex items-center gap-2">
                                {card.title}
                                <HoverCard openDelay={150}>
                                  <HoverCardTrigger asChild>
                                    <span className="text-default-warning inline-flex cursor-help">
                                      <TriangleAlert
                                        aria-label="This server requires authentication"
                                        className="size-3.5"
                                      />
                                    </span>
                                  </HoverCardTrigger>
                                  <HoverCardContent
                                    align="start"
                                    className="w-72"
                                  >
                                    <Text small className="block">
                                      {noneWarning}
                                    </Text>
                                  </HoverCardContent>
                                </HoverCard>
                              </span>
                            ) : (
                              card.title
                            )
                          }
                        >
                          {card.description}
                        </RadioCard>
                      );
                    })}
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
                    to={routes.remoteIdentityProviders.href()}
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
                manageHref={routes.remoteIdentityProviders.href()}
                createHref={routes.remoteIdentityProviders.href()}
                inspectHref={mcpServerTabHref(routes, target.slug, "inspect")}
                providerHref={(issuerId) =>
                  routes.remoteIdentityProviders.issuerDetail.href(issuerId)
                }
                clientHref={(issuerId, clientId) =>
                  routes.remoteIdentityProviders.clientDetail.href(
                    issuerId,
                    clientId,
                  )
                }
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

          <Collapsible>
            <CollapsibleTrigger className="group hover:bg-muted/40 flex w-full items-center gap-3 px-6 py-4 text-left">
              <ChevronDown
                aria-hidden="true"
                className="size-4 transition-transform group-data-[state=open]:rotate-180"
              />
              <Text small className="font-medium">
                Custom Headers
              </Text>
              <Text muted small>
                Upstream headers sent with every request.
              </Text>
              {headerDrafts.isDirty ? (
                // The rows commit with the footer's Save, so a collapsed
                // disclosure must still admit it is holding unsaved edits.
                <Text muted small className="ml-auto">
                  Unsaved
                </Text>
              ) : null}
            </CollapsibleTrigger>
            <CollapsibleContent className="px-6 pt-2 pb-6">
              <HeadersSection
                siblingMcpServers={siblingMcpServers}
                state={headerDrafts}
                resourceId={target.permissionResourceId}
              />
            </CollapsibleContent>
          </Collapsible>
        </div>

        {identityResolved &&
        (selectedMode !== "none" || destructive || headerDrafts.isDirty) ? (
          <SettingsSection.Footer>
            {headersReason ? (
              // The disclosure can be collapsed over the offending row, so the
              // reason Save is dead has to be readable from out here, in the
              // same orange the row's field is wearing. Nothing is said when
              // nothing is wrong.
              <Text small warning>
                {headerDrafts.validationError}
              </Text>
            ) : null}
            <SettingsSection.FooterActions>
              <RequireScope
                scope="mcp:write"
                resourceId={target.permissionResourceId}
                level="component"
              >
                <FooterSaveButton
                  pending={savePending}
                  disabled={!canSave || savePending || identityReadOnly}
                  onClick={() => {
                    if (destructive) setConfirmOpen(true);
                    else void performSave();
                  }}
                />
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        ) : null}
      </SettingsSection.Panel>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>
              {leavingUser
                ? `Stop signing users in through ${upstreamName}?`
                : "Remove the shared credential?"}
            </Dialog.Title>
            <Dialog.Description>
              {leavingUser
                ? "Saving unlinks the identity provider from this server. People who already signed in lose access through it and would have to authorize again if you switch back. The provider and its client stay available to other servers."
                : "Saving removes the static Authorization credential from the Remote MCP source. Requests will no longer authenticate upstream."}
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={savePending}
              onClick={() => setConfirmOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={savePending || !canWrite || rbacLoading}
              onClick={() => void performSave()}
            >
              {savePending ? (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {savePending ? "Saving" : "Save changes"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
