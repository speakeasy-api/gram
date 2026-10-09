import { normalizeScopes } from "@/lib/remote-identity/model/clientConfiguration";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { ResourceMetadata } from "@gram/client/models/components/prepareemarequestbody.js";
import type { ProtectedResourceMetadata } from "@gram/client/models/components/protectedresourcemetadata.js";

// Mirrors oauthwire.IsOIDCReservedScope; the exchange strips these.
const OIDC_RESERVED_SCOPES = new Set([
  "openid",
  "profile",
  "email",
  "offline_access",
  "phone",
  "address",
]);

function isOIDCReservedScope(scope: string): boolean {
  return OIDC_RESERVED_SCOPES.has(scope);
}

function withoutOIDCScopes(scopes: string[] | undefined | null): string[] {
  return normalizeScopes(scopes ?? []).filter(
    (scope) => !isOIDCReservedScope(scope),
  );
}

/** The client's scopes a chained token can request; undefined when the client sets none. */
export function chainableClientScopes(
  clientScope: string[] | undefined | null,
): string[] | undefined {
  return clientScope == null ? undefined : withoutOIDCScopes(clientScope);
}

/** The scopes to request before an administrator chooses any. */
export function defaultChainingScopes(
  clientScope: string[] | undefined | null,
): string[] {
  return chainableClientScopes(clientScope) ?? [];
}

export type ChainingScopeOption = {
  value: string;
  label: string;
  disabled?: boolean;
  description?: string;
};

/**
 * Options for the requested scopes control. prepareEMA only accepts a subset
 * of the client's scope, so a resource scope outside it is shown but disabled.
 */
export function chainingScopeOptions(
  clientScope: string[] | undefined | null,
  resourceScopes: string[] | undefined | null,
  selected: string[],
): ChainingScopeOption[] {
  const allowed = chainableClientScopes(clientScope);
  const advertised = withoutOIDCScopes(resourceScopes);
  const values = normalizeScopes([
    ...(allowed ?? []),
    ...advertised,
    ...selected,
  ]);
  return values.map((value) => {
    const outsideClient = allowed !== undefined && !allowed.includes(value);
    if (outsideClient) {
      return {
        value,
        label: value,
        disabled: true,
        description: "Add to the client's scope first",
      };
    }
    return advertised.includes(value)
      ? { value, label: value, description: "Advertised by this server" }
      : { value, label: value };
  });
}

/** Whether prepareEMA accepts these scopes for the client. */
export function chainingScopesAllowed(
  scopes: string[],
  clientScope: string[] | undefined | null,
): boolean {
  const allowed = chainableClientScopes(clientScope);
  if (allowed === undefined) return true;
  return scopes.every((scope) => allowed.includes(scope));
}

/** Drops OIDC-reserved scopes and duplicates from an administrator's choice. */
export function sanitizeChainingScopes(scopes: string[]): string[] {
  return withoutOIDCScopes(scopes);
}

export function sameScopes(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((scope) => set.has(scope));
}

type ChainingSnapshot = Pick<
  IdentityChainingPreparation,
  "state" | "bindingId" | "clientId"
>;

/** Whether a client is bound; a read with no binding still reports eligibility states. */
export function chainingBound(
  snapshot: ChainingSnapshot | undefined | null,
): boolean {
  return snapshot?.bindingId != null && snapshot.clientId != null;
}

/** Whether a binding row exists that Disable can unlink. */
export function chainingUnlinkable(
  snapshot: ChainingSnapshot | undefined | null,
): boolean {
  return snapshot?.bindingId != null && snapshot.state !== "unlinked";
}

/** The state to present: a missing binding that only needs a client reads as not enabled. */
export function displayedChainingState(
  snapshot: ChainingSnapshot,
): IdentityChainingPreparation["state"] {
  if (!chainingBound(snapshot) && snapshot.state === "configuration_required") {
    return "unlinked";
  }
  return snapshot.state;
}

/** Whether prepareEMA persisted a usable binding rather than reporting a blocker. */
export function preparationSucceeded(
  result: Pick<IdentityChainingPreparation, "state">,
): boolean {
  return (
    result.state === "ready" ||
    result.state === "published_acceptance_unverified"
  );
}

export const JWT_BEARER_GRANT = "urn:ietf:params:oauth:grant-type:jwt-bearer";
// RFC 7591 section 2: omitted grant_types default to authorization_code.
const REGISTRATION_DEFAULT_GRANTS = ["authorization_code"];
// A CIMD client with no recorded grants publishes both interactive grants.
const CIMD_DEFAULT_GRANTS = ["authorization_code", "refresh_token"];

/**
 * The grants enabling chaining declares for the client. `rewrites` is true
 * when they differ from its recorded grants, which rewrites its registration.
 */
export function chainingGrantDeclaration(
  grantTypes: string[] | undefined | null,
  cimd = false,
): { grants: string[]; rewrites: boolean } {
  const base =
    grantTypes ?? (cimd ? CIMD_DEFAULT_GRANTS : REGISTRATION_DEFAULT_GRANTS);
  const grants = [...new Set([...base, JWT_BEARER_GRANT])];
  const rewrites = grantTypes == null || !sameScopes(grants, grantTypes);
  return { grants, rewrites };
}

type ChainingClient = {
  clientIdMetadataUri?: string | undefined;
  tokenEndpointAuthMethod?: string | undefined;
};

/** A CIMD client publishes its grants in its metadata document. */
export function isCimdClient(client: ChainingClient): boolean {
  return !!client.clientIdMetadataUri;
}

/** ID-JAG redemption is limited to confidential clients. */
export function isPublicClient(client: ChainingClient): boolean {
  return client.tokenEndpointAuthMethod === "none";
}

/** The probed resource's authorization servers, so prepare refuses a mismatch. */
export function chainingResourceMetadata(
  metadata: ProtectedResourceMetadata | null | undefined,
): ResourceMetadata | undefined {
  if (!metadata?.resource || !metadata.authorizationServers?.length) {
    return undefined;
  }
  return {
    resource: metadata.resource,
    authorizationServers: metadata.authorizationServers,
  };
}
