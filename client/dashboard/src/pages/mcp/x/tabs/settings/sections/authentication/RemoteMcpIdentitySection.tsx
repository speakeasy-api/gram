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
import { useRoutes } from "@/routes";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useDetachUserSessionIssuerMutation } from "@gram/client/react-query/detachUserSessionIssuer.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { invalidateAllGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Loader2, TriangleAlert } from "lucide-react";
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
import { ResourceScopePinField } from "./ResourceScopePinField";
import { useResourceScopePin } from "./resourceScopePin";
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
    { enabled: remoteMcpServerId !== "", throwOnError: false },
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
  const {
    data: userSessionIssuer,
    isLoading: issuerLoading,
    isError: issuerError,
  } = useUserSessionIssuer(
    { id: target.userSessionIssuerId ?? undefined },
    undefined,
    { enabled: !!target.userSessionIssuerId, throwOnError: false },
  );
  // On an organization-level issuer, a binding to an organization-owned
  // client is shared by every project's servers on that issuer, so the server
  // refuses to replace it from one project (ErrIdentityOrgWideBinding).
  // Project-owned clients stay editable, so only that case locks the panel.
  const organizationIssuer = userSessionIssuer?.projectId === "";
  const orgSharedClient = organizationIssuer
    ? clients.find((linked) => linked.projectId === "")
    : undefined;
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
  // Held until the issuer is classified: an organization-wide one could make
  // Save fail with ErrIdentityOrgWideBinding, so neither a pending nor a
  // failed lookup may leave the controls open.
  const identityReadOnly =
    sharedSource ||
    !!orgSharedClient ||
    issuerLoading ||
    issuerError ||
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

  // The scope pin belongs to the server's protected resource, and reading it
  // needs mcp:write, so it is only fetched for a writer with a bound client.
  const scopePin = useResourceScopePin({
    mcpServerId: target.permissionResourceId,
    enabled: canWrite && identityResolved && actualMode === "user",
  });
  const scopePinSlot =
    canWrite && selectedMode === "user" && userDraft.connected;
  const showScopePin = scopePinSlot && !!scopePin.data;
  const scopePinDirty = showScopePin && scopePin.dirty;

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
  // A server can carry a bound client and a leftover static credential at
  // once, and the client wins the derived mode. No Identity has to clear both
  // or the credential alone would make the server read as Agent again — and
  // unlike under User, nothing overrides it on the way upstream.
  const leavingAgent =
    (actualMode === "agent" && selectedMode !== "agent") ||
    (selectedMode === "none" && !!authorizationHeader);
  // Staying on User but saving a different client swaps out the one people
  // signed in through, so they all have to sign in again.
  const replacingClient = selectedMode === "user" && userDraft.replacesClient;
  const destructive = leavingUser || leavingAgent || replacingClient;

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
    await Promise.all([
      invalidateAllRemoteSessionClients(queryClient),
      invalidateAllGetRemoteMcpServerScopes(queryClient),
    ]);
    return true;
  };

  const removeAgentCredential = async (): Promise<boolean> => {
    if (!authorizationHeader) return false;
    // Under No Identity the row is editable in Custom Headers. When the draft
    // has already dropped it, the header save deletes it; deleting it here
    // first would fail that save on a row that is gone and strand the rest.
    const headerId = authorizationHeader.id;
    if (!headerDrafts.drafts.some((draft) => draft.id === headerId)) {
      return false;
    }
    await deleteHeader.mutateAsync({ request: { id: headerId } });
    const refreshed = await invalidateHeaders();
    if (!refreshed) {
      toast.warning("Credential removed, but headers could not be refreshed.");
    }
    return true;
  };

  // Switching to User commits a client; without one, removing the old
  // identity first would leave the server with none.
  const userSwitchBlocked =
    selectedMode === "user" && actualMode !== "user" && !userDraft.canSave;
  // An unfinished identity edit holds the whole commit, or Save would write
  // the rest and silently drop it.
  const userEditIncomplete =
    selectedMode === "user" &&
    actualMode === "user" &&
    userDraft.pendingChange &&
    !userDraft.canSave;

  const performSave = async () => {
    if (!canWrite || rbacLoading || userSwitchBlocked || userEditIncomplete)
      return;
    setConfirmOpen(false);
    try {
      if (leavingUser) await detachUserIdentity();
      if (selectedMode === "user" && leavingAgent) {
        // Bind the client before dropping the credential, so a failed commit
        // never leaves the server with no identity; the client outranks the
        // header while both exist.
        if (!(await userDraft.save())) return;
        await removeAgentCredential();
      } else {
        if (leavingAgent) await removeAgentCredential();
        if (selectedMode === "agent") {
          await agentDraft.save();
        } else if (
          selectedMode === "user" &&
          (actualMode !== "user" || userDraft.canSave)
        ) {
          // A pin or header edit alone must not recommit the connected client.
          await userDraft.save();
        }
      }
    } catch (error) {
      toast.error(errorMessage(error, "Failed to save identity"));
      return;
    }
    // Headers after identity: it may have just written or removed the
    // Authorization row, and these rows are diffed against what the server
    // holds once that has landed. The pin is independent of both.
    const [pinResult, headersResult] = await Promise.allSettled([
      scopePinDirty ? scopePin.save() : Promise.resolve(false),
      headerDrafts.save(),
    ]);
    if (pinResult.status === "rejected") {
      toast.error(
        errorMessage(pinResult.reason, "Failed to save pinned scopes"),
      );
    } else if (pinResult.value) {
      toast.success("Pinned scopes updated");
    }
    if (headersResult.status === "rejected") {
      toast.error(errorMessage(headersResult.reason, "Failed to save headers"));
      return;
    }
    // Reported only once the headers have landed: when the draft already
    // dropped the credential, the header save is what deletes it, and a
    // failure there must not follow a claim that it is gone.
    if (selectedMode === "none" && destructive) {
      toast.success("Identity removed");
    }
    if (headersResult.value) {
      toast.success("Upstream headers updated");
    }
  };

  const identityError = headersQuery.error ?? clientsQueryError;
  // A server that answers with a challenge has no business on No Identity, so
  // that card says so rather than a banner underneath the choice.
  const noneWarning =
    noneProbeStatus === "authentication-required"
      ? "This server answers with an authentication challenge. With no identity configured, requests to it will keep failing — choose User Identity or a Service Account."
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
    // the mode to actually be. One already on the server counts: in the
    // legacy state where a bound client outranks a static header, detaching
    // the client is the whole change, and demanding a fresh secret would ask
    // the operator to retype one they cannot read.
    identityCanSave =
      agentDraft.canSave || (leavingUser && !!authorizationHeader);
  } else {
    // No Identity commits only the removal it implies.
    identityCanSave = destructive;
  }
  // A locked identity still lets a pin-only edit through.
  const pinOnlyChange =
    scopePinDirty && !identityCanSave && !headerDrafts.isDirty;
  // Rows that cannot be written stop the whole commit rather than letting the
  // identity half through and dropping the rest on the floor.
  const headersBlocked =
    headerDrafts.isDirty && headerDrafts.validationError !== null;
  const headersReason = headersBlocked && headerDrafts.reportErrors;
  const canSave =
    !headersBlocked &&
    !userSwitchBlocked &&
    !userEditIncomplete &&
    (identityCanSave || headerDrafts.isDirty || scopePinDirty);
  const savePending =
    detachIssuer.isPending ||
    scopePin.saving ||
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

            {orgSharedClient && !sharedSource ? (
              <Alert variant="warning" dismissible={false}>
                This server uses a client shared by every project on the
                organization&apos;s session issuer, so it can&apos;t be changed
                here. An organization admin can remove it from this server on
                the{" "}
                <Link
                  className="font-medium underline underline-offset-2"
                  to={routes.remoteIdentityProviders.clientDetail.mcpServers.href(
                    orgSharedClient.remoteSessionIssuerId,
                    orgSharedClient.id,
                  )}
                >
                  client&apos;s MCP servers
                </Link>
                , or you can move this server to a project session issuer under
                Sessions below.
              </Alert>
            ) : null}

            {siblingsQuery.isError ? (
              <Alert variant="error" dismissible={false}>
                Could not verify whether this Remote MCP source is shared.
                Identity editing is disabled.
              </Alert>
            ) : null}

            {issuerError ? (
              <Alert variant="error" dismissible={false}>
                Could not load this server's user session issuer. Identity
                editing is disabled.
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
                Remove it in Custom Headers before selecting Service Account or
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
            // The provider row is the whole decision, so it takes the full
            // width rather than sitting beside a label that restates it.
            <div className="px-6 py-5">
              <UserIdentityRow
                draft={userDraft}
                disabled={identityReadOnly || userDraft.saving}
                createHref={routes.remoteIdentityProviders.href()}
                clientHref={(issuerId, clientId) =>
                  routes.remoteIdentityProviders.clientDetail.href(
                    issuerId,
                    clientId,
                  )
                }
              />
              {showScopePin && scopePin.data ? (
                // Commits with the footer's Save, like the rest of the panel.
                <div className="mt-4 pl-[52px]">
                  <ResourceScopePinField
                    pin={scopePin}
                    scopes={scopePin.data}
                    connectedClientId={userDraft.connectedClient?.id ?? null}
                    issuerScopes={userDraft.scopeOptions}
                    serverName={
                      linkedServers
                        .find(
                          (server) => server.id === target.permissionResourceId,
                        )
                        ?.name?.trim() ?? ""
                    }
                    // The pin has its own lock: the server checks write
                    // access to every server sharing the resource.
                    disabled={!canWrite || savePending}
                  />
                </div>
              ) : scopePinSlot && scopePin.isError ? (
                <Text muted small className="mt-4 block pl-[52px]">
                  {scopePin.forbidden
                    ? "Pinned scopes are shared by every MCP server that uses this URL. You need edit access to all of them to view or change the pin."
                    : "Couldn't load pinned scopes."}
                </Text>
              ) : scopePinSlot ? (
                <Text muted small className="mt-4 block pl-[52px]">
                  Loading pinned scopes…
                </Text>
              ) : null}
            </div>
          ) : null}

          {identityResolved && selectedMode === "agent" ? (
            <AuthRow
              label="Service Account credential"
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
            {userEditIncomplete ? (
              <Text small warning>
                Finish the User Identity change, or cancel it, to save.
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
                  disabled={
                    !canSave ||
                    savePending ||
                    (identityReadOnly && !pinOnlyChange)
                  }
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
              {confirmTitle(leavingUser, replacingClient, upstreamName)}
            </Dialog.Title>
            <Dialog.Description>
              {removalConsequences(leavingUser, leavingAgent, replacingClient)}
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

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

const UNLINK_PROVIDER_CONSEQUENCE =
  "Saving unlinks the identity provider from this server. People who already signed in lose access through it and would have to authorize again if you switch back. The provider and its client stay available to other servers.";
const REPLACE_CLIENT_CONSEQUENCE =
  "Saving replaces the client this server uses. Everyone signed in through the current one will have to sign in again. The old client stays available to other servers.";
const REMOVE_CREDENTIAL_CONSEQUENCE =
  "Saving removes the static Authorization credential from the Remote MCP source. Requests will no longer authenticate upstream.";

/**
 * What the confirmation discloses. A server can carry a bound client and a
 * leftover static credential at once, and No Identity removes both, so the
 * dialog has to name both.
 */
function removalConsequences(
  leavingUser: boolean,
  leavingAgent: boolean,
  replacingClient: boolean,
): string {
  if (replacingClient) return REPLACE_CLIENT_CONSEQUENCE;
  if (leavingUser && leavingAgent) {
    return `${UNLINK_PROVIDER_CONSEQUENCE} ${REMOVE_CREDENTIAL_CONSEQUENCE}`;
  }
  if (leavingUser) return UNLINK_PROVIDER_CONSEQUENCE;
  return REMOVE_CREDENTIAL_CONSEQUENCE;
}

function confirmTitle(
  leavingUser: boolean,
  replacingClient: boolean,
  upstreamName: string,
): string {
  if (replacingClient) return "Replace the connected client?";
  if (leavingUser) return `Stop signing users in through ${upstreamName}?`;
  return "Remove the shared credential?";
}
