import { isNotFoundError } from "@/lib/errors";
import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import { pickPreferredAuthMethod } from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { CommitServerIdentityConfigurationResult } from "@gram/client/models/components/commitserveridentityconfigurationresult.js";
import type { ServerIdentityClientConfigurationTokenEndpointAuthMethod } from "@gram/client/models/components/serveridentityclientconfiguration.js";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

export type RemoteMcpCreationIdentity = "user" | "agent" | "none";

type ConfigureCreatedIdentityInput = {
  client: Gram;
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  identityMode: RemoteMcpCreationIdentity;
  agentAuthorization?: string;
  /** The server's issuer is organization-owned, so a project may not set it up. */
  organizationOwnedUserSessionIssuer?: boolean;
  options?: RequestOptions;
};

export type ConfigureCreatedIdentityResult =
  | {
      status: "configured";
      mcpServer: McpServer;
      identityMode: RemoteMcpCreationIdentity;
      userIdentity?: CommitServerIdentityConfigurationResult;
    }
  | {
      status: "setup-required";
      mcpServer: McpServer;
      identityMode: RemoteMcpCreationIdentity;
      message: string;
      userIdentity?: CommitServerIdentityConfigurationResult;
    };

// The issuer form's method list is wider than what this RPC accepts: it also
// carries private_key_jwt, which the composite cannot express. In practice
// pickPreferredAuthMethod never returns it — it falls back to
// client_secret_basic when nothing recognized is advertised — so this narrows
// the type at the boundary rather than changing which method is sent.
function rpcAuthMethod(
  supported: string[],
): ServerIdentityClientConfigurationTokenEndpointAuthMethod {
  switch (pickPreferredAuthMethod(supported)) {
    case "client_secret_post":
      return "client_secret_post";
    case "none":
      return "none";
    case "client_secret_basic":
    // The composite has no private_key_jwt; pickPreferredAuthMethod cannot
    // return it either, so this only satisfies exhaustiveness.
    case "private_key_jwt":
      return "client_secret_basic";
  }
}

export async function configureCreatedRemoteMcpIdentity({
  client,
  remoteMcpServer,
  mcpServer,
  identityMode,
  agentAuthorization,
  organizationOwnedUserSessionIssuer = false,
  options,
}: ConfigureCreatedIdentityInput): Promise<ConfigureCreatedIdentityResult> {
  if (identityMode === "none") {
    try {
      return configured(
        await setMcpServerVisibility(client, mcpServer, "private", options),
        identityMode,
      );
    } catch {
      return setupRequired(
        mcpServer,
        identityMode,
        "The server could not be enabled. Review it in Settings.",
      );
    }
  }

  if (identityMode === "agent") {
    const authorization = agentAuthorization?.trim();
    if (!authorization) {
      return setupRequired(
        mcpServer,
        identityMode,
        "Add an Agent Identity credential in Settings > Identity.",
      );
    }
    try {
      await client.remoteMcp.createServerHeader(
        {
          createServerHeaderForm: {
            remoteMcpServerId: remoteMcpServer.id,
            name: "Authorization",
            isRequired: true,
            isSecret: true,
            value: authorization,
          },
        },
        undefined,
        options,
      );
    } catch {
      return setupRequired(
        mcpServer,
        identityMode,
        "Agent Identity could not be configured. Add the credential in Settings > Identity.",
      );
    }

    try {
      return configured(
        await setMcpServerVisibility(client, mcpServer, "private", options),
        identityMode,
      );
    } catch {
      return setupRequired(
        mcpServer,
        identityMode,
        "Agent Identity was configured, but the server could not be enabled. Enable it from Settings.",
      );
    }
  }

  // Only User Identity touches the session issuer, and an organization-owned
  // one is not a project's to set up.
  if (organizationOwnedUserSessionIssuer) {
    return setupRequired(
      mcpServer,
      identityMode,
      "Organization user session issuers are configured by organization administrators.",
    );
  }

  if (!mcpServer.userSessionIssuerId) {
    return setupRequired(
      mcpServer,
      identityMode,
      "The server has no user identity session configuration.",
    );
  }

  const protectedResource = await discoverProtectedResource(
    client,
    remoteMcpServer,
    options,
  );
  const authorizationServer =
    protectedResource?.metadata?.authorizationServers?.[0];
  if (!protectedResource?.available || !authorizationServer) {
    return setupRequired(
      mcpServer,
      identityMode,
      "OAuth metadata could not be discovered. Configure User Identity in Settings > Identity.",
    );
  }

  let draft: RemoteSessionIssuerDraft;
  try {
    draft = await client.remoteSessionIssuers.fetchMetadata(
      {
        fetchIssuerMetadataRequestBody: { issuer: authorizationServer },
      },
      undefined,
      options,
    );
  } catch {
    return setupRequired(
      mcpServer,
      identityMode,
      "The authorization server metadata could not be discovered. Configure User Identity in Settings > Identity.",
    );
  }

  let provider: RemoteSessionIssuer | null;
  try {
    provider = await client.remoteSessionIssuers.get(
      { issuer: draft.issuer },
      undefined,
      options,
    );
  } catch (error) {
    if (!isNotFoundError(error)) {
      return setupRequired(
        mcpServer,
        identityMode,
        "Existing identity providers could not be checked. Configure User Identity in Settings > Identity.",
      );
    }
    provider = null;
  }

  if (
    provider &&
    (!provider.authorizationEndpoint || !provider.tokenEndpoint)
  ) {
    return setupRequired(
      mcpServer,
      identityMode,
      "The matching identity provider is missing OAuth endpoints. Update it in Remote Identity Providers.",
    );
  }
  if (!provider && (!draft.authorizationEndpoint || !draft.tokenEndpoint)) {
    return setupRequired(
      mcpServer,
      identityMode,
      "OAuth metadata is missing required endpoints. Configure User Identity in Settings > Identity.",
    );
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
          clientMode: "auto",
          clientConfiguration: {
            scope: scopes.length > 0 ? scopes : undefined,
            tokenEndpointAuthMethod: rpcAuthMethod(
              draft.tokenEndpointAuthMethodsSupported ?? [],
            ),
          },
        },
      },
      undefined,
      options,
    );
  } catch {
    return setupRequired(
      mcpServer,
      identityMode,
      "User Identity could not be configured. Open Settings > Identity to finish setup.",
    );
  }

  if (!result.status) {
    return setupRequired(
      mcpServer,
      identityMode,
      commitFailureMessage(result),
      result,
    );
  }

  try {
    return configured(
      await setMcpServerVisibility(client, mcpServer, "private", options),
      identityMode,
      result,
    );
  } catch {
    return setupRequired(
      mcpServer,
      identityMode,
      "User Identity was configured, but the server could not be enabled. Enable it from Settings.",
      result,
    );
  }
}

