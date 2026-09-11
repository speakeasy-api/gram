import { isNotFoundError } from "@/lib/errors";
import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import { pickPreferredAuthMethod } from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { CommitServerUserIdentityConfigurationResult } from "@gram/client/models/components/commitserveruseridentityconfigurationresult.js";
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
  options?: RequestOptions;
};

export type ConfigureCreatedIdentityResult =
  | {
      status: "configured";
      mcpServer: McpServer;
      identityMode: RemoteMcpCreationIdentity;
      userIdentity?: CommitServerUserIdentityConfigurationResult;
    }
  | {
      status: "setup-required";
      mcpServer: McpServer;
      identityMode: RemoteMcpCreationIdentity;
      message: string;
      userIdentity?: CommitServerUserIdentityConfigurationResult;
    };

export async function configureCreatedRemoteMcpIdentity({
  client,
  remoteMcpServer,
  mcpServer,
  identityMode,
  agentAuthorization,
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
  let result: CommitServerUserIdentityConfigurationResult;
  try {
    result = await client.remoteSessions.commitServerUserIdentityConfiguration(
      {
        commitServerUserIdentityConfigurationForm: {
          mcpServerId: mcpServer.id,
          providerId: provider?.id,
          createProvider: provider ? undefined : providerForm(draft, mcpServer),
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
  result: CommitServerUserIdentityConfigurationResult,
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
  userIdentity?: CommitServerUserIdentityConfigurationResult,
): ConfigureCreatedIdentityResult {
  return { status: "configured", mcpServer, identityMode, userIdentity };
}

function setupRequired(
  mcpServer: McpServer,
  identityMode: RemoteMcpCreationIdentity,
  message: string,
  userIdentity?: CommitServerUserIdentityConfigurationResult,
): ConfigureCreatedIdentityResult {
  return {
    status: "setup-required",
    mcpServer,
    identityMode,
    message,
    userIdentity,
  };
}
