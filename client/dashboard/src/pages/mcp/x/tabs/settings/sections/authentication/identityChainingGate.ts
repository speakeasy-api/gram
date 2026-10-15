import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

/** Which sources can vouch for a person's sign-in on an identity chaining issuer. */
export type ChainingProviders = { external: boolean; speakeasy: boolean };

// Set-up issuers that are paused, keyless or turned off keep bindings manageable;
// the order shows why Speakeasy can't serve.
const SPEAKEASY_ON: ReadonlySet<InternalSessionIssuer["state"]> = new Set([
  "enabled",
  "signing_paused",
  "keyless",
  "disabled",
]);

export function speakeasyIssuerOn(
  issuer: InternalSessionIssuer | undefined,
): boolean {
  return !!issuer && SPEAKEASY_ON.has(issuer.state);
}

/**
 * Identity chaining needs an organization issuer that either trusts an
 * identity provider sign-in client or can rely on Speakeasy's own issuer,
 * which vouches from the person's Speakeasy sign-in.
 */
export function identityChainingProviders(
  issuer: UserSessionIssuer | undefined,
  {
    externalRollout,
    speakeasyRollout,
    speakeasyIssuer,
  }: {
    externalRollout: boolean;
    /** Speakeasy only serves vendors whose saved provider order names it. */
    speakeasyRollout: boolean;
    speakeasyIssuer: InternalSessionIssuer | undefined;
  },
): ChainingProviders | undefined {
  if (issuer?.projectId !== "") return undefined;
  const external =
    externalRollout &&
    !!issuer.trustedRemoteSessionIssuerId &&
    !!issuer.trustedRemoteSessionClientId;
  const speakeasy = speakeasyRollout && speakeasyIssuerOn(speakeasyIssuer);
  return external || speakeasy ? { external, speakeasy } : undefined;
}

/** Whether the issuer's sign-in trusts an external identity provider at all. */
export function trustsExternalIdp(issuer: UserSessionIssuer): boolean {
  return (
    !!issuer.trustedRemoteSessionIssuerId ||
    !!issuer.trustedRemoteSessionClientId
  );
}
