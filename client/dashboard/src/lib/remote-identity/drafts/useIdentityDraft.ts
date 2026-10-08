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
import { invalidateAllGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateSharedIssuerImpact } from "@/lib/remote-identity/model/upstreamRepointing";
import { queryKeyRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import {
  invalidateAllRemoteSessionIssuers,
  useRemoteSessionIssuers,
  useRemoteSessionIssuersInfinite,
} from "@gram/client/react-query/remoteSessionIssuers.js";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import {
  invalidateAllRemoteSessionsCount,
  useRemoteSessionsCount,
} from "@gram/client/react-query/remoteSessionsCount.js";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  advertisedScopes,
  normalizeScopes,
  preferredScopes,
  serverIdentityAuthMethod,
} from "../model/clientConfiguration";
import { useAllRemoteSessionClients } from "../queries/useAllRemoteSessionClients";
import { useProtectedResourceMetadata } from "../queries/useProtectedResourceMetadata";
import { useRemoteSessionIssuersByIds } from "../queries/useRemoteSessionIssuersByIds";

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

export type ProviderGroup = {
  tier: ProviderTier;
  options: ProviderOption[];
  /** The tier's first page is still loading. */
  isLoading: boolean;
  isError: boolean;
  /** More of this tier (for the current search) lies past what has loaded. */
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
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

const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

// The server accepts provider endpoints only as absolute https URLs, or http
// on loopback (urls.IsAbsoluteHTTPSOrLoopback); offering automatic setup for
// anything else would only fail at Save.
function secureEndpoint(endpoint: string | null | undefined): boolean {
  const trimmed = endpoint?.trim();
  if (!trimmed) return false;
  try {
    const url = new URL(trimmed);
    if (url.protocol === "https:") return true;
    return url.protocol === "http:" && LOOPBACK_HOSTS.has(url.hostname);
  } catch {
    return false;
  }
}

// Mirrors the server's registration choice so the cards never offer a path
// Save would refuse. Without both OAuth endpoints a registration would
// persist an identity nobody can complete a login through. CIMD needs the
// provider to accept public clients (`none`, or no methods listed), as
// supportsCIMD does server-side.
function automaticSupport(candidate: {
  clientIdMetadataDocumentSupported?: boolean;
  registrationEndpoint?: string | null;
  authorizationEndpoint?: string | null;
  tokenEndpoint?: string | null;
  tokenEndpointAuthMethodsSupported?: string[] | null;
}): AutomaticSupport {
  if (
    !secureEndpoint(candidate.authorizationEndpoint) ||
    !secureEndpoint(candidate.tokenEndpoint)
  ) {
    return { cimd: false, dcr: false };
  }
  const methods = candidate.tokenEndpointAuthMethodsSupported ?? [];
  return {
    cimd:
      !!candidate.clientIdMetadataDocumentSupported &&
      (methods.length === 0 || methods.includes("none")),
    dcr: secureEndpoint(candidate.registrationEndpoint),
  };
}

const CLIENT_DATE = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
});

// A client_id is whatever the provider issued — a metadata URL for CIMD, an
// opaque string otherwise — and means nothing to an operator picking between
// clients. Name a client by how it came to exist and when.
function clientOptionName(candidate: RemoteSessionClient): string {
  const created = CLIENT_DATE.format(candidate.createdAt);
  return candidate.clientIdMetadataUri
    ? `Automatic client · created ${created}`
    : `Client created ${created}`;
}

function idTail(id: string): string {
  return id.length > 8 ? `…${id.slice(-6)}` : id;
}

// A short tail tells clients apart: a non-CIMD client's own client_id always,
// and a CIMD client's row id only when another client shares its label.
function clientOptionHint(
  candidate: RemoteSessionClient,
  labelShared: boolean,
): string | null {
  if (!candidate.clientIdMetadataUri) return idTail(candidate.clientId);
  return labelShared ? idTail(candidate.id) : null;
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

type ProviderTierQuery = {
  issuers: RemoteSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
};

// One tier of the provider menu, searched and paged on the server.
function useProviderTier(
  tier: "project" | "organization" | "platform",
  search: string,
  enabled: boolean,
): ProviderTierQuery {
  const query = useRemoteSessionIssuersInfinite(
    { tier, search: search || undefined },
    undefined,
    { enabled, throwOnError: false },
  );
  const { fetchNextPage } = query;
  const issuers = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );
  const loadMore = useCallback(() => void fetchNextPage(), [fetchNextPage]);
  const { isLoading, isError, hasNextPage, isFetchingNextPage } = query;
  return useMemo(
    () => ({
      issuers,
      isLoading,
      isError,
      hasMore: !!hasNextPage,
      loadingMore: isFetchingNextPage,
      loadMore,
    }),
    [issuers, isLoading, isError, hasNextPage, isFetchingNextPage, loadMore],
  );
}

