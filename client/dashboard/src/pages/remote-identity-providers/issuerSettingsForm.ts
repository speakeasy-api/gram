import type { CreateRemoteSessionIssuerForm } from "@gram/client/models/components/createremotesessionissuerform.js";
import type { UpdateRemoteSessionIssuerForm } from "@gram/client/models/components/updateremotesessionissuerform.js";
import type { DiscoveredEndpoints } from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";

// The editable state both issuer Settings tabs collect. The tenant tab and the
// platform catalog tab hold it in their own useState, but they submit exactly
// the same shape.
export type IssuerSettingsFormState = {
  id: string;
  name: string;
  // The logo's asset id, empty when the issuer has no logo. Like the other
  // optional strings, "" on update is the explicit "clear to NULL" sentinel.
  logoAssetId: string;
  slug: string;
  clientSetupDocumentationUrl: string;
  issuerUrl: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  discoveredSnapshot: DiscoveredEndpoints | null;
};

// buildUpdateIssuerForm turns the Settings tab state into the update payload.
//
// It lives here rather than in either tab because the two tiers differ only in
// which endpoint they post to: the tenant tab calls
// organizationRemoteSessionIssuers.update and the platform catalog calls
// adminRemoteSessions.updateGlobalIssuer, but both take
// UpdateRemoteSessionIssuerForm. Keeping one builder means a new RFC 8414 field
// or a changed narg semantic cannot be applied to one tier and silently missed
// on the other, which nothing would type-check or test.
//
// Two semantics are load-bearing and easy to get wrong when copied:
//   - Empty strings are meaningful. The server reads "" on the URL fields as
//     the explicit "clear this to NULL" sentinel, so they are always sent.
//   - The RFC 8414 arrays are only sent when a fresh discovery produced them
//     for the URL currently in the field. Otherwise they are omitted so the
//     server keeps what it has (COALESCE narg semantics). Sending a stale
//     snapshot would overwrite discovered metadata with another issuer's.
export function buildUpdateIssuerForm(
  state: IssuerSettingsFormState,
): UpdateRemoteSessionIssuerForm {
  const issuer = state.issuerUrl.trim();
  const snapshot = state.discoveredSnapshot;
  const fromDiscovery = snapshot && snapshot.url === issuer ? snapshot : null;

  return {
    id: state.id,
    name: state.name.trim(),
    logoAssetId: state.logoAssetId.trim(),
    slug: state.slug.trim(),
    clientSetupDocumentationUrl: state.clientSetupDocumentationUrl.trim(),
    issuer,
    authorizationEndpoint: state.authorizationEndpoint.trim(),
    tokenEndpoint: state.tokenEndpoint.trim(),
    registrationEndpoint: state.registrationEndpoint.trim(),
    jwksUri: state.jwksUri.trim(),
    scopesSupported: fromDiscovery?.scopesSupported,
    grantTypesSupported: fromDiscovery?.grantTypesSupported,
    responseTypesSupported: fromDiscovery?.responseTypesSupported,
    tokenEndpointAuthMethodsSupported:
      fromDiscovery?.tokenEndpointAuthMethodsSupported,
    clientIdMetadataDocumentSupported:
      fromDiscovery?.clientIdMetadataDocumentSupported,
    revocationEndpoint: fromDiscovery?.revocationEndpoint,
    serviceDocumentation: fromDiscovery?.serviceDocumentation,
    opPolicyUri: fromDiscovery?.opPolicyUri,
    opTosUri: fromDiscovery?.opTosUri,
  };
}

// The state both create sheets collect: the update state minus the id, which
// does not exist yet.
export type IssuerCreateFormState = Omit<IssuerSettingsFormState, "id">;

// buildCreateIssuerForm turns a create sheet's state into the creation payload.
//
// Shared between the tenant CreateRemoteIdentityProviderSheet and the platform
// CreatePlatformIssuerSheet for the same reason buildUpdateIssuerForm is shared
// by the Settings tabs: the two tiers differ only in which endpoint they post
// to (createIssuer vs createGlobalIssuer) and the sheets stay deliberately
// separate UIs, so this builder is the one place the payload semantics live.
// The tenant caller spreads a projectId on top; its request body is otherwise
// structurally identical to CreateRemoteSessionIssuerForm.
//
// Creation semantics differ from update in two load-bearing ways:
//   - Empty optional strings are omitted (undefined), not sent as "". There is
//     no saved value to clear; undefined simply stores NULL.
//   - The RFC 8414 metadata arrays are NOT NULL server-side, so they are always
//     sent: what discovery returned, or empty arrays when the operator typed
//     everything by hand. The stale-snapshot guard is the same as update's —
//     a snapshot for a different URL than the one being submitted is ignored.
export function buildCreateIssuerForm(
  state: IssuerCreateFormState,
): CreateRemoteSessionIssuerForm {
  const issuer = state.issuerUrl.trim();
  const snapshot = state.discoveredSnapshot;
  const fromDiscovery = snapshot && snapshot.url === issuer ? snapshot : null;

  return {
    slug: state.slug.trim(),
    issuer,
    name: state.name.trim() || undefined,
    logoAssetId: state.logoAssetId.trim() || undefined,
    clientSetupDocumentationUrl:
      state.clientSetupDocumentationUrl.trim() || undefined,
    authorizationEndpoint: state.authorizationEndpoint.trim() || undefined,
    tokenEndpoint: state.tokenEndpoint.trim() || undefined,
    registrationEndpoint: state.registrationEndpoint.trim() || undefined,
    jwksUri: state.jwksUri.trim() || undefined,
    scopesSupported: fromDiscovery?.scopesSupported ?? [],
    grantTypesSupported: fromDiscovery?.grantTypesSupported ?? [],
    responseTypesSupported: fromDiscovery?.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      fromDiscovery?.tokenEndpointAuthMethodsSupported ?? [],
    // CIMD support is parsed during discovery and persisted here so the issuer
    // can offer the CIMD client type. Defaults false when the operator skipped
    // Discover and typed the endpoints by hand.
    clientIdMetadataDocumentSupported:
      fromDiscovery?.clientIdMetadataDocumentSupported ?? false,
    // The RFC 7009 revocation endpoint is discovery-only too, and undefined is
    // the ordinary case: plenty of issuers advertise none, and sessions minted
    // against those revoke locally with no upstream call.
    revocationEndpoint: fromDiscovery?.revocationEndpoint || undefined,
    // RFC 8414 documentation URLs are discovery-only — there are no form
    // inputs for them. Undefined when the operator skipped Discover or the
    // issuer advertised nothing usable.
    serviceDocumentation: fromDiscovery?.serviceDocumentation || undefined,
    opPolicyUri: fromDiscovery?.opPolicyUri || undefined,
    opTosUri: fromDiscovery?.opTosUri || undefined,
  };
}
