import { useState, type ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { SettingsSection } from "@/components/detail/settings-section";
import { CopyableValue } from "@/components/setup-steps/StepBlocks";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Alert } from "@/components/ui/Alert";
import { Checkbox } from "@/components/ui/Checkbox";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { StatusDot, type StatusDotTone } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { mapWithConcurrency } from "@/lib/bounded-concurrency";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useProtectedResourceMetadata } from "@/lib/remote-identity/queries/useProtectedResourceMetadata";
import {
  acceptanceLabel,
  issuerDisplayName,
  vouchedFor,
  providerFixLink,
  providerView,
  serviceName,
  addIssuerStep,
  crossAppAccessStep,
} from "@/pages/org/identity-provider/services/emaServiceView";
import { useOktaIssuerId } from "@/pages/org/identity-provider/services/useEmaServices";
import {
  emaServiceHref,
  identityProvidersHref,
  oktaViewHref,
} from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { invalidateAllOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import {
  useRemoteSessionClientsPrepareEMAMutation,
  type RemoteSessionClientsPrepareEMAMutationVariables,
} from "@gram/client/react-query/remoteSessionClientsPrepareEMA.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { buildRemoteSessionClientsReadEMAMutation } from "@gram/client/react-query/remoteSessionClientsReadEMA.js";
import { useRemoteSessionClientsUnlinkEMAMutation } from "@gram/client/react-query/remoteSessionClientsUnlinkEMA.js";

import { MCP_SESSIONS_SECTION_ID } from "../RemoteMcpSessionsSection";
import { type ChainingProviders } from "./identityChainingGate";
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
  sanitizeChainingScopes,
} from "./identityChainingScopes";

type ChainingState = IdentityChainingPreparation["state"];
type Tone = StatusDotTone;

/** Why Turn on is blocked, or an on server isn't running, said the same way. */
const NO_VOUCHER = "No identity provider can vouch for people on this server.";

