import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge, type BadgeProps } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { VendorEmaTrustNotice } from "@/components/vendor-ema-trust-notice";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useProtectedResourceMetadata } from "@/lib/remote-identity/queries/useProtectedResourceMetadata";
import { identityProvidersHref } from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes } from "@/routes";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation } from "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js";
import {
  useRemoteSessionClientsPrepareEMAMutation,
  type RemoteSessionClientsPrepareEMAMutationVariables,
} from "@gram/client/react-query/remoteSessionClientsPrepareEMA.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { buildRemoteSessionClientsReadEMAMutation } from "@gram/client/react-query/remoteSessionClientsReadEMA.js";
import { useRemoteSessionClientsUnlinkEMAMutation } from "@gram/client/react-query/remoteSessionClientsUnlinkEMA.js";

import { AuthRow } from "./AuthRow";
import { EmaProviderOrderLine } from "./EmaProviderOrderLine";
import { useSpeakeasyAppendCheck } from "./useSpeakeasyAppendCheck";
import {
  trustsExternalIdp,
  type ChainingProviders,
} from "./identityChainingGate";
import { IdentityChainingScopesField } from "./IdentityChainingScopesField";
import {
  chainingBound,
  chainingGrantDeclaration,
  chainingResourceMetadata,
  chainingScopesAllowed,
  chainingUnlinkable,
  defaultChainingScopes,
  displayedChainingState,
  isCimdClient,
  isPublicClient,
  preparationSucceeded,
  sameScopes,
  sanitizeChainingScopes,
} from "./identityChainingScopes";

type ChainingState = IdentityChainingPreparation["state"];
type BadgeVariant = NonNullable<BadgeProps["variant"]>;

function stateBadge(state: ChainingState): {
  label: string;
  variant: BadgeVariant;
} {
  switch (state) {
    case "ready":
      return { label: "Enabled", variant: "success" };
    case "unlinked":
      return { label: "Not enabled", variant: "neutral" };
    case "in_progress":
      return { label: "In progress", variant: "information" };
    case "published_acceptance_unverified":
      return { label: "Published, unverified", variant: "information" };
    case "configuration_required":
      return { label: "Configuration required", variant: "warning" };
    case "manual_setup_required":
      return { label: "Manual setup required", variant: "warning" };
    case "unknown_grants":
      return { label: "Grants unknown", variant: "warning" };
    case "incomplete_metadata":
      return { label: "Incomplete metadata", variant: "warning" };
    case "indeterminate":
      return { label: "Indeterminate", variant: "warning" };
    case "transient_failure":
      return { label: "Temporary failure", variant: "warning" };
    case "unsupported_profile":
      return { label: "Not supported", variant: "destructive" };
    case "provider_rejection":
      return { label: "Provider rejected", variant: "destructive" };
  }
}

/** A server refusal as a sentence. */
function sentence(message: string): string {
  const text = message.trim();
  if (!text) return text;
  const capitalized = text[0]!.toUpperCase() + text.slice(1);
  return /[.!?]$/.test(capitalized) ? capitalized : `${capitalized}.`;
}

