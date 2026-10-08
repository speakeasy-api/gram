import { getServerURL } from "@/lib/utils";
import { DEFAULT_USER_SESSION_DURATION_HOURS } from "@/lib/externalMcpUserSessions";
import type { GramCore } from "@gram/client/core.js";
import { jsonWebKeySetsCreate } from "@gram/client/funcs/jsonWebKeySetsCreate.js";
import { jsonWebKeySetsListKeys } from "@gram/client/funcs/jsonWebKeySetsListKeys.js";
import { jsonWebKeySetsPublishKey } from "@gram/client/funcs/jsonWebKeySetsPublishKey.js";
import { organizationRemoteSessionClientsAttachKeySet } from "@gram/client/funcs/organizationRemoteSessionClientsAttachKeySet.js";
import { organizationRemoteSessionClientsCreate } from "@gram/client/funcs/organizationRemoteSessionClientsCreate.js";
import { organizationRemoteSessionClientsUpdate } from "@gram/client/funcs/organizationRemoteSessionClientsUpdate.js";
import { organizationUserSessionIssuersCreate } from "@gram/client/funcs/organizationUserSessionIssuersCreate.js";
import { organizationUserSessionIssuersUpdate } from "@gram/client/funcs/organizationUserSessionIssuersUpdate.js";
import type { JSONWebKey } from "@gram/client/models/components/jsonwebkey.js";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import type { OrganizationUserSessionIssuerReference } from "@gram/client/models/components/organizationusersessionissuerreference.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { CreateOrganizationUserSessionIssuerRequest } from "@gram/client/models/operations/createorganizationusersessionissuer.js";
import { unwrapAsync } from "@gram/client/types/fp.js";

import { SESSION_SECURITY } from "../../identityProviderQueries";

const SIGN_IN_SCOPES = ["openid", "email", "profile", "offline_access"];
const SIGN_IN_ISSUER_SLUG = "okta-sign-in";
const SIGN_IN_KEY_SET_NAME = "Okta sign-in";

export type KeySetChoice =
  | { kind: "existing"; setId: string }
  | { kind: "create"; externalKeyId: string };

export type SignInClientMatch = {
  client: RemoteSessionClient | undefined;
  /** More than one when several clients claim the agent ID; none is chosen then. */
  duplicates: number;
};

/** The org-owned client whose client_id is the agent ID; never the managed connection client. */
export function findSignInClient(
  clients: RemoteSessionClient[],
  agentId: string | undefined,
  managedClientId: string | undefined,
): SignInClientMatch {
  if (!agentId || agentId === managedClientId) {
    return { client: undefined, duplicates: 0 };
  }
  const matches = clients.filter(
    (client) => client.projectId === "" && client.clientId === agentId,
  );
  if (matches.length > 1) {
    return { client: undefined, duplicates: matches.length };
  }
  return { client: matches[0], duplicates: 0 };
}

/** Org clients of the connection's issuer registered for some other agent ID. */
export function staleSignInClientIds(
  clients: RemoteSessionClient[],
  agentId: string,
  managedClientId: string | undefined,
): Set<string> {
  return new Set(
    clients
      .filter(
        (client) =>
          client.projectId === "" &&
          client.clientId !== agentId &&
          client.clientId !== managedClientId,
      )
      .map((client) => client.id),
  );
}

/** Sets owned by the connection's managed clients; the server refuses to attach them elsewhere. */
export function managedKeySetIds(
  clients: RemoteSessionClient[],
  agentId: string,
): Set<string> {
  return new Set(
    clients
      .filter((client) => client.clientId !== agentId)
      .map((client) => client.jsonWebKeySetId)
      .filter((id): id is string => id != null),
  );
}

/** External keys backing managed sets; the server refuses to back a new set with them. */
export function managedExternalKeyIds(
  sets: JSONWebKeySet[],
  managedSetIds: ReadonlySet<string>,
): Set<string> {
  return new Set(
    sets
      .filter((set) => managedSetIds.has(set.id))
      .map((set) => set.externalKeyId),
  );
}

export function clientJwksUrl(client: RemoteSessionClient): string {
  const pinned = client.federatedCallbackUrl ?? client.callbackUrl;
  const origin = pinned ? new URL(pinned).origin : getServerURL();
  return `${origin.replace(/\/+$/, "")}/.well-known/oauth-client/${client.id}/jwks.json`;
}

const PUBLIC_JWK_MEMBERS = [
  "kid",
  "kty",
  "alg",
  "use",
  "n",
  "e",
  "crv",
  "x",
  "y",
] as const;

