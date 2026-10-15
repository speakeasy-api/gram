import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

/** Whether an external identity provider vouches for this sign-in; otherwise Speakeasy does. */
export type ChainingProviders = { external: boolean };

// Set-up issuers that are paused, keyless or turned off keep bindings manageable;
// the server page shows why Speakeasy can't serve.
const SPEAKEASY_ON: ReadonlySet<InternalSessionIssuer["state"]> = new Set([
  "enabled",
  "signing_paused",
  "keyless",
  "disabled",
]);

function speakeasyIssuerOn(issuer: InternalSessionIssuer | undefined): boolean {
  return !!issuer && SPEAKEASY_ON.has(issuer.state);
}

/**
 * Identity chaining needs an organization issuer that either trusts an
 * identity provider sign-in client or can rely on Speakeasy's own issuer,
 * which vouches from the person's Speakeasy sign-in.
 */
export function identityChainingProviders(
  issuer: UserSessionIssuer | undefined,
  rollouts: ChainingRollouts,
): ChainingProviders | undefined {
  if (issuer?.projectId !== "") return undefined;
  return chainingVouchers(issuer, rollouts);
}

type ChainingRollouts = {
  externalRollout: boolean;
  /** Speakeasy vouches only where the sign-in has no identity provider. */
  speakeasyRollout: boolean;
  speakeasyIssuer: InternalSessionIssuer | undefined;
};

/** Who would vouch for this sign-in if it belonged to the organization. */
export function chainingVouchers(
  issuer: UserSessionIssuer,
  { externalRollout, speakeasyRollout, speakeasyIssuer }: ChainingRollouts,
): ChainingProviders | undefined {
  const external =
    externalRollout &&
    !!issuer.trustedRemoteSessionIssuerId &&
    !!issuer.trustedRemoteSessionClientId;
  const speakeasy = speakeasyRollout && speakeasyIssuerOn(speakeasyIssuer);
  return external || speakeasy ? { external } : undefined;
}

/** Whether the issuer's sign-in trusts an external identity provider at all. */
export function trustsExternalIdp(issuer: UserSessionIssuer): boolean {
  return (
    !!issuer.trustedRemoteSessionIssuerId ||
    !!issuer.trustedRemoteSessionClientId
  );
}