function ClientPicker({
  clients,
  value,
  onChange,
  disabled,
}: {
  clients: RemoteSessionClient[];
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {clients.map((client) => (
          <SelectItem key={client.id} value={client.id}>
            {client.clientId}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function chainingHint(providers: ChainingProviders): string {
  if (!providers.speakeasy)
    return "Exchange the person’s identity provider sign-in for a token to this server, without a separate consent.";
  if (!providers.external)
    return "Speakeasy vouches for the person’s Speakeasy sign-in to get a token to this server, without a separate consent.";
  return "Exchange the person’s identity provider sign-in, or have Speakeasy vouch for their Speakeasy sign-in, for a token to this server without a separate consent.";
}

/** Rendered only for an organization issuer that an identity provider or Speakeasy can vouch for. */
export function IdentityChainingField({
  userSessionIssuer,
  providers,
  speakeasyIssuerUrl,
  speakeasyEnabled = false,
  linkedClients,
  resource,
  remoteMcpServerId,
}: {
  userSessionIssuer: UserSessionIssuer;
  providers: ChainingProviders;
  /** Speakeasy's issuer URL, which vendors trust for Speakeasy-signed grants. */
  speakeasyIssuerUrl?: string;
  /** Speakeasy's issuer is turned on and can sign. */
  speakeasyEnabled?: boolean;
  linkedClients: RemoteSessionClient[];
  /** The remote MCP server URL; the executor matches it exactly. */
  resource: string;
  /** Reuses the server's protected resource metadata probe for advertised scopes. */
  remoteMcpServerId?: string;
}): JSX.Element {
  const core = useGramContext();
  const gramProject = useProjectSlugForRequests();
  // The identity provider's own sign-in client is never a chaining target.
  const trustedClientId = userSessionIssuer.trustedRemoteSessionClientId;
  const candidates = linkedClients.filter(
    (client) => !trustedClientId || client.id !== trustedClientId,
  );
  const [selectedId, setSelectedIdState] = useState<string | null>(null);
  // A prepare that returned a blocker; kept until the next action.
  const [prepareOutcome, setPrepareOutcome] =
    useState<IdentityChainingPreparation | null>(null);
  const [grantsConfirmed, setGrantsConfirmed] = useState(false);
  const setSelectedId = (id: string) => {
    setSelectedIdState(id);
    setPrepareOutcome(null);
    setGrantsConfirmed(false);
  };
  const picked =
    candidates.find((client) => client.id === selectedId) ?? candidates[0];

  const target = picked && {
    userSessionIssuerId: userSessionIssuer.id,
    remoteSessionIssuerId: picked.remoteSessionIssuerId,
    resource,
  };

  const queryClient = useQueryClient();
  const readKey = ["identityChaining", "readEMA", gramProject, target];
  const read = useQuery({
    queryKey: readKey,
    enabled: target != null,
    retry: false,
    queryFn: ({ signal }) => {
      if (!target) throw new Error("No client selected");
      return buildRemoteSessionClientsReadEMAMutation(core).mutationFn({
        request: { gramProject, readEMARequestBody: target },
        options: { fetchOptions: { signal } },
      });
    },
  });

  const prm = useProtectedResourceMetadata(
    remoteMcpServerId,
    remoteMcpServerId !== undefined,
  );
  const [scopeDraft, setScopeDraft] = useState<{
    clientId: string;
    scopes: string[];
  } | null>(null);
  // The binding is keyed by issuer and resource, so it may name another client.
  const boundClient = chainingBound(read.data)
    ? candidates.find(
        (client) =>
          client.id === read.data?.clientId &&
          client.remoteSessionIssuerId === picked?.remoteSessionIssuerId,
      )
    : undefined;
  const selected = selectedId === null ? (boundClient ?? picked) : picked;
  const switchingClient =
    chainingBound(read.data) && read.data?.clientId !== selected?.id;
  const bound = chainingBound(read.data) && !switchingClient;
  const storedScopes = bound && read.data ? read.data.scopes : [];
  const activeScopes = bound
    ? sanitizeChainingScopes(storedScopes)
    : defaultChainingScopes(selected?.scope);
  const scopes =
    scopeDraft && scopeDraft.clientId === selected?.id
      ? scopeDraft.scopes
      : activeScopes;
  const scopesValid =
    selected != null && chainingScopesAllowed(scopes, selected.scope);
  const scopesChanged =
    bound &&
    !sameScopes(
      sanitizeChainingScopes(scopes),
      sanitizeChainingScopes(storedScopes),
    );
  const [confirmDisable, setConfirmDisable] = useState(false);
  const trustedIssuerQuery = useRemoteSessionIssuer(
    { id: userSessionIssuer.trustedRemoteSessionIssuerId ?? "" },
    undefined,
    {
      enabled:
        providers.external && !!userSessionIssuer.trustedRemoteSessionIssuerId,
      throwOnError: false,
      retry: false,
    },
  );

  const { hasScope } = useRBAC();
  const organization = useOrganization();
  const project = useProject();
  const cimd = !!selected && isCimdClient(selected);
  const publicClient = !!selected && isPublicClient(selected);
  const grantDeclaration = selected
    ? chainingGrantDeclaration(selected.grantTypes, cimd)
    : null;
  const needsGrantConfirm = !!grantDeclaration?.rewrites;
  // An organization-owned client's registration is shared across projects.
  const grantsBlocked =
    needsGrantConfirm &&
    selected?.projectId === "" &&
    !hasScope("org:admin", organization.id);

  const onSettled = () => void read.refetch();

  // A sign-in that trusts no external identity provider can only be vouched
  // for by Speakeasy, so enabling also puts Speakeasy in the vendor's order.
  const speakeasyOnly =
    providers.speakeasy && !trustsExternalIdp(userSessionIssuer);
  const session = useSession();
  const orgRoutes = useOrgRoutes();
  const order = read.data?.emaProviders;
  const speakeasyInOrder =
    !!order &&
    !order.isDefault &&
    order.providers.some((provider) => provider.kind === "speakeasy");
  const needsSpeakeasy = speakeasyOnly && !!order && !speakeasyInOrder;
  const orgAdmin = hasScope("org:admin", organization.id);
  const supportSession =
    session.organizationOverride || !!session.impersonatorEmail;
  const canAddSpeakeasy =
    needsSpeakeasy && speakeasyEnabled && orgAdmin && !supportSession;
  const appendTarget = picked && {
    remoteSessionIssuerId: picked.remoteSessionIssuerId,
    userSessionIssuerId: userSessionIssuer.id,
  };
  const appendCheck = useSpeakeasyAppendCheck(
    appendTarget ?? { remoteSessionIssuerId: "", userSessionIssuerId: "" },
    canAddSpeakeasy && !!appendTarget && !read.isFetching,
  );
  const addSpeakeasy =
    useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation({
      onError: () => undefined,
    });
  const identitySettingsHref = `${orgRoutes.identity.href()}${identityProvidersHref()}`;
  let speakeasyBlock: {
    reason: string;
    link?: { href: string; label: string };
  } | null = null;
  if (speakeasyOnly && !speakeasyEnabled)
    speakeasyBlock = {
      reason: "Turn on Speakeasy’s issuer first.",
      link: {
        href: identitySettingsHref,
        label: "Speakeasy identity provider",
      },
    };
  else if (needsSpeakeasy && !orgAdmin)
    speakeasyBlock = {
      reason:
        "An organization admin needs to add Speakeasy for this vendor first.",
    };
  else if (needsSpeakeasy && supportSession)
    speakeasyBlock = {
      reason:
        "The provider order can’t be changed from a support or impersonated session.",
    };
  else if (needsSpeakeasy && appendCheck.isError)
    speakeasyBlock = { reason: sentence(appendCheck.error.message) };
  const speakeasyChecking =
    canAddSpeakeasy && (appendCheck.isFetching || !appendCheck.isSuccess);
  const prepare = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      if (!preparationSucceeded(result)) {
        setPrepareOutcome(result);
        toast.warning(
          `Identity chaining not enabled: ${stateBadge(result.state).label}`,
        );
        return;
      }
      queryClient.setQueryData(readKey, result);
      // Declared grants are written onto the client row.
      void invalidateAllRemoteSessionClients(queryClient);
      setScopeDraft(null);
      setGrantsConfirmed(false);
      toast.success(`Identity chaining: ${stateBadge(result.state).label}`);
    },
    onError: () => undefined,
    onSettled,
  });
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      setScopeDraft(null);
      toast.success("Identity chaining disabled");
    },
    onError: () => undefined,
    onSettled,
  });

  const enable = () => {
    if (
      !selected ||
      !target ||
      !read.data ||
      !scopesValid ||
      !grantDeclaration ||
      publicClient
    )
      return;
    if (grantsBlocked || (needsGrantConfirm && !grantsConfirmed)) return;
    setPrepareOutcome(null);
    const prepareRequest: RemoteSessionClientsPrepareEMAMutationVariables = {
      request: {
        gramProject,
        prepareEMARequestBody: {
          ...target,
          clientId: selected.id,
          scopes,
          mechanism: cimd ? "cimd" : "manual",
          confirmGrants: grantDeclaration.grants,
          expectedGeneration: read.data.generation,
          resourceMetadata: chainingResourceMetadata(prm.metadata),
        },
      },
    };
    if (!needsSpeakeasy) {
      prepare.mutate(prepareRequest);
      return;
    }
    // Add first: a failed prepare leaves Speakeasy in the order, which is
    // harmless and makes a retry's add a no-op.
    if (!appendTarget || speakeasyBlock) return;
    addSpeakeasy.mutate(
      {
        request: {
          addSpeakeasyEMAProviderRequestBody: {
            ...appendTarget,
            dryRun: false,
          },
        },
      },
      { onSuccess: () => prepare.mutate(prepareRequest) },
    );
  };
  const appendSpeakeasy = () => {
    if (!appendTarget || speakeasyBlock) return;
    addSpeakeasy.mutate(
      {
        request: {
          addSpeakeasyEMAProviderRequestBody: {
            ...appendTarget,
            dryRun: false,
          },
        },
      },
      {
        onSuccess: () => {
          toast.success("Speakeasy added to the provider order");
        },
        onSettled,
      },
    );
  };
  const disable = () => {
    if (!target || !read.data) return;
    setPrepareOutcome(null);
    unlink.mutate(
      {
        request: {
          gramProject,
          unlinkEMARequestBody: {
            ...target,
            expectedGeneration: read.data.generation,
          },
        },
      },
      { onSettled: () => setConfirmDisable(false) },
    );
  };

  const pending =
    read.isFetching ||
    prepare.isPending ||
    unlink.isPending ||
    addSpeakeasy.isPending;
  const ready = bound && read.data?.state === "ready";
  const shownState = read.data ? displayedChainingState(read.data) : null;
  const canUnlink = chainingUnlinkable(read.data);
  // Enable/Disable already say whether chaining is on.
  const badgeRedundant =
    shownState === "unlinked" || (shownState === "ready" && canUnlink);
  const badge = shownState && !badgeRedundant ? stateBadge(shownState) : null;
  const enableBlocked =
    !!speakeasyBlock ||
    (needsSpeakeasy && speakeasyChecking) ||
    publicClient ||
    !scopesValid ||
    grantsBlocked ||
    (needsGrantConfirm && !grantsConfirmed);

  return (
    <>
      <AuthRow label="Identity chaining" hint={chainingHint(providers)}>
        {candidates.length === 0 ? (
          <Text muted small>
            {providers.external
              ? "Link a client for this server’s identity provider first."
              : "Link a client registered with this server’s authorization server first."}
          </Text>
        ) : (
          <>
            {candidates.length > 1 && (
              <ClientPicker
                clients={candidates}
                value={selected?.id ?? ""}
                onChange={setSelectedId}
                disabled={prepare.isPending || unlink.isPending}
              />
            )}
            {selected && (
              <IdentityChainingScopesField
                client={selected}
                resourceScopes={prm.metadata?.scopesSupported}
                value={scopes}
                onChange={(next) => {
                  setPrepareOutcome(null);
                  setScopeDraft({ clientId: selected.id, scopes: next });
                }}
                disabled={pending || !read.data}
              />
            )}
            <div className="flex flex-wrap items-center gap-3">
              {badge && (
                <Badge variant={badge.variant} size="sm">
                  {badge.label}
                </Badge>
              )}
              {read.isPending && (
                <Text muted small>
                  Checking…
                </Text>
              )}
              <RequireScope
                scope="project:write"
                resourceId={project.id}
                level="component"
              >
                {ready ? (
                  scopesChanged && (
                    <Button
                      size="sm"
                      disabled={pending || enableBlocked}
                      onClick={enable}
                    >
                      Update scopes
                    </Button>
                  )
                ) : (
                  <Button
                    size="sm"
                    disabled={pending || !read.data || enableBlocked}
                    onClick={enable}
                  >
                    {switchingClient
                      ? "Use this client instead"
                      : "Enable identity chaining"}
                  </Button>
                )}
                {canUnlink && (
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={pending}
                    onClick={() => setConfirmDisable(true)}
                  >
                    Disable
                  </Button>
                )}
              </RequireScope>
            </div>
            {!ready && speakeasyBlock && (
              <Text muted small className="block">
                {speakeasyBlock.reason}
                {speakeasyBlock.link && (
                  <>
                    {" "}
                    <Link
                      className="underline underline-offset-4"
                      to={speakeasyBlock.link.href}
                    >
                      {speakeasyBlock.link.label}
                    </Link>
                  </>
                )}
              </Text>
            )}
            {ready && needsSpeakeasy && (
              <div className="flex flex-wrap items-center gap-3">
                <Text warning small>
                  Speakeasy isn’t in this vendor’s provider order, so identity
                  chaining can’t run.
                </Text>
                {canAddSpeakeasy && (
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={pending || speakeasyChecking || !!speakeasyBlock}
                    onClick={appendSpeakeasy}
                  >
                    Add Speakeasy
                  </Button>
                )}
                {speakeasyBlock && (
                  <Text muted small className="block w-full">
                    {speakeasyBlock.reason}
                  </Text>
                )}
              </div>
            )}
            {switchingClient && (
              <Text muted small className="block">
                Identity chaining uses client{" "}
                {boundClient?.clientId ?? read.data?.externalClientId}. Using
                this client instead replaces it.
              </Text>
            )}
            {publicClient && selected && (
              <Text muted small className="block">
                Client {selected.clientId} is a public client. Identity chaining
                needs a confidential client that authenticates to the provider’s
                token endpoint.
              </Text>
            )}
            {!publicClient &&
              needsGrantConfirm &&
              grantDeclaration &&
              selected && (
                <RequireScope
                  scope="project:write"
                  resourceId={project.id}
                  level="component"
                >
                  {grantsBlocked ? (
                    <Text muted small className="block">
                      This client belongs to the organization. An organization
                      admin must confirm its grants before enabling identity
                      chaining.
                    </Text>
                  ) : (
                    <label className="flex items-start gap-2">
                      <Checkbox
                        className="mt-0.5"
                        checked={grantsConfirmed}
                        disabled={pending}
                        onCheckedChange={(next) =>
                          setGrantsConfirmed(next === true)
                        }
                      />
                      <Text small>
                        {cimd
                          ? "Publish these grants in the client's metadata document: "
                          : `Declare that client ${selected.clientId} is registered with the provider for these grants, and record them on it: `}
                        {grantDeclaration.grants.join(", ")}
                      </Text>
                    </label>
                  )}
                </RequireScope>
              )}
            {!ready && providers.external && (
              <VendorEmaTrustNotice
                issuerUrl={trustedIssuerQuery.data?.issuer}
              />
            )}
            {!ready && speakeasyOnly && (
              <VendorEmaTrustNotice issuerUrl={speakeasyIssuerUrl} speakeasy />
            )}
            {prepareOutcome?.remediation ? (
              <Alert variant="warning" dismissible={false}>
                {prepareOutcome.remediation}
              </Alert>
            ) : (
              read.data?.remediation &&
              !ready &&
              shownState !== "unlinked" && (
                <Text muted small className="block">
                  {read.data.remediation}
                </Text>
              )
            )}
            {order && (
              <EmaProviderOrderLine
                order={order}
                speakeasyOnly={speakeasyOnly}
              />
            )}
            <ApiErrorAlert
              error={
                read.error ??
                addSpeakeasy.error ??
                prepare.error ??
                unlink.error
              }
            />
          </>
        )}
      </AuthRow>
      <Dialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Disable identity chaining?</Dialog.Title>
            <Dialog.Description>
              Speakeasy stops exchanging sign-ins for tokens to this server.
              Every person using it falls back to interactive sign-in and
              authorizes the server themselves.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={unlink.isPending}
              onClick={() => setConfirmDisable(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={unlink.isPending}
              onClick={disable}
            >
              {unlink.isPending ? "Disabling…" : "Disable"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
