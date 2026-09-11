import { useSdkClient } from "@/contexts/Sdk";
import { slugify } from "@/lib/constants";
import {
  deriveRemoteSessionIssuerNameFromUrl,
  remoteSessionScopeTier,
} from "@/lib/sources";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import { useProtectedResourceMetadata } from "./useProtectedResourceMetadata";

/**
 * The provider the upstream advertises but Speakeasy has no record of yet.
 * Selecting it commits `create_provider`, so it carries a sentinel id rather
 * than a row id right up until save.
 */
export const DISCOVERED_PROVIDER_ID = "__discovered__";

/** Tier headings, in the order AIM-230 fixes for this flow. */
const TIER_ORDER = ["Platform", "Organization", "Project"] as const;
export type ProviderTier = (typeof TIER_ORDER)[number];

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
};

export type ClientOption = {
  id: string;
  name: string;
  connections: number;
};

export type UserIdentityStatus =
  | { kind: "idle" }
  | { kind: "pending" }
  | { kind: "done"; label: "Registered" | "Linked" }
  | { kind: "refused"; message: string | null }
  | { kind: "unreachable"; message: string | null };

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

  clientOptions: ClientOption[];
  clientsLoading: boolean;
  existingClient: ClientOption | null;
  selectClient: (id: string | null) => void;
  clientLabel: string;
  clientCaption: string;
  newClientHint: string;
  automaticAvailable: boolean;

  manualNeeded: boolean;
  clientId: string;
  setClientId: (value: string) => void;
  clientSecret: string;
  setClientSecret: (value: string) => void;
  enterCredentialsManually: () => void;
  registrationGuideUrl: string | null;

  status: UserIdentityStatus;
  idleHint: string;
  canSave: boolean;
  save: () => void;
  saving: boolean;
};

/**
 * Owns the User Identity choice for one Remote MCP-backed server: which
 * Remote Identity Provider, which OAuth client, and the single commit that
 * writes both. The issuer/client split stays inside here — callers see one
 * provider and one registration choice, per AIM-230.
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
  const [clientPick, setClientPick] = useState<string | null>(null);
  const [forceManual, setForceManual] = useState(false);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [status, setStatus] = useState<UserIdentityStatus>({ kind: "idle" });

  // Only probe for a provider to create when none of the existing ones match;
  // a configured server already has its answer.
  const upstreamHost = hostOf(upstreamUrl);
  const matchedIssuer = useMemo(
    () =>
      issuers.find((issuer) => {
        const host = hostOf(issuer.issuer);
        return (
          host !== "" && (host === upstreamHost || upstreamHost.endsWith(host))
        );
      }),
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
        name: remoteSessionClientDisplayName(candidate),
        connections: candidate.userSessionIssuerIds.length,
      })),
    [providerClients],
  );
  const existingClient =
    clientOptions.find((candidate) => candidate.id === clientPick) ?? null;

  // A provider that publishes neither a CIMD-capable document nor a
  // registration endpoint cannot register this server on its own.
  const automaticAvailable = selectedDiscovered
    ? true
    : !!selectedIssuer &&
      (selectedIssuer.clientIdMetadataDocumentSupported ||
        !!selectedIssuer.registrationEndpoint?.trim());
  const manualNeeded = !existingClient && (forceManual || !automaticAvailable);

  // Reset the client choice whenever the provider changes: a client belongs to
  // exactly one provider.
  useEffect(() => {
    setClientPick(null);
    setForceManual(false);
    setStatus({ kind: "idle" });
  }, [selectedProviderId]);

  const commit = useMutation({
    mutationFn: async () => {
      if (!selected) throw new Error("choose an identity provider");
      const clientMode = existingClient
        ? "existing"
        : manualNeeded
          ? "manual"
          : "auto";

      // A discovered provider is created from what the upstream publishes, so
      // read its metadata now and send the whole record with the commit.
      let createProvider = undefined;
      let providerId: string | undefined = selectedIssuer?.id;
      if (selectedDiscovered && discoveredIssuerUrl) {
        const draft = await client.remoteSessionIssuers.fetchMetadata({
          fetchIssuerMetadataRequestBody: { issuer: discoveredIssuerUrl },
        });
        providerId = undefined;
        createProvider = {
          issuer: discoveredIssuerUrl,
          slug: slugify(selectedDiscovered.name) || "identity-provider",
          name: selectedDiscovered.name,
          authorizationEndpoint: draft.authorizationEndpoint ?? undefined,
          tokenEndpoint: draft.tokenEndpoint ?? undefined,
          registrationEndpoint: draft.registrationEndpoint ?? undefined,
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
        };
      }

      return await client.remoteSessions.commitServerUserIdentityConfiguration({
        commitServerUserIdentityConfigurationForm: {
          mcpServerId,
          providerId,
          createProvider,
          clientMode,
          existingClientId: existingClient?.id,
          clientConfiguration: existingClient
            ? undefined
            : {
                clientId: manualNeeded ? clientId.trim() : undefined,
                clientSecret: manualNeeded
                  ? clientSecret.trim() || undefined
                  : undefined,
              },
        },
      });
    },
    onSuccess: async (result) => {
      if (result.failure) {
        setStatus({
          kind:
            result.failure.outcome === "refused" ? "refused" : "unreachable",
          message: result.failure.providerMessage ?? null,
        });
        return;
      }
      if (result.manualSetupRequired) {
        // Not a registration failure: nothing was written, and the operator
        // needs to paste credentials the provider issued out of band.
        setForceManual(true);
        setStatus({ kind: "idle" });
        return;
      }
      setStatus({
        kind: "done",
        label: result.status === "linked" ? "Linked" : "Registered",
      });
      setClientId("");
      setClientSecret("");
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllRemoteSessionIssuers(queryClient),
      ]);
    },
    onError: () => {
      setStatus({ kind: "idle" });
    },
  });

  const { mutate: runCommit, isPending } = commit;
  useEffect(() => {
    if (isPending) setStatus({ kind: "pending" });
  }, [isPending]);

  const canSave =
    !!selected &&
    !isPending &&
    status.kind !== "done" &&
    (!manualNeeded || clientId.trim() !== "");

  const idleHint = !selected
    ? "Choose a provider to continue."
    : existingClient
      ? `Uses ${existingClient.name} when you save.`
      : manualNeeded
        ? "Saved with the credentials you paste."
        : selected.isNew
          ? "Created from what the upstream publishes and registered when you save."
          : "Registered automatically when you save.";

  return {
    providerGroups,
    selected,
    selectProvider: setProviderPick,
    providerUnreachable,
    providerLoading: prm.status === "loading",

    clientOptions,
    clientsLoading,
    existingClient,
    selectClient: setClientPick,
    /** Label for the registration picker's trigger. */
    clientLabel: existingClient
      ? existingClient.name
      : manualNeeded
        ? "New client"
        : "Auto-Configure",
    clientCaption: existingClient
      ? `${existingClient.connections} connection${existingClient.connections === 1 ? "" : "s"}`
      : manualNeeded
        ? "Credentials below"
        : "Registers on save",
    newClientHint: manualNeeded
      ? "Needs credentials from the provider"
      : "Recommended. Registers automatically.",
    automaticAvailable,

    manualNeeded,
    clientId,
    setClientId,
    clientSecret,
    setClientSecret,
    enterCredentialsManually: (): void => {
      setForceManual(true);
      setStatus({ kind: "idle" });
    },
    registrationGuideUrl: selectedIssuer?.clientSetupDocumentationUrl ?? null,

    status,
    idleHint,
    canSave,
    save: (): void => runCommit(),
    saving: isPending,
  };
}