// Issuers whose host is `host` or one of its parent domains, from every tier
// the project can see. Off for an empty host.
function useIssuersForHost(
  host: string,
  enabled: boolean,
): { issuers: RemoteSessionIssuer[]; isLoading: boolean; isError: boolean } {
  const query = useRemoteSessionIssuers({ upstreamHost: host }, undefined, {
    enabled: enabled && host !== "",
    throwOnError: false,
  });
  return {
    issuers: query.data?.result.items ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
  };
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
  /** Text the provider menu searches on the server; "" lists every tier. */
  providerSearch: string;
  setProviderSearch: (search: string) => void;
  selected: ProviderOption | null;
  selectProvider: (id: string | null) => void;
  /** The upstream advertised no provider and none matched. */
  providerUnreachable: boolean;
  providerLoading: boolean;
  /** A provider lookup failed, so the choices may be incomplete. */
  providerLoadFailed: boolean;

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
  /** Scopes chosen for a manual client. Empty requests the defaults. */
  scopes: string[];
  setScopes: (values: string[]) => void;
  /** Scopes the server and provider advertise, offered as choices. */
  scopeOptions: string[];
  registrationGuideUrl: string | null;

  /** Save swaps the server's client for another, so everyone signs in again. */
  replacesClient: boolean;
  status: UserIdentityStatus;
  canSave: boolean;
  /** The operator changed the selection, whether or not it can be saved yet. */
  pendingChange: boolean;
  /** Resolves true when the commit landed; failures are already on screen. */
  save: () => Promise<boolean>;
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
  linkedClients,
  configured,
  enabled,
}: {
  mcpServerId: string;
  remoteMcpServerId: string;
  upstreamUrl: string | undefined;
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
  const [scopes, setScopes] = useState<string[]>([]);
  const [localStatus, setLocalStatus] = useState<UserIdentityStatus>({
    kind: "idle",
  });

  // The menu lists each tier on its own, searched and paged on the server:
  // the platform catalog is far larger than any page, and a single mixed
  // listing lets it crowd the project's own providers out entirely.
  const [providerSearch, setProviderSearch] = useState("");
  const debouncedSearch = useDebouncedValue(providerSearch.trim(), 250);
  const platformTier = useProviderTier("platform", debouncedSearch, enabled);
  const organizationTier = useProviderTier(
    "organization",
    debouncedSearch,
    enabled,
  );
  const projectTier = useProviderTier("project", debouncedSearch, enabled);

  // The linked and picked providers are resolved by id and merged in, never
  // looked for in the menu's pages alone: a search or a short page can leave
  // either out, and the server would then render with no provider selected.
  const linkedIssuerId = linkedClients[0]?.remoteSessionIssuerId;
  const pickedIssuerId =
    providerPick && providerPick !== DISCOVERED_PROVIDER_ID
      ? providerPick
      : undefined;
  const resolvedIssuerIds = useMemo(
    () =>
      [...new Set([linkedIssuerId, pickedIssuerId])].filter(
        (id): id is string => !!id,
      ),
    [linkedIssuerId, pickedIssuerId],
  );
  const {
    items: linkedIssuers,
    isLoading: linkedIssuerLoading,
    isError: linkedIssuerError,
  } = useRemoteSessionIssuersByIds(resolvedIssuerIds, { enabled });

  // The provider this server's upstream points at, found on the server: the
  // same-site rule (the upstream's host or a parent domain of it) runs over
  // every issuer the project can see, not just a loaded page. Newest first,
  // as the listing is.
  const upstreamHost = hostOf(upstreamUrl);
  const hostMatchQuery = useIssuersForHost(upstreamHost, enabled);
  const matchedIssuer = hostMatchQuery.issuers.find((issuer) =>
    sameSite(hostOf(issuer.issuer), upstreamHost),
  );

  // Only probe for a provider to create when none of the existing ones match;
  // a configured server already has its answer.
  const prm = useProtectedResourceMetadata(
    remoteMcpServerId,
    enabled &&
      !configured &&
      !hostMatchQuery.isLoading &&
      !matchedIssuer &&
      !linkedIssuerId,
  );
  const discoveredIssuerUrl = prm.metadata?.authorizationServers?.[0] ?? null;
  // An advertised provider is new only when no issuer the project can see
  // shares its host. Held (not offered) until that lookup answers.
  const discoveredHost = hostOf(discoveredIssuerUrl);
  const discoveredQuery = useIssuersForHost(discoveredHost, enabled);
  const knownDiscoveredIssuer = discoveredQuery.issuers.find(
    (issuer) => hostOf(issuer.issuer) === discoveredHost,
  );
  const discoveredIsKnown = !!knownDiscoveredIssuer;

  // Every issuer the chooser may need to name: the loaded menu pages, plus
  // the linked, picked and matched ones resolved above, which may sit past
  // them.
  const issuers = useMemo(() => {
    const merged = new Map<string, RemoteSessionIssuer>();
    for (const issuer of [
      ...platformTier.issuers,
      ...organizationTier.issuers,
      ...projectTier.issuers,
      ...linkedIssuers,
      ...(matchedIssuer ? [matchedIssuer] : []),
      ...(knownDiscoveredIssuer ? [knownDiscoveredIssuer] : []),
    ]) {
      if (!merged.has(issuer.id)) merged.set(issuer.id, issuer);
    }
    return [...merged.values()];
  }, [
    platformTier.issuers,
    organizationTier.issuers,
    projectTier.issuers,
    linkedIssuers,
    matchedIssuer,
    knownDiscoveredIssuer,
  ]);

  // A lookup that failed is not an answer: offering a provider as new, or
  // picking none, on a failed lookup could duplicate or hide one the project
  // already has. The row reports the failure and Save stays off instead.
  const providerLoadFailed =
    linkedIssuerError || hostMatchQuery.isError || discoveredQuery.isError;

  const discovered = useMemo<ProviderOption | null>(
    () =>
      discoveredIssuerUrl &&
      !discoveredQuery.isLoading &&
      !discoveredQuery.isError &&
      !discoveredIsKnown
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
    [
      discoveredIssuerUrl,
      discoveredQuery.isLoading,
      discoveredQuery.isError,
      discoveredIsKnown,
    ],
  );

  // Nothing matched and nothing was advertised: the upstream could not tell us
  // where its users sign in, so the chooser opens empty.
  // Not claimed while our own lookup failed: that alert already explains the
  // missing provider, and the upstream may be fine.
  const providerUnreachable =
    enabled &&
    !configured &&
    !providerLoadFailed &&
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

  const searching = debouncedSearch !== "";
  const providerGroups = useMemo<ProviderGroup[]>(() => {
    // While searching, a tier shows only its search results. Otherwise the
    // matched provider, the one in use, the existing one the upstream
    // advertises, and a discovered one to create are always offered, whether
    // or not the tier's loaded pages reach them.
    const pinned = searching
      ? []
      : [matchedIssuer, selectedIssuer, knownDiscoveredIssuer].filter(
          (issuer): issuer is RemoteSessionIssuer => !!issuer,
        );
    const tierQueries = {
      Platform: platformTier,
      Organization: organizationTier,
      Project: projectTier,
    } satisfies Record<ProviderTier, ProviderTierQuery>;
    return TIER_ORDER.map((tier) => {
      const query = tierQueries[tier];
      const byId = new Map(query.issuers.map((issuer) => [issuer.id, issuer]));
      for (const issuer of pinned) {
        if (TIER_BY_SCOPE[remoteSessionScopeTier(issuer)] === tier) {
          byId.set(issuer.id, issuer);
        }
      }
      const options: ProviderOption[] = [...byId.values()].map((issuer) => ({
        id: issuer.id,
        name: issuerDisplayName(issuer),
        url: displayUrl(issuer.issuer),
        isNew: false,
        match: issuer.id === matchedIssuer?.id,
      }));
      if (tier === "Project" && discovered && !searching) {
        options.push(discovered);
      }
      return {
        tier,
        // URL matches first, then alphabetical, so the provider this server
        // actually points at is never buried in a long platform list.
        options: options.sort(
          (a, b) =>
            Number(b.match) - Number(a.match) || a.name.localeCompare(b.name),
        ),
        isLoading: query.isLoading,
        isError: query.isError,
        hasMore: query.hasMore,
        loadingMore: query.loadingMore,
        loadMore: query.loadMore,
      };
    });
  }, [
    searching,
    matchedIssuer,
    selectedIssuer,
    knownDiscoveredIssuer,
    discovered,
    platformTier,
    organizationTier,
    projectTier,
  ]);

  // Existing clients live on the provider; a provider that does not exist yet
  // has none to offer.
  const { items: providerClients, isLoading: clientsLoading } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: selectedIssuer?.id ?? "" },
      { enabled: enabled && !!selectedIssuer },
    );
  const clientOptions = useMemo<ClientOption[]>(() => {
    const names = providerClients.map(clientOptionName);
    return providerClients.map((candidate, index) => ({
      id: candidate.id,
      name: names[index] ?? "",
      hint: clientOptionHint(
        candidate,
        names.filter((name) => name === names[index]).length > 1,
      ),
      scopes: candidate.scope ?? [],
    }));
  }, [providerClients]);
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

  // The provider probe is off once a provider matches. Manual is also the
  // fallback while clients load, so wait for the choice to settle.
  const scopeProbe = useProtectedResourceMetadata(
    remoteMcpServerId,
    enabled && !!selected && !clientsLoading && manualNeeded && !connected,
  );
  const resourceScopes = scopeProbe.metadata?.scopesSupported;
  const issuerScopes =
    selectedIssuer?.scopesSupported ?? discoveredMetadata?.scopesSupported;
  const scopeOptions = useMemo(
    () => advertisedScopes(resourceScopes, issuerScopes),
    [resourceScopes, issuerScopes],
  );

  const resetChoice = (): void => {
    setChoicePick(null);
    setExistingPick(null);
    setRegistrationMethod("cimd");
    // Manual credentials are issued by one provider and meaningless to the
    // next, so they leave with it rather than being saved under its successor.
    setClientId("");
    setClientSecret("");
    setScopes([]);
    setLocalStatus({ kind: "idle" });
  };

  // A client belongs to exactly one provider, so choosing a provider clears
  // the client choice and any outcome from the previous one.
  const selectProvider = (id: string | null): void => {
    // Seed the by-id lookup with the record the menu already holds, so the
    // pick stays named once the menu's search moves on.
    const picked = issuers.find((issuer) => issuer.id === id);
    if (picked) {
      queryClient.setQueryData(
        queryKeyRemoteSessionIssuer({ id: picked.id }),
        picked,
      );
    }
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
          authorizationGrantProfilesSupported:
            draft.authorizationGrantProfilesSupported ?? [],
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
        // broke Salesforce logins. Scopes chosen for a manual client win.
        const chosenScopes = manualNeeded ? scopes : [];
        const requestedScopes =
          chosenScopes.length > 0
            ? chosenScopes
            : preferredScopes(
                prm.metadata?.scopesSupported ??
                  resourceScopes ??
                  (await protectedResourceScopes(client, remoteMcpServerId)),
                selectedIssuer?.scopesSupported ?? draft?.scopesSupported,
              );
        const secret = manualNeeded ? clientSecret.trim() : "";
        clientConfiguration = {
          clientId: manualNeeded ? clientId.trim() : undefined,
          clientSecret: secret || undefined,
          scope: requestedScopes.length > 0 ? requestedScopes : undefined,
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
        invalidateAllGetRemoteMcpServerScopes(queryClient),
        invalidateSharedIssuerImpact(queryClient),
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

  const pendingChange =
    touched &&
    status.kind !== "done" &&
    !(choice === "existing" && sameAsConnected);

  const canSave =
    !!selected &&
    !connected &&
    !providerLoadFailed &&
    !capabilitiesLoading &&
    // Saving before the client list lands would miss an existing client and
    // register a duplicate in its place.
    !clientsLoading &&
    !isPending &&
    status.kind !== "done" &&
    choiceComplete;

  return {
    providerGroups,
    providerSearch,
    setProviderSearch,
    selected,
    selectProvider,
    providerUnreachable,
    providerLoading:
      prm.status === "loading" ||
      linkedIssuerLoading ||
      hostMatchQuery.isLoading ||
      discoveredQuery.isLoading,
    providerLoadFailed,

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
    scopes,
    setScopes: (values: string[]): void => setScopes(normalizeScopes(values)),
    scopeOptions,
    registrationGuideUrl:
      selectedIssuer?.clientSetupDocumentationUrl ??
      selectedIssuer?.serviceDocumentation ??
      discoveredMetadata?.serviceDocumentation ??
      null,

    replacesClient,
    status,
    canSave,
    pendingChange,
    save: async (): Promise<boolean> => {
      // Awaitable so callers can sequence work after it. onError has already
      // put the failure on screen, so the rejection is swallowed here rather
      // than surfacing twice or escaping as an unhandled rejection.
      try {
        await runCommit();
        return true;
      } catch {
        /* reported by onError */
        return false;
      }
    },
    saving: isPending,
  };
}
