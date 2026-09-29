import { useSdkClient } from "@/contexts/Sdk";
import type { Gram } from "@gram/client";
import { slugify } from "@/lib/constants";
import {
  deriveRemoteSessionIssuerNameFromUrl,
  remoteSessionScopeTier,
} from "@/lib/sources";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import type { ServerIdentityClientConfiguration } from "@gram/client/models/components/serveridentityclientconfiguration.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import {
  invalidateAllRemoteSessionsCount,
  useRemoteSessionsCount,
} from "@gram/client/react-query/remoteSessionsCount.js";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  preferredScopes,
  serverIdentityAuthMethod,
} from "../model/clientConfiguration";
import { useAllRemoteSessionClients } from "../queries/useAllRemoteSessionClients";
import { useProtectedResourceMetadata } from "../queries/useProtectedResourceMetadata";

/**
 * The provider the upstream advertises but Speakeasy has no record of yet.
 * Selecting it commits `create_provider`, so it carries a sentinel id rather
 * than a row id right up until save.
 */
const DISCOVERED_PROVIDER_ID = "__discovered__";

/** Tier headings, in the order AIM-230 fixes for this flow. */
const TIER_ORDER = ["Platform", "Organization", "Project"] as const;
type ProviderTier = (typeof TIER_ORDER)[number];

export type ProviderOption = {
  id: string;
  name: string;
  /** Host-only rendering of the issuer URL; the full URL is never shown here. */
  url: string;
  /** True for the discovered provider that save will create. */
  isNew: boolean;
  /** Matches the upstream this server points at, so it sorts first. */
  match: boolean;
};

type ProviderGroup = {
  tier: ProviderTier;
  options: ProviderOption[];
};

export type ClientOption = {
  id: string;
  name: string;
  /** A short tail of the provider-issued client_id, to tell clients apart. */
  hint: string | null;
  /** Scopes the client was registered with. Empty: it takes what the provider grants. */
  scopes: string[];
};

/** How a server without a connected client gets one. */
export type RegistrationChoice = "existing" | "auto" | "manual";

/** Which automatic registration to use when the provider offers both. */
export type RegistrationMethod = "cimd" | "dcr";

type UserIdentityStatus =
  | { kind: "idle" }
  | { kind: "pending" }
  | { kind: "done" };

/** What a provider can do on its own, read from its metadata. */
type AutomaticSupport = { cimd: boolean; dcr: boolean };

// Without both endpoints a registration would persist an identity nobody can
// complete a login through, so neither automatic path is on offer.
function automaticSupport(candidate: {
  clientIdMetadataDocumentSupported?: boolean;
  registrationEndpoint?: string | null;
  authorizationEndpoint?: string | null;
  tokenEndpoint?: string | null;
}): AutomaticSupport {
  if (
    !candidate.authorizationEndpoint?.trim() ||
    !candidate.tokenEndpoint?.trim()
  ) {
    return { cimd: false, dcr: false };
  }
  return {
    cimd: !!candidate.clientIdMetadataDocumentSupported,
    dcr: !!candidate.registrationEndpoint?.trim(),
  };
}

const CLIENT_DATE = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
});

// A client_id is whatever the provider issued — a metadata URL for CIMD, an
// opaque string otherwise — and means nothing to an operator picking between
// clients. Name a client by how it came to exist and when; a short tail of a
// non-CIMD client_id is kept only to tell two clients apart.
function clientOptionName(candidate: RemoteSessionClient): string {
  const created = CLIENT_DATE.format(candidate.createdAt);
  return candidate.clientIdMetadataUri
    ? `Automatic client · created ${created}`
    : `Client created ${created}`;
}

function clientOptionHint(candidate: RemoteSessionClient): string | null {
  if (candidate.clientIdMetadataUri) return null;
  const id = candidate.clientId;
  return id.length > 8 ? `…${id.slice(-6)}` : id;
}

function scopesFromText(text: string): string[] {
  return [...new Set(text.split(/\s+/).filter((scope) => scope !== ""))];
}

// A provider counts as this upstream's own only on a DNS-label boundary. A
// bare suffix test would read notgithub.com as github.com and hand that
// unrelated upstream the provider's tokens.
function sameSite(issuerHost: string, upstreamHost: string): boolean {
  if (issuerHost === "" || upstreamHost === "") return false;
  return issuerHost === upstreamHost || upstreamHost.endsWith(`.${issuerHost}`);
}

