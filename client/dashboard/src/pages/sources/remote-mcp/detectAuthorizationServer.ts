import { isNotFoundError } from "@/lib/errors";
import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { readableServiceNameFromUrl } from "@/lib/sources";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { CreateRemoteSessionIssuerForm } from "@gram/client/models/components/createremotesessionissuerform.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

/**
 * Which authorization server a remote MCP server's people sign in with, as
 * the server itself advertises it.
 *
 * - `existing`: the project already has that authorization server.
 * - `new`: it does not; `draft` holds its discovered metadata, ready to be
 *   added.
 * - `unavailable`: it could not be worked out, and `reason` says which step
 *   failed.
 */
export type DetectedAuthorizationServer =
  | { kind: "existing"; issuer: RemoteSessionIssuer }
  | { kind: "new"; draft: RemoteSessionIssuerDraft }
  | { kind: "unavailable"; reason: DetectionFailure };

export type DetectionFailure =
  /** The server advertises no OAuth protected-resource metadata. */
  | "no_metadata"
  /** Existing authorization servers could not be checked. */
  | "lookup_failed"
  /** The matching authorization server has no OAuth endpoints. */
  | "existing_incomplete"
  /** The authorization server's own metadata could not be fetched. */
  | "metadata_failed"
  /** The metadata named a different issuer than the server pointed at. */
  | "issuer_mismatch"
  /** The metadata lacks the authorization or token endpoint. */
  | "missing_endpoints";

/**
 * Looks up the authorization server a remote MCP server advertises: an
 * existing one first, since a saved one carries what sign-in needs and a
 * private issuer cannot be discovered from the browser at all, then its
 * metadata when the project does not have it yet.
 */
export async function detectAuthorizationServer(
  client: Gram,
  remoteMcpServerId: string,
  options?: RequestOptions,
): Promise<DetectedAuthorizationServer> {
  let authorizationServer: string | undefined;
  try {
    const resource = await client.remoteMcp.discoverProtectedResourceMetadata(
      {
        discoverProtectedResourceMetadataRequestBody: { remoteMcpServerId },
      },
      undefined,
      options,
    );
    authorizationServer = resource.available
      ? resource.metadata?.authorizationServers?.[0]
      : undefined;
  } catch {
    authorizationServer = undefined;
  }
  if (!authorizationServer) {
    return { kind: "unavailable", reason: "no_metadata" };
  }

  let issuer: RemoteSessionIssuer | null;
  try {
    issuer = await client.remoteSessionIssuers.get(
      { issuer: authorizationServer },
      undefined,
      options,
    );
  } catch (error) {
    if (!isNotFoundError(error)) {
      return { kind: "unavailable", reason: "lookup_failed" };
    }
    issuer = null;
  }
  if (issuer) {
    return issuer.authorizationEndpoint && issuer.tokenEndpoint
      ? { kind: "existing", issuer }
      : { kind: "unavailable", reason: "existing_incomplete" };
  }

  let draft: RemoteSessionIssuerDraft;
  try {
    draft = await client.remoteSessionIssuers.fetchMetadata(
      { fetchIssuerMetadataRequestBody: { issuer: authorizationServer } },
      undefined,
      options,
    );
  } catch {
    return { kind: "unavailable", reason: "metadata_failed" };
  }
  // RFC 8414 requires the document to name the issuer it was fetched for. A
  // mismatch means the resource pointed at one authorization server and got
  // another's metadata, and the server is created from `draft.issuer`, so
  // accepting it would bind sign-in to whichever issuer the document claimed.
  // Exact equality, deliberately: no trailing-slash normalization, which
  // belongs to readers and not to a write like this.
  if (draft.issuer !== authorizationServer) {
    return { kind: "unavailable", reason: "issuer_mismatch" };
  }
  if (!draft.authorizationEndpoint || !draft.tokenEndpoint) {
    return { kind: "unavailable", reason: "missing_endpoints" };
  }
  return { kind: "new", draft };
}

/** The form that adds a discovered authorization server to the project. */
export function issuerFormFromDraft(
  draft: RemoteSessionIssuerDraft,
  baseSlug: string,
): CreateRemoteSessionIssuerForm {
  return {
    slug: buildUserSessionResourceSlug(baseSlug),
    issuer: draft.issuer,
    name: readableServiceNameFromUrl(draft.issuer) ?? undefined,
    authorizationEndpoint: draft.authorizationEndpoint,
    tokenEndpoint: draft.tokenEndpoint,
    registrationEndpoint: draft.registrationEndpoint,
    jwksUri: draft.jwksUri,
    scopesSupported: draft.scopesSupported ?? [],
    grantTypesSupported: draft.grantTypesSupported ?? [],
    authorizationGrantProfilesSupported:
      draft.authorizationGrantProfilesSupported ?? [],
    responseTypesSupported: draft.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      draft.tokenEndpointAuthMethodsSupported ?? [],
    codeChallengeMethodsSupported:
      draft.codeChallengeMethodsSupported ?? undefined,
    clientIdMetadataDocumentSupported: draft.clientIdMetadataDocumentSupported,
    serviceDocumentation: draft.serviceDocumentation,
    userinfoEndpoint: draft.userinfoEndpoint,
    introspectionEndpoint: draft.introspectionEndpoint,
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