/** The active key's public members only, for pasting into the agent's Okta credentials. */
export function activePublicJwk(
  keys: JSONWebKey[],
): Record<string, unknown> | undefined {
  const active = keys.find((key) => key.keyState === "active");
  const doc: unknown = active?.publicJwk;
  if (doc == null || typeof doc !== "object") return undefined;
  const source = doc as Record<string, unknown>;
  const jwk: Record<string, unknown> = {};
  for (const member of PUBLIC_JWK_MEMBERS) {
    if (member in source) jwk[member] = source[member];
  }
  jwk.use ??= "sig";
  return jwk;
}

function missingSignInScopes(client: RemoteSessionClient): string[] {
  const scopes = client.scope ?? [];
  return SIGN_IN_SCOPES.filter((scope) => !scopes.includes(scope));
}

// Okta expects the token endpoint URL as the assertion audience.
function needsTokenEndpointAudience(client: RemoteSessionClient): boolean {
  return client.tokenEndpointAuthAudienceFormat !== "token_endpoint";
}

export function isSignInClientReady(client: RemoteSessionClient): boolean {
  return (
    client.tokenEndpointAuthMethod === "private_key_jwt" &&
    !needsTokenEndpointAudience(client) &&
    client.jsonWebKeySetId != null &&
    missingSignInScopes(client).length === 0
  );
}

async function ensureActiveKey(core: GramCore, setId: string): Promise<void> {
  const { keys } = await unwrapAsync(
    jsonWebKeySetsListKeys(core, { setId }, SESSION_SECURITY),
  );
  if (keys.some((key) => key.keyState === "active")) return;
  // Publishing into a set with no active key activates the new key immediately.
  await unwrapAsync(
    jsonWebKeySetsPublishKey(core, { setId }, SESSION_SECURITY),
  );
}

async function resolveKeySet(
  core: GramCore,
  choice: KeySetChoice | undefined,
  onKeySetCreated: ((setId: string) => void) | undefined,
): Promise<string> {
  if (!choice) throw new Error("Choose a signing key set first.");
  if (choice.kind === "existing") return choice.setId;
  const created = await unwrapAsync(
    jsonWebKeySetsCreate(
      core,
      {
        createJSONWebKeySetForm: {
          name: SIGN_IN_KEY_SET_NAME,
          externalKeyId: choice.externalKeyId,
        },
      },
      SESSION_SECURITY,
    ),
  );
  onKeySetCreated?.(created.id);
  return created.id;
}

export type OktaSignInSetupInput = {
  remoteSessionIssuerId: string;
  agentId: string;
  existingClient: RemoteSessionClient | undefined;
  keySet: KeySetChoice | undefined;
  /** Undefined creates a new sign-in issuer with `newIssuerSlug`. */
  userSessionIssuer: UserSessionIssuer | undefined;
  newIssuerSlug: string;
  /** Lets a retry reuse a set created by a failed attempt. */
  onKeySetCreated?: (setId: string) => void;
};

/** Idempotent: each step is skipped when the server already reflects it. */
export async function setUpOktaSignIn(
  core: GramCore,
  input: OktaSignInSetupInput,
): Promise<{ client: RemoteSessionClient; issuer: UserSessionIssuer }> {
  const { remoteSessionIssuerId, agentId } = input;

  let client =
    input.existingClient ??
    (await unwrapAsync(
      organizationRemoteSessionClientsCreate(core, {
        createOrganizationRemoteSessionClientForm: {
          remoteSessionIssuerId,
          clientId: agentId,
          scope: SIGN_IN_SCOPES,
        },
      }),
    ));

  const setId =
    client.jsonWebKeySetId ??
    (await resolveKeySet(core, input.keySet, input.onKeySetCreated));
  await ensureActiveKey(core, setId);
  if (client.jsonWebKeySetId == null) {
    client = await unwrapAsync(
      organizationRemoteSessionClientsAttachKeySet(core, {
        attachKeySetForm: { id: client.id, jsonWebKeySetId: setId },
      }),
    );
  }

  const switchingMethod = client.tokenEndpointAuthMethod !== "private_key_jwt";
  const fixAudience = needsTokenEndpointAudience(client);
  if (
    switchingMethod ||
    fixAudience ||
    missingSignInScopes(client).length > 0
  ) {
    client = await unwrapAsync(
      organizationRemoteSessionClientsUpdate(core, {
        updateRemoteSessionClientForm: {
          id: client.id,
          tokenEndpointAuthMethod: "private_key_jwt",
          tokenEndpointAuthAudienceFormat: fixAudience
            ? "token_endpoint"
            : undefined,
          scope: [...new Set([...(client.scope ?? []), ...SIGN_IN_SCOPES])],
        },
      }),
    );
  }

  const existing = input.userSessionIssuer;
  if (!existing) {
    const issuer = await unwrapAsync(
      organizationUserSessionIssuersCreate(core, {
        createOrganizationUserSessionIssuerForm: {
          slug: input.newIssuerSlug,
          authnChallengeMode: "interactive",
          sessionDurationHours: DEFAULT_USER_SESSION_DURATION_HOURS,
          trustedRemoteSessionIssuerId: remoteSessionIssuerId,
          trustedRemoteSessionClientId: client.id,
        },
      }),
    );
    return { client, issuer };
  }
  if (
    existing.trustedRemoteSessionClientId === client.id &&
    existing.trustedRemoteSessionIssuerId === remoteSessionIssuerId
  ) {
    return { client, issuer: existing };
  }
  const issuer = await unwrapAsync(
    organizationUserSessionIssuersUpdate(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: existing.id,
        trustedRemoteSessionIssuerId: remoteSessionIssuerId,
        trustedRemoteSessionClientId: client.id,
      },
    }),
  );
  return { client, issuer };
}