// The probe only runs up front while a provider is being discovered. A server
// that already matched one still needs the resource's scopes at save time, so
// read them then. A failed probe is not fatal: the issuer's list is the
// fallback preferredScopes already defines.
async function protectedResourceScopes(
  client: Gram,
  remoteMcpServerId: string,
): Promise<string[] | undefined> {
  if (!remoteMcpServerId) return undefined;
  try {
    const result = await client.remoteMcp.discoverProtectedResourceMetadata({
      discoverProtectedResourceMetadataRequestBody: { remoteMcpServerId },
    });
    return result.available ? result.metadata?.scopesSupported : undefined;
  } catch {
    return undefined;
  }
}

function hostOf(url: string | undefined | null): string {
  if (!url) return "";
  try {
    return new URL(url).host.toLowerCase();
  } catch {
    return url
      .replace(/^https?:\/\//, "")
      .replace(/\/.*$/, "")
      .toLowerCase();
  }
}

/** Host plus path, minus the scheme — how the mock renders an issuer URL. */
function displayUrl(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

function issuerDisplayName(issuer: RemoteSessionIssuer): string {
  return (
    issuer.name?.trim() ||
    deriveRemoteSessionIssuerNameFromUrl(issuer.issuer) ||
    issuer.slug
  );
}

const TIER_BY_SCOPE = {
  platform: "Platform",
  organization: "Organization",
  project: "Project",
} as const satisfies Record<string, ProviderTier>;

/** Everything the User Identity row renders and everything it can change. */
export type UserIdentityDraft = {
  providerGroups: ProviderGroup[];
  selected: ProviderOption | null;
  selectProvider: (id: string | null) => void;
  /** The upstream advertised no provider and none matched. */
  providerUnreachable: boolean;
  providerLoading: boolean;

  clientsLoading: boolean;
  /** A discovered provider's capabilities are still being read. */
  capabilitiesLoading: boolean;

  /** The server has a client and the operator has not cleared it. */
  connected: boolean;
  /** The connected client, once the provider's client list has loaded. */
  connectedClient: ClientOption | null;
  /** People signed in through the connected client; null while unknown. */
  signedIn: number | null;
  /** The server had a client and the operator cleared it. Save replaces it. */
  cleared: boolean;
  clear: () => void;
  /** Back out of clearing and keep the connected client. */
  cancelClear: () => void;

  choice: RegistrationChoice;
  selectChoice: (choice: RegistrationChoice) => void;
  existingAvailable: boolean;
  automaticAvailable: boolean;
  existingOptions: ClientOption[];
  existingClientId: string | null;
  selectExisting: (id: string) => void;
  /** The chosen existing client is the one already connected, so Save would change nothing. */
  sameAsConnected: boolean;

  /** The provider supports both CIMD and DCR, so the operator may pick one. */
  methodChoiceAvailable: boolean;
  registrationMethod: RegistrationMethod;
  setRegistrationMethod: (method: RegistrationMethod) => void;

  clientId: string;
  setClientId: (value: string) => void;
  clientSecret: string;
  setClientSecret: (value: string) => void;
  /** Space-separated scopes for a manual client. Blank requests the defaults. */
  scopeText: string;
  setScopeText: (value: string) => void;
  registrationGuideUrl: string | null;

  /** Save swaps the server's client for another, so everyone signs in again. */
  replacesClient: boolean;
  status: UserIdentityStatus;
  canSave: boolean;
  save: () => Promise<void>;
  saving: boolean;
};

/**
 * Owns the User Identity choice for one Remote MCP-backed server: which
 * Remote Identity Provider, which OAuth client, and the single commit that
 * writes both. The issuer/client split stays inside here — callers see one
 * provider and one registration choice, per AIM-230.
 *
 * A server with a client shows it as connected. Clearing it opens the choice
 * of an existing client, automatic registration or manual credentials; nothing
 * changes until Save.
 */
export function useUserIdentityDraft({
  mcpServerId,
  remoteMcpServerId,
  upstreamUrl,
  issuers,
  linkedClients,
  configured,
  enabled,
}: {
  mcpServerId: string;
  remoteMcpServerId: string;
  upstreamUrl: string | undefined;
  issuers: RemoteSessionIssuer[];
  linkedClients: RemoteSessionClient[];
  configured: boolean;
  enabled: boolean;
}): UserIdentityDraft {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  const [providerPick, setProviderPick] = useState<string | null>(null);
  const [cleared, setCleared] = useState(false);
  // null: follow the default for whatever the provider offers.
  const [choicePick, setChoicePick] = useState<RegistrationChoice | null>(null);
  const [existingPick, setExistingPick] = useState<string | null>(null);
  const [registrationMethod, setRegistrationMethod] =
    useState<RegistrationMethod>("cimd");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [scopeText, setScopeText] = useState("");
  const [localStatus, setLocalStatus] = useState<UserIdentityStatus>({
    kind: "idle",
  });

  // Only probe for a provider to create when none of the existing ones match;
  // a configured server already has its answer.
  const upstreamHost = hostOf(upstreamUrl);
  const matchedIssuer = useMemo(
    () =>
      issuers.find((issuer) => sameSite(hostOf(issuer.issuer), upstreamHost)),
    [issuers, upstreamHost],
  );
  const linkedIssuerId = linkedClients[0]?.remoteSessionIssuerId;
  const prm = useProtectedResourceMetadata(
    remoteMcpServerId,
    enabled && !configured && !matchedIssuer && !linkedIssuerId,
  );
  const discoveredIssuerUrl = prm.metadata?.authorizationServers?.[0] ?? null;
  const discoveredIsKnown =
    !!discoveredIssuerUrl &&
    issuers.some(
      (issuer) => hostOf(issuer.issuer) === hostOf(discoveredIssuerUrl),
    );
  const discovered = useMemo<ProviderOption | null>(
    () =>
      discoveredIssuerUrl && !discoveredIsKnown
        ? {
            id: DISCOVERED_PROVIDER_ID,
            name:
              deriveRemoteSessionIssuerNameFromUrl(discoveredIssuerUrl) ??
              displayUrl(discoveredIssuerUrl),
            url: displayUrl(discoveredIssuerUrl),
            isNew: true,
            match: true,
          }
        : null,
    [discoveredIssuerUrl, discoveredIsKnown],
  );

  // Nothing matched and nothing was advertised: the upstream could not tell us
  // where its users sign in, so the chooser opens empty.
  const providerUnreachable =
    enabled &&
    !configured &&
    !matchedIssuer &&
    !linkedIssuerId &&
    !discovered &&
    prm.status === "unavailable";

  const defaultProviderId =
    linkedIssuerId ?? matchedIssuer?.id ?? discovered?.id ?? null;
  const selectedProviderId = providerPick ?? defaultProviderId;
  const selectedIssuer = issuers.find(
    (issuer) => issuer.id === selectedProviderId,
  );
  const selectedDiscovered =
    selectedProviderId === DISCOVERED_PROVIDER_ID ? discovered : null;
  const selected: ProviderOption | null = selectedIssuer
    ? {
        id: selectedIssuer.id,
        name: issuerDisplayName(selectedIssuer),
        url: displayUrl(selectedIssuer.issuer),
        isNew: false,
        match: selectedIssuer.id === matchedIssuer?.id,
      }
    : selectedDiscovered;

  const providerGroups = useMemo<ProviderGroup[]>(() => {
    const groups = new Map<ProviderTier, ProviderOption[]>(
      TIER_ORDER.map((tier) => [tier, []]),
    );
    for (const issuer of issuers) {
      const tier = TIER_BY_SCOPE[remoteSessionScopeTier(issuer)];
      groups.get(tier)?.push({
        id: issuer.id,
        name: issuerDisplayName(issuer),
        url: displayUrl(issuer.issuer),
        isNew: false,
        match: issuer.id === matchedIssuer?.id,
      });
    }
    if (discovered) groups.get("Project")?.push(discovered);
    return TIER_ORDER.map((tier) => ({
      tier,
      // URL matches first, then alphabetical, so the provider this server
      // actually points at is never buried in a long platform list.
      options: [...(groups.get(tier) ?? [])].sort(
        (a, b) =>
          Number(b.match) - Number(a.match) || a.name.localeCompare(b.name),
      ),
    }));
  }, [issuers, matchedIssuer, discovered]);

  // Existing clients live on the provider; a provider that does not exist yet
  // has none to offer.
  const { items: providerClients, isLoading: clientsLoading } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: selectedIssuer?.id ?? "" },
      { enabled: enabled && !!selectedIssuer },
    );
  const clientOptions = useMemo<ClientOption[]>(
    () =>
      providerClients.map((candidate) => ({
        id: candidate.id,
        name: clientOptionName(candidate),
        hint: clientOptionHint(candidate),
        scopes: candidate.scope ?? [],
      })),
    [providerClients],
  );
  const linkedClientId =
    linkedClients.find(
      (candidate) => candidate.remoteSessionIssuerId === selectedProviderId,
    )?.id ?? null;
  const connected = !!linkedClientId && !cleared;
  const connectedClient = connected
    ? (clientOptions.find((candidate) => candidate.id === linkedClientId) ??
      null)
    : null;
  const signedInQuery = useRemoteSessionsCount(
    { remoteSessionClientId: linkedClientId ?? "" },
    undefined,
    { enabled: enabled && connected, throwOnError: false },
  );
  const signedIn = connected ? (signedInQuery.data?.subjects ?? null) : null;

  // A provider we have no record of has published no capabilities either, so
  // read its metadata before promising anything. Plenty of real upstreams —
  // GitHub among them — publish neither a registration endpoint nor a CIMD
  // document and can only be set up by hand.
  const discoveredMetadataQuery = useQuery({
    queryKey: ["remote-session-issuer-metadata", discoveredIssuerUrl],
    queryFn: async () => {
      if (!discoveredIssuerUrl) throw new Error("no discovered issuer");
      return await client.remoteSessionIssuers.fetchMetadata({
        fetchIssuerMetadataRequestBody: { issuer: discoveredIssuerUrl },
      });
    },
    enabled: enabled && !!selectedDiscovered && !!discoveredIssuerUrl,
    throwOnError: false,
    retry: false,
    staleTime: 5 * 60 * 1000,
  });
  const discoveredMetadata = selectedDiscovered
    ? (discoveredMetadataQuery.data ?? null)
    : null;
  // Nothing is claimed while the answer is outstanding.
  const capabilitiesLoading =
    !!selectedDiscovered && discoveredMetadataQuery.isLoading;

  let support: AutomaticSupport = { cimd: false, dcr: false };
  if (selectedDiscovered && discoveredMetadata) {
    support = automaticSupport(discoveredMetadata);
  } else if (selectedIssuer) {
    support = automaticSupport(selectedIssuer);
  }
  const automaticAvailable = support.cimd || support.dcr;
  const methodChoiceAvailable = support.cimd && support.dcr;
  const existingAvailable = clientOptions.length > 0;

  // Reusing a client beats registering another, and registering beats asking
  // for credentials by hand. A pick the provider no longer allows falls back.
  let defaultChoice: RegistrationChoice = "manual";
  if (existingAvailable) defaultChoice = "existing";
  else if (automaticAvailable) defaultChoice = "auto";
  let choice: RegistrationChoice = choicePick ?? defaultChoice;
  if (choice === "existing" && !existingAvailable) choice = defaultChoice;
  if (choice === "auto" && !automaticAvailable) choice = defaultChoice;

  const existingClient =
    choice === "existing"
      ? (clientOptions.find((candidate) => candidate.id === existingPick) ??
        clientOptions[0] ??
        null)
      : null;
  const sameAsConnected =
    !!existingClient && existingClient.id === linkedClientId;
  const manualNeeded = choice === "manual";
  // Any client this server holds now is replaced by the one Save picks, and the
  // people signed in through the old one have to sign in again.
  const replacesClient =
    linkedClients.length > 0 && !connected && !sameAsConnected;

  const resetChoice = (): void => {
    setChoicePick(null);
    setExistingPick(null);
    setRegistrationMethod("cimd");
    // Manual credentials are issued by one provider and meaningless to the
    // next, so they leave with it rather than being saved under its successor.
    setClientId("");
    setClientSecret("");
    setScopeText("");
    setLocalStatus({ kind: "idle" });
  };

  // A client belongs to exactly one provider, so choosing a provider clears
  // the client choice and any outcome from the previous one.
  const selectProvider = (id: string | null): void => {
    setProviderPick(id);
    resetChoice();
  };

  const commit = useMutation({
    mutationFn: async () => {
      if (!selected) throw new Error("choose an identity provider");

      // A discovered provider is created from what the upstream publishes, so
      // read its metadata now and send the whole record with the commit.
      let createProvider = undefined;
      let providerId: string | undefined = selectedIssuer?.id;
      let draft: RemoteSessionIssuerDraft | null = null;
      if (selectedDiscovered && discoveredIssuerUrl) {
        draft =
          discoveredMetadata ??
          (await client.remoteSessionIssuers.fetchMetadata({
            fetchIssuerMetadataRequestBody: { issuer: discoveredIssuerUrl },
          }));
        providerId = undefined;
        createProvider = {
          issuer: discoveredIssuerUrl,
          slug: slugify(selectedDiscovered.name) || "identity-provider",
          name: selectedDiscovered.name,
          authorizationEndpoint: draft.authorizationEndpoint ?? undefined,
          tokenEndpoint: draft.tokenEndpoint ?? undefined,
          registrationEndpoint: draft.registrationEndpoint ?? undefined,
          serviceDocumentation: draft.serviceDocumentation ?? undefined,
          jwksUri: draft.jwksUri ?? undefined,
          scopesSupported: draft.scopesSupported ?? undefined,
          grantTypesSupported: draft.grantTypesSupported ?? undefined,
          responseTypesSupported: draft.responseTypesSupported ?? undefined,
          tokenEndpointAuthMethodsSupported:
            draft.tokenEndpointAuthMethodsSupported ?? undefined,
          codeChallengeMethodsSupported:
            draft.codeChallengeMethodsSupported ?? undefined,
          clientIdMetadataDocumentSupported:
            draft.clientIdMetadataDocumentSupported,
          // The rest of the document. Callback validation and enrichment read
          // these later, and the create path in configureCreatedIdentity
          // already forwards them — a provider should not come out different
          // depending on which surface created it.
          userinfoEndpoint: draft.userinfoEndpoint ?? undefined,
          introspectionEndpoint: draft.introspectionEndpoint ?? undefined,
          introspectionEndpointAuthMethodsSupported:
            draft.introspectionEndpointAuthMethodsSupported ?? undefined,
          idTokenSigningAlgValuesSupported:
            draft.idTokenSigningAlgValuesSupported ?? undefined,
          claimsSupported: draft.claimsSupported ?? undefined,
          backchannelLogoutSupported: draft.backchannelLogoutSupported,
          authorizationResponseIssParameterSupported:
            draft.authorizationResponseIssParameterSupported,
          oidc: draft.oidc,
          passthrough: draft.passthrough,
        };
      }

      let clientConfiguration: ServerIdentityClientConfiguration | undefined;
      if (choice !== "existing") {
        // A new client asks for what this server's protected resource
        // advertises, exactly as the create flow does. Left empty, the server
        // falls back to every scope the issuer advertises — the request that
        // broke Salesforce logins. Scopes typed for a manual client win.
        const typedScopes = manualNeeded ? scopesFromText(scopeText) : [];
        const scopes =
          typedScopes.length > 0
            ? typedScopes
            : preferredScopes(
                prm.metadata?.scopesSupported ??
                  (await protectedResourceScopes(client, remoteMcpServerId)),
                selectedIssuer?.scopesSupported ?? draft?.scopesSupported,
              );
        const secret = manualNeeded ? clientSecret.trim() : "";
        clientConfiguration = {
          clientId: manualNeeded ? clientId.trim() : undefined,
          clientSecret: secret || undefined,
          scope: scopes.length > 0 ? scopes : undefined,
          // A manual client without a secret is a public client; naming a
          // secret-based method for it is refused by the server.
          tokenEndpointAuthMethod:
            manualNeeded && !secret
              ? "none"
              : serverIdentityAuthMethod(
                  selectedIssuer?.tokenEndpointAuthMethodsSupported ??
                    draft?.tokenEndpointAuthMethodsSupported ??
                    [],
                ),
        };
      }

      return await client.remoteSessions.commitServerIdentityConfiguration({
        commitServerIdentityConfigurationForm: {
          mcpServerId,
          providerId,
          createProvider,
          clientMode: choice,
          existingClientId: existingClient?.id,
          clientConfiguration,
          // Only sent when it changes something: CIMD is the server default.
          registrationMethod:
            choice === "auto" && methodChoiceAvailable
              ? registrationMethod
              : undefined,
        },
      });
    },
    onSuccess: async (result) => {
      const providerName = selected?.name ?? "The provider";
      if (result.failure) {
        setLocalStatus({ kind: "idle" });
        toast.error(
          result.failure.providerMessage ??
            (result.failure.outcome === "refused"
              ? `${providerName} refused to register this server.`
              : `Couldn't reach ${providerName} to register this server. Save again to retry.`),
        );
        return;
      }
      if (result.manualSetupRequired) {
        // Not a registration failure: nothing was written, and the operator
        // needs to paste credentials the provider issued out of band.
        setChoicePick("manual");
        setLocalStatus({ kind: "idle" });
        toast.warning(
          `${providerName} can't register this server on its own. Enter the credentials it issued.`,
        );
        return;
      }
      setCleared(false);
      resetChoice();
      setLocalStatus({ kind: "done" });
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllRemoteSessionIssuers(queryClient),
        invalidateAllRemoteSessionsCount(queryClient),
      ]);
    },
    onError: (error: unknown) => {
      // The reachable case is an operator with mcp:write but not
      // project:write: the commit needs project:write to create or register a
      // client, so the button is enabled and the request is refused.
      setLocalStatus({ kind: "idle" });
      toast.error(
        error instanceof Error ? error.message : "Failed to save identity",
      );
    },
  });

  const { mutateAsync: runCommit, isPending } = commit;

  // Dirty means the selection differs from what the server holds — not that
  // the operator opened a menu.
  const touched = selectedProviderId !== defaultProviderId || cleared;
  let status: UserIdentityStatus = localStatus;
  if (isPending) {
    status = { kind: "pending" };
  } else if (localStatus.kind === "idle" && configured && !touched) {
    status = { kind: "done" };
  }

  let choiceComplete = true;
  if (choice === "existing")
    choiceComplete = !!existingClient && !sameAsConnected;
  if (choice === "manual") choiceComplete = clientId.trim() !== "";

  const canSave =
    !!selected &&
    !connected &&
    !capabilitiesLoading &&
    // Saving before the client list lands would miss an existing client and
    // register a duplicate in its place.
    !clientsLoading &&
    !isPending &&
    status.kind !== "done" &&
    choiceComplete;

  return {
    providerGroups,
    selected,
    selectProvider,
    providerUnreachable,
    providerLoading: prm.status === "loading",

    clientsLoading,
    capabilitiesLoading,

    connected,
    connectedClient,
    signedIn,
    cleared,
    clear: (): void => {
      setCleared(true);
      resetChoice();
    },
    cancelClear: (): void => {
      setCleared(false);
      resetChoice();
    },

    choice,
    selectChoice: (next: RegistrationChoice): void => {
      setChoicePick(next);
      setLocalStatus({ kind: "idle" });
    },
    existingAvailable,
    automaticAvailable,
    existingOptions: clientOptions,
    existingClientId: existingClient?.id ?? null,
    selectExisting: setExistingPick,
    sameAsConnected,

    methodChoiceAvailable,
    registrationMethod,
    setRegistrationMethod,

    clientId,
    setClientId,
    clientSecret,
    setClientSecret,
    scopeText,
    setScopeText,
    registrationGuideUrl:
      selectedIssuer?.clientSetupDocumentationUrl ??
      selectedIssuer?.serviceDocumentation ??
      discoveredMetadata?.serviceDocumentation ??
      null,

    replacesClient,
    status,
    canSave,
    save: async (): Promise<void> => {
      // Awaitable so callers can sequence work after it. onError has already
      // put the failure on screen, so the rejection is swallowed here rather
      // than surfacing twice or escaping as an unhandled rejection.
      try {
        await runCommit();
      } catch {
        /* reported by onError */
      }
    },
    saving: isPending,
  };
}
