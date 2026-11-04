import {
  displayUrl,
  issuerDisplayName,
  useAllRemoteSessionClients,
  useProtectedResourceMetadata,
  useRemoteSessionIssuersByIds,
} from "@/lib/remote-identity";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useState } from "react";

export type SignInChoice = {
  registration: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  /** The authorization server, which is what an administrator recognizes. */
  label: string;
  /** Where it lives, and which registration of it this is when that matters. */
  description: string;
};

type SignInChoices = {
  choices: SignInChoice[];
  /** Registrations left out because they belong to another server. */
  hiddenCount: number;
  /** The upstream's authorization server as shown, or "" when it names none. */
  upstreamIssuer: string;
  showAll: boolean;
  setShowAll: (showAll: boolean) => void;
  isLoading: boolean;
  isError: boolean;
};

/** Spellings of one issuer URL compare equal, as the server's matching does. */
function canonicalIssuer(url: string): string {
  try {
    const parsed = new URL(url.trim());
    return `${parsed.protocol}//${parsed.host}${parsed.pathname.replace(/\/+$/, "")}`;
  } catch {
    return url.trim().replace(/\/+$/, "");
  }
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
  const url = issuer ? displayUrl(issuer.issuer) : "";
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
 */
export function signInChoices(
  registrations: RemoteSessionClient[],
  issuers: RemoteSessionIssuer[],
  authorizationServers: string[] | undefined,
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
  if (!authorizationServers?.length) return { matching: all, all };
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
 * The sign-ins a new credential can use, narrowed to the authorization server
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
  const authorizationServers = resource.metadata?.authorizationServers;
  const { matching, all } = signInChoices(
    owned,
    issuers.items,
    authorizationServers,
  );

  return {
    choices: showAll ? all : matching,
    hiddenCount: all.length - matching.length,
    upstreamIssuer: authorizationServers?.[0]
      ? displayUrl(authorizationServers[0])
      : "",
    showAll,
    setShowAll,
    isLoading:
      registrations.isLoading ||
      issuers.isLoading ||
      resource.status === "loading",
    isError: registrations.isError || issuers.isError,
  };
}