export type SignInIssuerRow = {
  issuer: UserSessionIssuer;
  trustsSignIn: boolean;
  /** Trusts some other sign-in pair, which trusting Okta would replace. */
  trustsOther: boolean;
  /** Trusts the sign-in client of a previous agent ID. */
  trustsStale: boolean;
};

/** Organization sign-in issuers and whether each trusts the Okta sign-in pair. */
export function signInIssuerRows(
  issuers: UserSessionIssuer[],
  remoteSessionIssuerId: string,
  client: RemoteSessionClient | undefined,
  staleClientIds: ReadonlySet<string> = new Set(),
): SignInIssuerRow[] {
  return issuers
    .filter((issuer) => issuer.projectId === "")
    .map((issuer) => {
      const trustsSignIn =
        client != null &&
        issuer.trustedRemoteSessionIssuerId === remoteSessionIssuerId &&
        issuer.trustedRemoteSessionClientId === client.id;
      const trustsOther =
        !trustsSignIn && issuer.trustedRemoteSessionClientId != null;
      const trustsStale =
        trustsOther &&
        issuer.trustedRemoteSessionIssuerId === remoteSessionIssuerId &&
        staleClientIds.has(issuer.trustedRemoteSessionClientId ?? "");
      return { issuer, trustsSignIn, trustsOther, trustsStale };
    });
}

export function normalizeIssuerSlug(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+/, "");
}

export function signInIssuerSlugError(
  slug: string,
  issuers: UserSessionIssuer[],
): string | null {
  const trimmed = slug.replace(/-+$/, "");
  if (trimmed === "") return "Enter a slug.";
  const taken = issuers.some(
    (issuer) => issuer.projectId === "" && issuer.slug === trimmed,
  );
  return taken ? "An organization issuer already uses this slug." : null;
}

/** The slug a new sign-in issuer defaults to: the first free okta-sign-in variant. */
export function freeSignInIssuerSlug(issuers: UserSessionIssuer[]): string {
  const taken = new Set(
    issuers.filter((issuer) => issuer.projectId === "").map((i) => i.slug),
  );
  let slug = SIGN_IN_ISSUER_SLUG;
  for (let n = 2; taken.has(slug); n++) slug = `${SIGN_IN_ISSUER_SLUG}-${n}`;
  return slug;
}

export function addSignInIssuerRequest(input: {
  slug: string;
  remoteSessionIssuerId: string;
  clientId: string;
}): CreateOrganizationUserSessionIssuerRequest {
  return {
    createOrganizationUserSessionIssuerForm: {
      slug: input.slug.replace(/-+$/, ""),
      authnChallengeMode: "interactive",
      sessionDurationHours: DEFAULT_USER_SESSION_DURATION_HOURS,
      trustedRemoteSessionIssuerId: input.remoteSessionIssuerId,
      trustedRemoteSessionClientId: input.clientId,
    },
  };
}

export type TrustSignInConfirmation = {
  issuer: UserSessionIssuer;
  trustsOther: boolean;
  servers: OrganizationUserSessionIssuerReference[];
  toolsets: OrganizationUserSessionIssuerReference[];
  lookupFailed: boolean;
};

export function needsTrustConfirmation(
  confirmation: Omit<TrustSignInConfirmation, "issuer">,
): boolean {
  return (
    confirmation.trustsOther ||
    confirmation.servers.length > 0 ||
    confirmation.toolsets.length > 0 ||
    confirmation.lookupFailed
  );
}
