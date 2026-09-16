import type { useSdkClient } from "@/contexts/Sdk";
import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { isNotFoundError } from "@/lib/route-errors";
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import { pickPreferredAuthMethod } from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { CommitServerIdentityConfigurationResult } from "@gram/client/models/components/commitserveridentityconfigurationresult.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import type { ServerIdentityRegistrationFailure } from "@gram/client/models/components/serveridentityregistrationfailure.js";

type SdkClient = ReturnType<typeof useSdkClient>;

/**
 * How far the sign-in provider got for one drafted server. The server itself is
 * already made by the time any of this runs, so none of these undo it.
 */
export type DraftIdentityOutcome =
  /** Working on it. */
  | { status: "configuring" }
  /** The upstream asked for no sign-in, so there is no provider to configure. */
  | { status: "none" }
  | { status: "configured"; providerId: string | undefined }
  | { status: "manual"; providerId: string | undefined }
  | { status: "failed"; message: string };

/**
 * What each registration failure means, in the administrator's terms. The
 * sentence is built from the bounded taxonomy the server reports and never
 * from the raw response: the reason codes say enough, and anything the
 * provider sent back is its own text to be kept off the card.
 */
const FAILURE_SENTENCE: Record<string, string> = {
  dns_error: "The sign-in provider's host could not be resolved.",
  tls_error: "The sign-in provider's certificate could not be verified.",
  timeout: "The sign-in provider did not answer in time.",
  network_error: "The sign-in provider could not be reached.",
  rate_limited: "The sign-in provider is rate limiting Speakeasy.",
  upstream_unavailable: "The sign-in provider is unavailable.",
  authorization_rejected: "The sign-in provider rejected the registration.",
  invalid_success_response:
    "The sign-in provider answered with something Speakeasy could not read.",
};

function failureSentence(failure: ServerIdentityRegistrationFailure): string {
  const known = FAILURE_SENTENCE[failure.reason];
  if (known) return known;
  return failure.outcome === "unreachable"
    ? "The sign-in provider could not be reached."
    : "The sign-in provider refused the registration.";
}