/** A blocked state in plain words, with what to do next. */
function stateCopy(
  state: ChainingState,
  vendor: string,
): { label: string; next: string; tone: Tone } {
  switch (state) {
    case "ready":
      return { label: "On", next: "", tone: "success" };
    case "unlinked":
      return { label: "Off", next: "", tone: "neutral" };
    case "in_progress":
      return {
        label: "Setting up",
        next: "Speakeasy is still setting this up. Check back in a minute.",
        tone: "information",
      };
    case "published_acceptance_unverified":
      return {
        label: `Waiting for ${vendor}`,
        next: `Speakeasy published the client’s settings. ${vendor} hasn’t confirmed them yet. Try connecting from your MCP client.`,
        tone: "information",
      };
    case "configuration_required":
      return {
        label: "Needs setup",
        next: "This server’s client needs a change first. Check the client in Identity above.",
        tone: "warning",
      };
    case "manual_setup_required":
      return {
        label: "Needs a manual step",
        next: `${vendor} needs the client registered by hand. Register it in ${vendor}’s admin settings, then turn this on again.`,
        tone: "warning",
      };
    case "unknown_grants":
      return {
        label: "Couldn’t check the client",
        next: "Speakeasy couldn’t tell which sign-in methods the client allows. Confirm them below, then turn this on again.",
        tone: "warning",
      };
    case "incomplete_metadata":
      return {
        label: `Couldn’t read ${vendor}’s settings`,
        next: `${vendor} didn’t publish everything Speakeasy needs. Try again later.`,
        tone: "warning",
      };
    case "indeterminate":
      return {
        label: "Couldn’t tell",
        next: "Speakeasy couldn’t confirm the setup. Try again.",
        tone: "warning",
      };
    case "transient_failure":
      return {
        label: "Temporary failure",
        next: "Something failed for a moment. Try again shortly.",
        tone: "warning",
      };
    case "unsupported_profile":
      return {
        label: "Not supported",
        next: `${vendor} doesn’t support the kind of Enterprise Managed Authorization that Speakeasy uses.`,
        tone: "destructive",
      };
    case "provider_rejection":
      return {
        label: `${vendor} refused`,
        next: `${vendor} refused the request. Check its admin settings, then try again.`,
        tone: "destructive",
      };
  }
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
      <SelectTrigger className="max-w-md">
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

/**
 * One link in the chain that gets a person from their sign-in to the
 * service: who it is, whether it holds, and where to fix it.
 */
function ChainLink({
  label,
  tone,
  children,
}: {
  label: string;
  tone: Tone;
  children: ReactNode;
}): JSX.Element {
  return (
    <li className="grid items-start gap-x-6 gap-y-2 px-6 py-4 sm:grid-cols-[170px_minmax(0,1fr)]">
      <div className="flex items-center gap-2 sm:pt-0.5">
        <StatusDot tone={tone} />
        <span className="text-eyebrow">{label}</span>
      </div>
      <div className="min-w-0 space-y-2">{children}</div>
    </li>
  );
}

function SettingsLink({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Link
      className="text-muted-foreground text-sm underline underline-offset-4"
      to={to}
    >
      {children}
    </Link>
  );
}

/** How many stale-binding reads run at once. */
const MAX_CONCURRENT_STALE_READS = 3;

/** Rendered only for an organization issuer that an identity provider or Speakeasy can vouch for. */
export function IdentityChainingField({
  userSessionIssuer,
  providers,
  speakeasyIssuerUrl,
  linkedClients,
  resource,
  remoteMcpServerId,
  onlyWhenOn = false,
  frame = (content) => <>{content}</>,
}: {
  userSessionIssuer: UserSessionIssuer;
  providers: ChainingProviders;
  /** Speakeasy's issuer URL, which vendors trust for Speakeasy-signed grants. */
  speakeasyIssuerUrl?: string;
  linkedClients: RemoteSessionClient[];
  /** The remote MCP server URL; the executor matches it exactly. */
  resource: string;
  /** Reuses the server's protected resource metadata probe for advertised scopes. */
  remoteMcpServerId?: string;
  /** Render nothing unless a binding is on, for when who can vouch is unknown. */
  onlyWhenOn?: boolean;
  /** Wraps whatever renders, so the caller's section shows only with it. */
  frame?: (content: ReactNode) => JSX.Element;
}): JSX.Element | null {
  const core = useGramContext();
  const gramProject = useProjectSlugForRequests();
  // The identity provider's own sign-in client is never a chaining target.
  const trustedClientId = userSessionIssuer.trustedRemoteSessionClientId;
  const linkable = linkedClients.filter(
    (client) => !trustedClientId || client.id !== trustedClientId,
  );
  const prm = useProtectedResourceMetadata(
    remoteMcpServerId,
    remoteMcpServerId !== undefined,
  );
  // One sign-in can serve many services; offer only clients of this one.
  const clientIssuers = useRemoteSessionIssuersByIds([
    ...new Set(linkable.map((client) => client.remoteSessionIssuerId)),
  ]);
  // Only clients of the server's current authorization server can be turned
  // on; a binding on one the metadata no longer names must still turn off.
  const authorizationServers = prm.metadata?.authorizationServers;
  // Offer nothing until every client's authorization server is known.
  const resolvingClients =
    prm.status === "loading" ||
    (!!authorizationServers?.length && clientIssuers.isLoading);
  const candidates = resolvingClients
    ? []
    : clientsForResource(linkable, clientIssuers.items, authorizationServers);
  const staleIssuerIds = resolvingClients
    ? []
    : [
        ...new Set(
          linkable
            .filter((client) => !candidates.includes(client))
            .map((client) => client.remoteSessionIssuerId),
        ),
      ].filter(
        (id) =>
          !candidates.some((client) => client.remoteSessionIssuerId === id),
      );
  // A failed lookup proves nothing about what the metadata names, but a
  // binding on that client must still be possible to turn off.
  const unchecked = (id: string) =>
    !clientIssuers.items.some((issuer) => issuer.id === id);
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
  const staleTargets = staleIssuerIds.map((remoteSessionIssuerId) => ({
    userSessionIssuerId: userSessionIssuer.id,
    remoteSessionIssuerId,
    resource,
  }));
  // One read per issuer that could still hold a binding, a few at a time, so
  // a sign-in with many linked issuers doesn't fire them all at once.
  const staleReads = useQuery({
    queryKey: [
      "identityChaining",
      "readEMA",
      gramProject,
      "stale",
      userSessionIssuer.id,
      resource,
      staleIssuerIds,
    ],
    enabled: staleTargets.length > 0,
    retry: false,
    queryFn: ({ signal }) =>
      mapWithConcurrency(
        staleTargets,
        MAX_CONCURRENT_STALE_READS,
        (staleTarget) =>
          buildRemoteSessionClientsReadEMAMutation(core)
            .mutationFn({
              request: { gramProject, readEMARequestBody: staleTarget },
              options: { fetchOptions: { signal } },
            })
            .catch(() => undefined),
      ),
  });
  const staleBindings = staleTargets.flatMap((staleTarget, i) => {
    const data = staleReads.data?.[i];
    return data && chainingUnlinkable(data)
      ? [
          {
            target: staleTarget,
            generation: data.generation,
            clientLabel:
              linkable.find((client) => client.id === data.clientId)
                ?.clientId ?? data.externalClientId,
          },
        ]
      : [];
  });

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
  // The connection's scopes are edited in Identity; chaining has no list of its own.
  const scopes = bound
    ? sanitizeChainingScopes(storedScopes)
    : defaultChainingScopes(selected?.scope);
  // prepareEMA skips the scope check only when it follows the connection (scopesOutOfDate).
  const scopesValid =
    selected != null &&
    ((bound && read.data?.scopesOutOfDate === true) ||
      chainingScopesAllowed(scopes, selected.scope));
  const [confirmDisable, setConfirmDisable] = useState(false);

  const vendorIssuer = useRemoteSessionIssuer(
    { id: picked?.remoteSessionIssuerId ?? "" },
    undefined,
    { enabled: !!picked, throwOnError: false, retry: false },
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

  const orgRoutes = useOrgRoutes();
  const routes = useRoutes();
  const clientHref = (client: RemoteSessionClient) =>
    routes.remoteIdentityProviders.clientDetail.overview.href(
      client.remoteSessionIssuerId,
      client.id,
    );
  const identityHref = (search: string) =>
    `${orgRoutes.identity.href()}${search}`;
  const prepare = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      if (!preparationSucceeded(result)) {
        setPrepareOutcome(result);
        toast.warning(
          `Enterprise Managed Authorization isn’t on: ${stateCopy(result.state, vendor).label}`,
        );
        return;
      }
      // The prepare result carries no providers or acceptance; keep the read's.
      queryClient.setQueryData<IdentityChainingPreparation>(
        readKey,
        (prev) => ({
          ...result,
          emaProviders: result.emaProviders ?? prev?.emaProviders,
          lastAcceptedAt: prev?.lastAcceptedAt,
        }),
      );
      void invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient);
      // Declared grants are written onto the client row.
      void invalidateAllRemoteSessionClients(queryClient);
      setGrantsConfirmed(false);
      toast.success("Enterprise Managed Authorization is on");
    },
    onError: () => undefined,
    onSettled,
  });
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      void invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient);
      toast.success("Enterprise Managed Authorization is off");
    },
    onError: () => undefined,
    onSettled,
  });

  // Who can vouch for people on this server follows from its sign-in and the
  // organization's Speakeasy issuer; nothing is chosen here.
  const emaProviders = read.data?.emaProviders;
  const listed = (emaProviders?.providers ?? []).filter(
    (provider) =>
      provider.kind === "speakeasy" || !!provider.remoteSessionIssuerId,
  );
  const usable = listed.filter((provider) => !provider.unavailableReason);
  const trustedIssuerId = userSessionIssuer.trustedRemoteSessionIssuerId;
  const issuers = useRemoteSessionIssuersByIds([
    ...(trustedIssuerId ? [trustedIssuerId] : []),
    ...listed.flatMap((provider) =>
      provider.kind === "external" &&
      provider.remoteSessionIssuerId &&
      provider.remoteSessionIssuerId !== trustedIssuerId
        ? [provider.remoteSessionIssuerId]
        : [],
    ),
  ]);
  const issuerById = (id: string | undefined) =>
    issuers.items.find((issuer) => issuer.id === id);
  const orgAdmin = hasScope("org:admin", organization.id);
  // Organization-wide acceptance is withheld from people who can't read the organization.
  const acceptanceKnown = orgAdmin || hasScope("org:read", organization.id);
  const oktaView = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const vendor = serviceName(vendorIssuer.data?.name, resource);
  const signIn = {
    name: trustedIssuerId
      ? issuerDisplayName(issuerById(trustedIssuerId))
      : "Speakeasy’s own sign-in",
    slug: userSessionIssuer.slug,
  };
  const oktaLookup =
    orgAdmin &&
    oktaView.status === "enabled" &&
    listed.some(
      (provider) =>
        provider.kind === "external" && !!provider.remoteSessionIssuerId,
    );
  const connectedOktaId = useOktaIssuerId(oktaLookup);
  const oktaIssuerId = oktaLookup ? connectedOktaId : undefined;
  const allVouchers = listed.map((provider) =>
    providerView(provider, {
      issuer: issuerById(provider.remoteSessionIssuerId),
      speakeasyIssuerUrl,
      oktaIssuerId,
      signInName: trustedIssuerId ? signIn.name : undefined,
    }),
  );
  const usableVouchers = allVouchers.filter((row) => row.usable);
  // A provider this sign-in doesn't go through is noise while another one
  // vouches; when none does, it shows greyed with why.
  const vouchers =
    usableVouchers.length > 0
      ? allVouchers.filter((row) => !row.unusedBySignIn)
      : allVouchers;
  // The service doesn't accept the one provider this sign-in goes through.
  const notAccepted =
    !!emaProviders &&
    !emaProviders.isDefault &&
    !vouchedFor(emaProviders.providers, {
      signInIssuerId: trustedIssuerId || undefined,
    });
  // Speakeasy's issuer URL is withheld without org:read; it still vouches.
  const trusts = usableVouchers;
  const noneUsable = !!emaProviders && usable.length === 0;
  // The vendor already trusts one of these, somewhere in the organization.
  const trusted = trusts.find((row) => row.lastAcceptedAt);

  const enable = () => {
    if (
      !selected ||
      !target ||
      !read.data ||
      !scopesValid ||
      !grantDeclaration ||
      publicClient ||
      noneUsable
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
    prepare.mutate(prepareRequest);
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

  const [applyingScopes, setApplyingScopes] = useState(false);
  const pending =
    read.isFetching || prepare.isPending || unlink.isPending || applyingScopes;
  const ready = bound && read.data?.state === "ready";
  const canWrite = hasScope("project:write", project.id);
  const shownState = read.data ? displayedChainingState(read.data) : null;
  const canUnlink = chainingUnlinkable(read.data);
  const enableBlocked =
    noneUsable ||
    publicClient ||
    !scopesValid ||
    grantsBlocked ||
    (needsGrantConfirm && !grantsConfirmed);

  const status = chainingStatus({
    loading: read.isPending,
    ready,
    // A refused Turn on reports why, even while the binding still reads off.
    off:
      !prepareOutcome &&
      (!read.data || shownState === "unlinked" || switchingClient),
    stalled: ready && noneUsable,
    // This server's own binding, not the organization's.
    lastAccepted: read.data?.lastAcceptedAt ?? undefined,
    trustedName: trusted?.name,
    state: prepareOutcome?.state ?? shownState,
    remediation: prepareOutcome?.remediation ?? read.data?.remediation,
    vendor,
  });

  const lookupNotice =
    clientIssuers.isError && !!authorizationServers?.length ? (
      <Text key="issuer-lookup" muted small className="block">
        Some clients aren’t shown, because Speakeasy couldn’t check their
        authorization server. Reload the page to try again.
      </Text>
    ) : null;
  const staleNotices = [lookupNotice].concat(
    staleBindings.map((binding) => (
      <StaleBinding
        key={binding.target.remoteSessionIssuerId}
        gramProject={gramProject}
        target={binding.target}
        generation={binding.generation}
        clientLabel={binding.clientLabel}
        unchecked={unchecked(binding.target.remoteSessionIssuerId)}
        vendor={vendor}
        projectId={project.id}
      />
    )),
  );

  const error = read.error ?? prepare.error ?? unlink.error;

  if (
    onlyWhenOn &&
    (resolvingClients || (!canUnlink && !staleBindings.length && !error))
  )
    return null;

  if (resolvingClients) {
    return frame(
      <SettingsSection.Body>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-full max-w-md" />
      </SettingsSection.Body>,
    );
  }

  // Who can vouch is unknown, so only turning bindings off is offered.
  if (onlyWhenOn && !canUnlink)
    return frame(
      <SettingsSection.Body>
        <ApiErrorAlert error={error} />
        {staleNotices}
      </SettingsSection.Body>,
    );

  if (candidates.length === 0) {
    return frame(
      <SettingsSection.Body>
        {staleNotices}
        <Text muted small>
          {providers.external
            ? "Link a client for this server’s identity provider in Identity above first."
            : "Link a client registered with this server’s authorization server in Identity above first."}
        </Text>
      </SettingsSection.Body>,
    );
  }

  const clientTone: Tone =
    publicClient || grantsBlocked
      ? "warning"
      : needsGrantConfirm && !ready
        ? "information"
        : "success";

  return frame(
    <>
      <SettingsSection.Body>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0 flex-1 space-y-1">
            <span className="flex items-center gap-2">
              <StatusDot tone={status.tone} />
              <Text className="font-medium">{status.title}</Text>
              {status.aside && (
                <Text muted small className="ml-auto">
                  {status.aside}
                </Text>
              )}
            </span>
            {status.detail && (
              <Text muted small className="block">
                {status.detail}
              </Text>
            )}
            {status.technical && (
              <Collapsible>
                <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
                  <ChevronDown
                    aria-hidden="true"
                    className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
                  />
                  Technical details
                </CollapsibleTrigger>
                <CollapsibleContent className="pt-2">
                  <Text muted small className="block font-mono">
                    {status.technical}
                  </Text>
                </CollapsibleContent>
              </Collapsible>
            )}
          </div>
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
          >
            <div className="flex items-center gap-2">
              {!ready && !onlyWhenOn && (
                <Button
                  size="sm"
                  disabled={pending || !read.data || enableBlocked}
                  onClick={enable}
                >
                  {switchingClient ? "Use this client instead" : "Turn on"}
                </Button>
              )}
              {canUnlink && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={pending}
                  onClick={() => setConfirmDisable(true)}
                >
                  Turn off
                </Button>
              )}
            </div>
          </RequireScope>
        </div>
        {bound && canUnlink && read.data?.scopesOutOfDate && target && (
          <ScopesOutOfDateNotice
            gramProject={gramProject}
            target={target}
            generation={read.data.generation}
            recorded={read.data.scopes}
            connection={read.data.connectionScopes ?? []}
            canApply={ready && canWrite && !grantsBlocked}
            canWrite={canWrite}
            busy={pending}
            onApplyingChange={setApplyingScopes}
          />
        )}
        <ApiErrorAlert error={error} />
        {staleNotices}
      </SettingsSection.Body>

      <ol
        aria-label="How people get in"
        className="border-border divide-border divide-y border-t"
      >
        <ChainLink label="Sign-in" tone="success">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <Text small title={signIn.slug}>
              {signIn.name}
            </Text>
            <SettingsLink to={`#${MCP_SESSIONS_SECTION_ID}`}>
              Change in Sessions
            </SettingsLink>
          </div>
        </ChainLink>

        <ChainLink
          label="Vouched for by"
          tone={usableVouchers.length > 0 ? "success" : "warning"}
        >
          {!emaProviders ? (
            <Text muted small>
              Checking…
            </Text>
          ) : vouchers.length === 0 ? (
            <Text small>
              None yet.{" "}
              {orgAdmin ? (
                <>
                  <Link
                    className="underline underline-offset-4"
                    to={identityHref(identityProvidersHref())}
                  >
                    Set up Speakeasy as an identity provider
                  </Link>
                  , or change this server’s sign-in to an identity provider
                  you’ve connected.
                </>
              ) : (
                "An organization admin can set up Speakeasy as an identity provider."
              )}
            </Text>
          ) : (
            <ul aria-label="Identity providers" className="space-y-1">
              {vouchers.map((row) => (
                <li
                  key={row.key}
                  className="flex flex-wrap items-baseline gap-x-2"
                >
                  <Text
                    small
                    className={cn(
                      "font-medium",
                      !row.usable && "text-muted-foreground",
                    )}
                  >
                    {row.name}
                  </Text>
                  <Text muted small>
                    {row.usable
                      ? row.kind === "speakeasy"
                        ? "for everyone who logs in to this server"
                        : `for people who log in with ${row.name}`
                      : row.unavailable}
                  </Text>
                  {row.fix &&
                    (orgAdmin ? (
                      <SettingsLink
                        to={identityHref(providerFixLink(row.fix).href)}
                      >
                        {providerFixLink(row.fix).label}
                      </SettingsLink>
                    ) : (
                      <Text muted small>
                        An organization admin can fix this.
                      </Text>
                    ))}
                </li>
              ))}
            </ul>
          )}
          {!ready && noneUsable && (
            <Text muted small className="block">
              {NO_VOUCHER}
            </Text>
          )}
          {noneUsable && notAccepted && (
            <Text small className="block">
              To fix it,{" "}
              {orgAdmin ? (
                <Link
                  className="underline underline-offset-4"
                  to={identityHref(
                    emaServiceHref(
                      emaProviders.vendorIssuer,
                      "accepted-providers",
                    ),
                  )}
                >
                  change the identity providers enabled for {vendor}
                </Link>
              ) : (
                `ask an organization admin to change the identity providers enabled for ${vendor}`
              )}{" "}
              or{" "}
              <Link
                className="underline underline-offset-4"
                to={`#${MCP_SESSIONS_SECTION_ID}`}
              >
                change the sign-in in Sessions
              </Link>
              .
            </Text>
          )}
          {emaProviders && usableVouchers.length > 0 && (
            <Text muted small className="block">
              {emaProviders.isDefault ? (
                defaultVoucherLine(trustedIssuerId ? signIn.name : undefined)
              ) : (
                <>
                  Set in{" "}
                  {orgAdmin ? (
                    <Link
                      className="underline underline-offset-4"
                      to={identityHref(
                        emaServiceHref(
                          emaProviders.vendorIssuer,
                          "accepted-providers",
                        ),
                      )}
                    >
                      the identity providers enabled for {vendor}
                    </Link>
                  ) : (
                    `the identity providers enabled for ${vendor}`
                  )}
                  .
                </>
              )}
            </Text>
          )}
        </ChainLink>

        <ChainLink
          label={`${vendor} trusts`}
          tone={
            trusts.length === 0
              ? "neutral"
              : trusts.some((row) => row.lastAcceptedAt)
                ? "success"
                : ready && acceptanceKnown
                  ? "warning"
                  : "neutral"
          }
        >
          {!emaProviders ? (
            <Text muted small>
              Checking…
            </Text>
          ) : trusts.length === 0 ? (
            <Text muted small>
              Nothing to set up at {vendor} yet. First, an identity provider
              must be able to vouch.
            </Text>
          ) : (
            <ul aria-label={`${vendor} trusts`} className="space-y-3">
              {trusts.map((row) => (
                <li
                  key={row.key}
                  className="border-border max-w-2xl space-y-2 border px-4 py-3"
                >
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <StatusDot
                      tone={row.lastAcceptedAt ? "success" : "neutral"}
                      className="translate-y-[-1px]"
                    />
                    <Text small className="font-medium">
                      {row.name}
                    </Text>
                    {(acceptanceKnown || row.lastAcceptedAt) && (
                      <Text muted small>
                        {acceptanceLabel(row.lastAcceptedAt)}
                      </Text>
                    )}
                  </div>
                  {row.kind !== "speakeasy" ? (
                    orgAdmin ? (
                      oktaIssuerId &&
                      row.remoteSessionIssuerId === oktaIssuerId ? (
                        <SettingsLink
                          to={identityHref(oktaViewHref("cross-app-access"))}
                        >
                          {crossAppAccessStep(vendor, row.name)}
                        </SettingsLink>
                      ) : (
                        <Text muted small className="block">
                          {crossAppAccessStep(vendor, row.name)}
                        </Text>
                      )
                    ) : (
                      <Text muted small className="block">
                        An organization admin sets up {vendor} in {row.name}’s
                        Cross App Access.
                      </Text>
                    )
                  ) : !row.issuerUrl ? (
                    !row.lastAcceptedAt && (
                      <Text muted small className="block">
                        An organization admin adds Speakeasy’s issuer URL in{" "}
                        {vendor}’s admin settings.
                      </Text>
                    )
                  ) : row.lastAcceptedAt ? (
                    <Collapsible>
                      <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
                        <ChevronDown
                          aria-hidden="true"
                          className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
                        />
                        Issuer URL
                      </CollapsibleTrigger>
                      <CollapsibleContent className="pt-2">
                        <CopyableValue
                          label={`${row.name}’s issuer URL`}
                          value={row.issuerUrl}
                        />
                      </CollapsibleContent>
                    </Collapsible>
                  ) : (
                    <>
                      <Text muted small className="block">
                        {addIssuerStep(vendor)}
                      </Text>
                      <CopyableValue
                        label={`${row.name}’s issuer URL`}
                        value={row.issuerUrl}
                      />
                    </>
                  )}
                </li>
              ))}
            </ul>
          )}
          {emaProviders && orgAdmin && (
            <SettingsLink
              to={identityHref(
                emaServiceHref(emaProviders.vendorIssuer, "servers"),
              )}
            >
              Every server that connects to {vendor}
            </SettingsLink>
          )}
        </ChainLink>

        <ChainLink label="Remote session client" tone={clientTone}>
          {candidates.length > 1 ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <ClientPicker
                clients={candidates}
                value={selected?.id ?? ""}
                onChange={setSelectedId}
                disabled={prepare.isPending || unlink.isPending}
              />
              {selected && (
                <SettingsLink to={clientHref(selected)}>
                  Manage client
                </SettingsLink>
              )}
            </div>
          ) : (
            selected && (
              <Link
                className="block truncate text-sm underline underline-offset-4"
                to={clientHref(selected)}
              >
                {selected.clientId}
              </Link>
            )
          )}
          {switchingClient && (
            <Text muted small className="block">
              Enterprise Managed Authorization uses client{" "}
              {boundClient?.clientId ?? read.data?.externalClientId} now. Using
              this client replaces it.
            </Text>
          )}
          {publicClient && selected && (
            <Text muted small className="block">
              Client {selected.clientId} is a public client. Enterprise Managed
              Authorization needs a confidential client, one with its own secret
              or key for the authorization server.
            </Text>
          )}
          {!publicClient && grantsBlocked && (
            <Text muted small className="block">
              This client belongs to the organization. An organization admin
              must confirm its grants first.
            </Text>
          )}
          {!ready &&
            !publicClient &&
            needsGrantConfirm &&
            !grantsBlocked &&
            grantDeclaration &&
            selected && (
              <RequireScope
                scope="project:write"
                resourceId={project.id}
                level="component"
              >
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
                      : `Declare that client ${selected.clientId} is registered with the authorization server for these grants, and record them on it: `}
                    {grantDeclaration.grants.join(", ")}
                  </Text>
                </label>
              </RequireScope>
            )}
        </ChainLink>
      </ol>

      <Dialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>
              Turn off Enterprise Managed Authorization?
            </Dialog.Title>
            <Dialog.Description>
              People will sign in to their own {vendor} account the next time
              they use this server.
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
              {unlink.isPending ? "Turning off…" : "Turn off"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>,
  );
}

