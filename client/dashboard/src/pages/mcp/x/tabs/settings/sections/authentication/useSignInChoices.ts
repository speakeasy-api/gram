import {
  issuerDisplayName,
  providerUrlLabel,
  useAllRemoteSessionClients,
  useProtectedResourceMetadata,
  useRemoteSessionIssuersByIds,
} from "@/lib/remote-identity";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useState } from "react";

type SignInChoice = {
  registration: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  /** The authorization server, which is what an administrator recognizes. */
  label: string;
  /** Where it lives, and which registration of it this is when that matters. */
  description: string;
};

type SignInChoices = {
  choices: SignInChoice[];
  /** The narrowed list, whichever one is showing. */
  matching: SignInChoice[];
  /** Registrations left out because they belong to another server. */
  hiddenCount: number;
  /** The upstream's authorization servers as shown, or "" when it names none. */
  upstreamIssuers: string;
  showAll: boolean;
  setShowAll: (showAll: boolean) => void;
  isLoading: boolean;
  isError: boolean;
};

/** Spellings of one issuer URL compare equal; a query or fragment sets it apart. */
function canonicalIssuer(url: string): string {
  try {
    const parsed = new URL(url.trim());
    return `${parsed.protocol}//${parsed.host}${parsed.pathname.replace(/\/+$/, "")}${parsed.search}${parsed.hash}`;
  } catch {
    return url.trim().replace(/\/+$/, "");
  }
}

const issuerList = new Intl.ListFormat("en", { type: "conjunction" });

/** Each named server once, however the upstream spelled it. */
function issuersLabel(authorizationServers: string[]): string {
  const shown = authorizationServers.map((url) =>
    providerUrlLabel(canonicalIssuer(url)),
  );
  return issuerList.format(new Set(shown));
}

function toChoice(
  registration: RemoteSessionClient,
  issuer: RemoteSessionIssuer | undefined,
  unnamedSiblings: number,
): SignInChoice {
  // An unnamed registration needs its id only to tell it from a sibling.
  const registrationLabel =
    registration.displayName || unnamedSiblings > 1
      ? remoteSessionClientDisplayName(registration)
      : "";
  const url = issuer ? providerUrlLabel(issuer.issuer) : "";
  return {
    registration,
    issuer,
    label:
      (issuer && issuerDisplayName(issuer)) ||
      remoteSessionClientDisplayName(registration),
    description: [url, registrationLabel].filter(Boolean).join(" · "),
  };
}

/**
 * Pairs each registration with its authorization server and keeps the ones of
 * the servers the upstream names. Naming none leaves every registration in.
 * One whose authorization server could not be looked up can't be shown to match,
 * so it stays out of the narrowed list.
 */
function signInChoices(
  registrations: RemoteSessionClient[],
  issuers: RemoteSessionIssuer[],
  authorizationServers: string[],
): { matching: SignInChoice[]; all: SignInChoice[] } {
  const byId = new Map(issuers.map((issuer) => [issuer.id, issuer]));
  const issuerOf = (registration: RemoteSessionClient) =>
    byId.get(registration.remoteSessionIssuerId);
  const issuerKey = (registration: RemoteSessionClient) => {
    const url = issuerOf(registration)?.issuer;
    return url ? canonicalIssuer(url) : registration.id;
  };
  const unnamed = new Map<string, number>();
  for (const registration of registrations) {
    if (registration.displayName) continue;
    const key = issuerKey(registration);
    unnamed.set(key, (unnamed.get(key) ?? 0) + 1);
  }
  const all = registrations.map((registration) =>
    toChoice(
      registration,
      issuerOf(registration),
      unnamed.get(issuerKey(registration)) ?? 0,
    ),
  );
  if (!authorizationServers.length) return { matching: all, all };
  const advertised = new Set(authorizationServers.map(canonicalIssuer));
  return {
    matching: all.filter(
      (choice) =>
        !!choice.issuer &&
        advertised.has(canonicalIssuer(choice.issuer.issuer)),
    ),
    all,
  };
}

/**
 * The sign-ins a new credential can use, narrowed to the authorization servers
 * the upstream names. An upstream can name the wrong one, which an
 * administrator can correct, so the rest stay one click away.
 */
export function useSignInChoices(
  owner: "self" | "subject" | undefined,
  remoteMcpServerId: string | undefined,
): SignInChoices {
  const enabled = !!owner;
  const [showAll, setShowAll] = useState(false);
  const registrations = useAllRemoteSessionClients({}, { enabled });
  const owned = registrations.items.filter(
    (registration) => registration.credentialOwner === owner,
  );
  const issuers = useRemoteSessionIssuersByIds(
    [...new Set(owned.map((r) => r.remoteSessionIssuerId))],
    { enabled },
  );
  const resource = useProtectedResourceMetadata(remoteMcpServerId, enabled);
  const authorizationServers = (
    resource.metadata?.authorizationServers ?? []
  ).filter((url) => url.trim());
  const { matching, all } = signInChoices(
    owned,
    issuers.items,
    authorizationServers,
  );

  const isError =
    registrations.isError ||
    (issuers.isError && authorizationServers.length > 0 && !matching.length);

  return {
    // Nothing is offered while the error shows, so no selection can stand.
    choices: isError ? [] : showAll ? all : matching,
    matching,
    hiddenCount: all.length - matching.length,
    upstreamIssuers: issuersLabel(authorizationServers),
    showAll,
    setShowAll,
    isLoading:
      registrations.isLoading ||
      issuers.isLoading ||
      resource.status === "loading",
    isError,
  };
}