function providerForm(draft: RemoteSessionIssuerDraft, mcpServer: McpServer) {
  return {
    slug: buildUserSessionResourceSlug(mcpServer.slug ?? "mcp"),
    issuer: draft.issuer,
    name: deriveRemoteSessionIssuerNameFromUrl(draft.issuer) ?? undefined,
    authorizationEndpoint: draft.authorizationEndpoint,
    tokenEndpoint: draft.tokenEndpoint,
    registrationEndpoint: draft.registrationEndpoint,
    jwksUri: draft.jwksUri,
    scopesSupported: draft.scopesSupported ?? [],
    grantTypesSupported: draft.grantTypesSupported ?? [],
    responseTypesSupported: draft.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      draft.tokenEndpointAuthMethodsSupported ?? [],
    clientIdMetadataDocumentSupported: draft.clientIdMetadataDocumentSupported,
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

function nonEmptyStrings(values: string[] | undefined): string[] {
  return (values ?? [])
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

/** What the resource itself asks for wins; the provider's list is the fallback. */
function preferredScopes(
  protectedResourceScopes: string[] | undefined,
  authorizationServerScopes: string[] | undefined,
): string[] {
  const resourceScopes = nonEmptyStrings(protectedResourceScopes);
  return resourceScopes.length > 0
    ? resourceScopes
    : nonEmptyStrings(authorizationServerScopes);
}

const MANUAL = "manual" as const;

/**
 * Carries on from a drafted MCP server to the sign-in its upstream asks for:
 * probe the endpoint, read the protected resource metadata it points at,
 * discover the authorization server behind it, and commit that as the server's
 * user session provider with a client registered automatically where the
 * provider supports it.
 *
 * Lifted from the Remote MCP source create flow
 * (pages/sources/remote-mcp/configureCreatedIdentity.ts), less the identity
 * modes and the visibility update a draft does not need — the server is
 * already private by the time this runs.
 *
 * Nothing here can fail the server: every step that does not land returns an
 * outcome the card reports, and the draft stands either way.
 */
export async function configureDraftIdentity({
  client,
  remoteMcpServer,
  mcpServer,
  options,
}: {
  client: SdkClient;
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  options?: RequestOptions;
}): Promise<DraftIdentityOutcome> {
  let probe;
  try {
    probe = await client.remoteMcp.probeURL(
      { probeURLForm: { url: remoteMcpServer.url } },
      undefined,
      options,
    );
  } catch {
    // Nothing was asked of the upstream that it answered, so there is nothing
    // to say about sign-in: the draft stands on its own.
    return { status: "none" };
  }

  if (probe.outcome !== "authentication_required") {
    return { status: "none" };
  }
  if (!probe.protectedResourceMetadataUrl) {
    // It wants a sign-in but does not say where from, so there is nothing to
    // discover and the rest is the administrator's to fill in.
    return { status: MANUAL, providerId: undefined };
  }

  let protectedResource;
  try {
    protectedResource =
      await client.remoteMcp.discoverProtectedResourceMetadata(
        {
          discoverProtectedResourceMetadataRequestBody: {
            remoteMcpServerId: remoteMcpServer.id,
          },
        },
        undefined,
        options,
      );
  } catch {
    return { status: MANUAL, providerId: undefined };
  }

  const authorizationServer =
    protectedResource.metadata?.authorizationServers?.[0];
  if (!protectedResource.available || !authorizationServer) {
    return { status: MANUAL, providerId: undefined };
  }

  let draft: RemoteSessionIssuerDraft;
  try {
    draft = await client.remoteSessionIssuers.fetchMetadata(
      { fetchIssuerMetadataRequestBody: { issuer: authorizationServer } },
      undefined,
      options,
    );
  } catch {
    return { status: MANUAL, providerId: undefined };
  }

  // A provider the project already has for this issuer is the one to use: two
  // applications behind one authorization server share it rather than each
  // standing up their own.
  let provider: RemoteSessionIssuer | null;
  try {
    provider = await client.remoteSessionIssuers.get(
      { issuer: draft.issuer },
      undefined,
      options,
    );
  } catch (error) {
    if (!isNotFoundError(error))
      return { status: MANUAL, providerId: undefined };
    provider = null;
  }

  if (
    provider &&
    (!provider.authorizationEndpoint || !provider.tokenEndpoint)
  ) {
    return { status: MANUAL, providerId: undefined };
  }
  if (!provider && (!draft.authorizationEndpoint || !draft.tokenEndpoint)) {
    return { status: MANUAL, providerId: undefined };
  }

  const scopes = preferredScopes(
    protectedResource.metadata?.scopesSupported,
    draft.scopesSupported,
  );

  let result: CommitServerIdentityConfigurationResult;
  try {
    result = await client.remoteSessions.commitServerIdentityConfiguration(
      {
        commitServerIdentityConfigurationForm: {
          mcpServerId: mcpServer.id,
          providerId: provider?.id,
          createProvider: provider ? undefined : providerForm(draft, mcpServer),
          // Whichever the provider supports — dynamic registration or a client
          // ID metadata document — without asking the administrator which.
          clientMode: "auto",
          clientConfiguration: {
            scope: scopes.length > 0 ? scopes : undefined,
            tokenEndpointAuthMethod: pickPreferredAuthMethod(
              draft.tokenEndpointAuthMethodsSupported ?? [],
            ),
          },
        },
      },
      undefined,
      options,
    );
  } catch {
    return {
      status: "failed",
      message:
        "The sign-in provider could not be configured. Try again, or set it up from its own page.",
    };
  }

  // manual_setup_required is a preparation signal rather than a failure: auto
  // mode found no way to register a client, and nothing was changed.
  if (result.manualSetupRequired) {
    return { status: MANUAL, providerId: result.provider?.id ?? provider?.id };
  }
  if (result.failure) {
    return { status: "failed", message: failureSentence(result.failure) };
  }
  if (!result.status) {
    return {
      status: "failed",
      message: "The sign-in provider could not be configured.",
    };
  }
  return {
    status: "configured",
    providerId: result.provider?.id ?? provider?.id,
  };
}