/** Who vouches on this server by default, in the words the Services page uses. */
function defaultVoucherLine(identityProvider: string | undefined): string {
  return identityProvider
    ? `People log in to this server with ${identityProvider}, so ${identityProvider} vouches for them.`
    : "People log in to this server with Speakeasy, so Speakeasy vouches for them.";
}

function scopeList(scopes: string[]): string {
  return scopes.length ? scopes.join(", ") : "no scopes";
}

/** The server connection's scopes moved on since chaining recorded them. */
function ScopesOutOfDateNotice({
  gramProject,
  target,
  generation,
  recorded,
  connection,
  canApply,
  canWrite,
  busy,
  onApplyingChange,
}: {
  gramProject: string;
  target: {
    userSessionIssuerId: string;
    remoteSessionIssuerId: string;
    resource: string;
  };
  generation: number;
  recorded: string[];
  connection: string[];
  canApply: boolean;
  canWrite: boolean;
  busy: boolean;
  onApplyingChange: (applying: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [conflict, setConflict] = useState(false);
  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ["identityChaining", "readEMA"],
    });
    void invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient);
    void invalidateAllRemoteSessionClients(queryClient);
  };
  const apply = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      // A stale generation comes back as configuration_required, not a 409.
      if (result.state === "configuration_required") {
        setConflict(true);
        void queryClient.invalidateQueries({
          queryKey: ["identityChaining", "readEMA"],
        });
        return;
      }
      if (!preparationSucceeded(result)) {
        toast.warning("Couldn’t apply the connection’s scopes. Try again.");
        refresh();
        return;
      }
      refresh();
      toast.success("Enterprise Managed Authorization uses the new scopes");
    },
    onError: () => undefined,
    onSettled: () => onApplyingChange(false),
  });
  return (
    <div className="space-y-2 pt-2">
      <Text small className="block">
        This server’s connection now asks for {scopeList(connection)}.
        Enterprise Managed Authorization still asks for {scopeList(recorded)}.
      </Text>
      <Text muted small className="block">
        {canApply
          ? "Applying them changes everyone’s next request."
          : canWrite
            ? "Turn it on again to apply them."
            : "Someone who can edit this project can apply them."}
      </Text>
      {conflict && (
        <Text small className="text-warning block">
          Someone else changed this. Review it and try again.
        </Text>
      )}
      {canApply && (
        <Button
          variant="secondary"
          size="sm"
          disabled={busy || apply.isPending}
          onClick={() => {
            setConflict(false);
            onApplyingChange(true);
            apply.mutate({
              request: {
                gramProject,
                prepareEMARequestBody: {
                  ...target,
                  expectedGeneration: generation,
                  scopes: recorded,
                },
              },
            });
          }}
        >
          {apply.isPending ? "Applying…" : "Apply connection scopes"}
        </Button>
      )}
      <ApiErrorAlert error={apply.error} />
    </div>
  );
}

