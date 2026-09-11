import type { CreateRemoteSessionIssuerForm } from "@gram/admin-client/models/components/createremotesessionissuerform";
import type { UpdateRemoteSessionIssuerForm } from "@gram/admin-client/models/components/updateremotesessionissuerform";

// Snapshot of the issuer + RFC 8414 metadata for a given Issuer URL. Created
// fresh on every successful discovery and seeded from saved records in the
// Modify sheet. Drives the Discover/Reset slot and the URL-change reset.
export type DiscoveredEndpoints = {
  url: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  scopesSupported: string[];
  grantTypesSupported: string[];
  responseTypesSupported: string[];
  tokenEndpointAuthMethodsSupported: string[];
  // PKCE methods (RFC 8414 code_challenge_methods_supported). Unlike the
  // arrays above this is tri-state, mirroring the nullable column: null means
  // the saved record was never captured, and it must round-trip as null so a
  // settings save does not turn "never captured" into "advertises no methods"
  // (an empty array — a refusal state under future PKCE enforcement). A
  // fresh-discovery snapshot is never null: an omitted field captures as [].
  codeChallengeMethodsSupported: string[] | null;
  // OAuth CIMD draft capability parsed from the discovery document: whether the
  // issuer accepts a Client ID Metadata Document URL as client_id. Persisted on
  // create/update so the CIMD client type can be offered for this issuer.
  clientIdMetadataDocumentSupported: boolean;
  // RFC 7009 revocation endpoint parsed from the discovery document. Carried
  // through create/update but not rendered as an input, like the capability
  // arrays above and the documentation URLs below: an operator never needs to
  // hand-write it, and re-running discovery is the way to correct a stale one.
  // An empty string means the issuer advertised none, which is common — the
  // sessions minted against such an issuer simply revoke locally.
  revocationEndpoint: string;
  // RFC 8414 documentation URLs parsed from the discovery document. An empty
  // string means the issuer advertised nothing usable — the update payload sends
  // it through verbatim, which the server reads as "clear to NULL", so a URL the
  // issuer stopped advertising does not linger.
  serviceDocumentation: string;
  opPolicyUri: string;
  opTosUri: string;
  // OIDC userinfo / RFC 7662 introspection endpoints; "" = not advertised.
  userinfoEndpoint: string;
  introspectionEndpoint: string;
  // Tri-state like codeChallengeMethodsSupported: null = never captured.
  introspectionEndpointAuthMethodsSupported: string[] | null;
  idTokenSigningAlgValuesSupported: string[] | null;
  claimsSupported: string[] | null;
  backchannelLogoutSupported: boolean | null;
  authorizationResponseIssParameterSupported: boolean | null;
};

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

// Updates send empty strings to clear saved values. Only matching discovery
// metadata is sent; omission preserves the stored value.
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
    // Tri-state: a seeded snapshot of a never-captured record holds null,
    // which must go out as undefined (omit; the server keeps its stored
    // value) — sending [] instead would record "the issuer advertises no
    // methods", a refusal state under future PKCE enforcement.
    codeChallengeMethodsSupported:
      fromDiscovery?.codeChallengeMethodsSupported ?? undefined,
    clientIdMetadataDocumentSupported:
      fromDiscovery?.clientIdMetadataDocumentSupported,
    revocationEndpoint: fromDiscovery?.revocationEndpoint,
    serviceDocumentation: fromDiscovery?.serviceDocumentation,
    opPolicyUri: fromDiscovery?.opPolicyUri,
    opTosUri: fromDiscovery?.opTosUri,
    // Endpoints verbatim ("" clears a dropped URL); null seeded = keep stored.
    userinfoEndpoint: fromDiscovery?.userinfoEndpoint,
    introspectionEndpoint: fromDiscovery?.introspectionEndpoint,
    introspectionEndpointAuthMethodsSupported:
      fromDiscovery?.introspectionEndpointAuthMethodsSupported ?? undefined,
    idTokenSigningAlgValuesSupported:
      fromDiscovery?.idTokenSigningAlgValuesSupported ?? undefined,
    claimsSupported: fromDiscovery?.claimsSupported ?? undefined,
    backchannelLogoutSupported:
      fromDiscovery?.backchannelLogoutSupported ?? undefined,
    authorizationResponseIssParameterSupported:
      fromDiscovery?.authorizationResponseIssParameterSupported ?? undefined,
  };
}

// Creation omits empty optional strings and defaults non-null metadata arrays
// to empty. Nullable capabilities retain never-captured versus captured-empty.
export function buildCreateIssuerForm(
  state: Omit<IssuerSettingsFormState, "id">,
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
    // No `?? []` default, unlike the NOT NULL arrays above: the column is
    // nullable, and an operator who typed endpoints by hand has not captured
    // the field — omitting it stores NULL ("not captured"), while [] would
    // claim the issuer advertises no methods. A fresh discovery snapshot is
    // never null, so a discovered create records what the document said.
    codeChallengeMethodsSupported:
      fromDiscovery?.codeChallengeMethodsSupported ?? undefined,
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
    // Discovery-only capabilities; omitted (NULL) unless discovery ran.
    userinfoEndpoint: fromDiscovery?.userinfoEndpoint || undefined,
    introspectionEndpoint: fromDiscovery?.introspectionEndpoint || undefined,
    introspectionEndpointAuthMethodsSupported:
      fromDiscovery?.introspectionEndpointAuthMethodsSupported ?? undefined,
    idTokenSigningAlgValuesSupported:
      fromDiscovery?.idTokenSigningAlgValuesSupported ?? undefined,
    claimsSupported: fromDiscovery?.claimsSupported ?? undefined,
    backchannelLogoutSupported:
      fromDiscovery?.backchannelLogoutSupported ?? undefined,
    authorizationResponseIssParameterSupported:
      fromDiscovery?.authorizationResponseIssParameterSupported ?? undefined,
  };
}

// Derive a unique slug from the Issuer URL's hostname. Mirrors the hyphen-style
// transform an operator would reasonably hand-write so the auto-filled value
// looks natural. Returns null for unparseable URLs — callers keep the prior slug
// in that case so partial typing doesn't blow it away.
export function deriveSlugFromUrl(url: string): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    if (!host) return null;
    const slug = host
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return slug || null;
  } catch {
    return null;
  }
}

// Derive a default Display name from the Issuer URL's hostname. Unlike the slug
// transform this keeps the hostname human-readable (no hyphenation/lowercasing).
// Returns null for unparseable URLs so callers leave the prior value intact while
// a partial URL is being typed.
export function deriveNameFromUrl(url: string): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    return host || null;
  } catch {
    return null;
  }
}