async function discoverProtectedResource(
  client: Gram,
  remoteMcpServer: RemoteMcpServer,
  options: RequestOptions | undefined,
) {
  try {
    return await client.remoteMcp.discoverProtectedResourceMetadata(
      {
        discoverProtectedResourceMetadataRequestBody: {
          remoteMcpServerId: remoteMcpServer.id,
        },
      },
      undefined,
      options,
    );
  } catch {
    return null;
  }
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

function commitFailureMessage(
  result: CommitServerIdentityConfigurationResult,
): string {
  if (result.failure) {
    const outcome =
      result.failure.outcome === "unreachable"
        ? "The identity provider was unreachable"
        : "The identity provider refused registration";
    return `${outcome}. Configure User Identity in Settings > Identity.`;
  }
  return "Automatic client registration is unavailable. Configure User Identity in Settings > Identity.";
}

async function setMcpServerVisibility(
  client: Gram,
  mcpServer: McpServer,
  visibility: McpServerVisibility,
  options?: RequestOptions,
): Promise<McpServer> {
  return await client.mcpServers.update(
    {
      updateMcpServerForm: {
        id: mcpServer.id,
        name: mcpServer.name ?? undefined,
        remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
        toolsetId: mcpServer.toolsetId ?? undefined,
        environmentId: mcpServer.environmentId ?? undefined,
        toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
        visibility,
      },
    },
    undefined,
    options,
  );
}

function preferredScopes(
  protectedResourceScopes: string[] | undefined,
  authorizationServerScopes: string[] | undefined,
): string[] {
  const resourceScopes = nonEmptyStrings(protectedResourceScopes);
  return resourceScopes.length > 0
    ? resourceScopes
    : nonEmptyStrings(authorizationServerScopes);
}

function nonEmptyStrings(values: string[] | undefined): string[] {
  return (values ?? [])
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

function configured(
  mcpServer: McpServer,
  identityMode: RemoteMcpCreationIdentity,
  userIdentity?: CommitServerIdentityConfigurationResult,
): ConfigureCreatedIdentityResult {
  return { status: "configured", mcpServer, identityMode, userIdentity };
}

function setupRequired(
  mcpServer: McpServer,
  identityMode: RemoteMcpCreationIdentity,
  message: string,
  userIdentity?: CommitServerIdentityConfigurationResult,
): ConfigureCreatedIdentityResult {
  return {
    status: "setup-required",
    mcpServer,
    identityMode,
    message,
    userIdentity,
  };
}