/**
 * A binding on a client of an authorization server this server's metadata no
 * longer names. It can't be turned on again, but it must turn off: the
 * client can't be replaced while the binding holds it.
 */
function StaleBinding({
  gramProject,
  target,
  generation,
  clientLabel,
  unchecked,
  vendor,
  projectId,
}: {
  gramProject: string;
  target: {
    userSessionIssuerId: string;
    remoteSessionIssuerId: string;
    resource: string;
  };
  generation: number;
  clientLabel: string | undefined;
  /** Its authorization server couldn't be checked, so it may still be current. */
  unchecked: boolean;
  vendor: string;
  projectId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      setConfirming(false);
      void invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient);
      void queryClient.invalidateQueries({
        queryKey: ["identityChaining", "readEMA"],
      });
      toast.success(
        "Enterprise Managed Authorization is off for the old client",
      );
    },
    onError: () => undefined,
  });
  return (
    <>
      <Alert variant="warning" alignTop>
        <div className="flex flex-col items-start gap-2">
          <Text small className="block">
            Enterprise Managed Authorization is still on for client{" "}
            {clientLabel ?? "an earlier client"}.{" "}
            {unchecked
              ? "Speakeasy couldn’t check that client’s authorization server. You can still turn it off."
              : "This server no longer uses that client’s authorization server. Turn it off before you replace or remove the client."}
          </Text>
          <RequireScope
            scope="project:write"
            resourceId={projectId}
            level="component"
          >
            <Button
              variant="secondary"
              size="sm"
              disabled={unlink.isPending}
              onClick={() => setConfirming(true)}
            >
              Turn off
            </Button>
          </RequireScope>
        </div>
      </Alert>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Turn off Enterprise Managed Authorization for the old client?"
        description={`People will sign in to their own ${vendor} account until you turn it on for this server’s current client.`}
        confirmLabel="Turn off"
        pendingLabel="Turning off…"
        isPending={unlink.isPending}
        error={
          unlink.error ? <ApiErrorAlert error={unlink.error} /> : undefined
        }
        onConfirm={() =>
          unlink.mutate({
            request: {
              gramProject,
              unlinkEMARequestBody: {
                ...target,
                expectedGeneration: generation,
              },
            },
          })
        }
      />
    </>
  );
}

/** Clients registered with the server's authorization server, when that is known. */
function clientsForResource(
  clients: RemoteSessionClient[],
  issuers: { id: string; issuer: string }[],
  authorizationServers: string[] | undefined,
): RemoteSessionClient[] {
  if (!authorizationServers?.length) return clients;
  const matching = clients.filter((client) => {
    const issuer = issuers.find((i) => i.id === client.remoteSessionIssuerId);
    // Turn on checks the advertised value exactly.
    return !!issuer && authorizationServers.includes(issuer.issuer);
  });
  return matching;
}

function chainingStatus({
  loading,
  ready,
  off,
  stalled,
  lastAccepted,
  trustedName,
  state,
  remediation,
  vendor,
}: {
  loading: boolean;
  ready: boolean;
  off: boolean;
  stalled: boolean;
  /** When the vendor last accepted a sign-in for this server. */
  lastAccepted: Date | undefined;
  /** An identity provider the vendor already accepts elsewhere in the organization. */
  trustedName: string | undefined;
  state: ChainingState | null;
  remediation: string | undefined;
  vendor: string;
}): {
  title: string;
  detail?: string;
  technical?: string;
  aside?: string;
  tone: Tone;
} {
  if (loading) return { title: "Checking…", tone: "neutral" };
  if (stalled)
    return {
      title: "On, but not running",
      detail: NO_VOUCHER,
      tone: "warning",
    };
  if (ready && lastAccepted)
    return {
      title: "Working",
      detail: `People are signed in to ${vendor} automatically.`,
      aside: `Last worked ${formatRelativeTime(lastAccepted)}`,
      tone: "success",
    };
  if (ready)
    return {
      title: "On, waiting for a first connection",
      detail: trustedName
        ? "Everything is set up, but no one has connected through this server yet to confirm it works. Try it from your MCP client."
        : `Speakeasy’s side is set up. Next, ${vendor} must trust an identity provider below. Then try it from your MCP client.`,
      tone: "warning",
    };
  if (off || !state)
    return {
      title: "Off",
      detail: `People sign in to their own ${vendor} account the first time they use this server.`,
      tone: "neutral",
    };
  const copy = stateCopy(state, vendor);
  const technical = [`State: ${state}`, remediation?.trim()]
    .filter(Boolean)
    .join(" · ");
  return {
    title: copy.label,
    detail: copy.next || undefined,
    technical,
    tone: copy.tone,
  };
}
