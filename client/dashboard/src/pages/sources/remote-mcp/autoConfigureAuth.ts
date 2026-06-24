import type { Gram } from "@gram/client";
import {
  type McpServer,
  type ProtectedResourceMetadata,
  type RemoteMcpServer,
  type RemoteSessionClient,
  type RemoteSessionIssuer,
  type RemoteSessionIssuerDraft,
} from "@gram/client/models/components";

import {
  buildUserSessionResourceSlug,
  DEFAULT_USER_SESSION_DURATION_HOURS,
} from "@/lib/externalMcpUserSessions";
import {
  type AuthedFetch,
  proxyRegisterUpstreamClient,
} from "@/lib/proxyRegisterUpstreamClient";
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import {
  narrowTokenEndpointAuthMethod,
  pickPreferredAuthMethod,
} from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";

type AutoConfigureAuthInput = {
  client: Gram;
  authedFetch: AuthedFetch;
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
};

export type AutoConfigureAuthResult =
  | {
      status: "configured";
      mcpServer: McpServer;
      remoteSessionIssuerId: string;
      userSessionIssuerId: string;
    }
  | {
      status: "skipped";
      message: string;
      warn: boolean;
    };

const SILENT_NO_METADATA_MESSAGE =
  "No OAuth protected-resource metadata was discovered.";

export async function autoConfigureRemoteMcpAuth({
  client,
  authedFetch,
  remoteMcpServer,
  mcpServer,
}: AutoConfigureAuthInput): Promise<AutoConfigureAuthResult> {
  let protectedResourceMetadata: ProtectedResourceMetadata | undefined;
  try {
    const protectedResource =
      await client.remoteMcp.discoverProtectedResourceMetadata({
        discoverProtectedResourceMetadataRequestBody: {
          remoteMcpServerId: remoteMcpServer.id,
        },
      });
    protectedResourceMetadata = protectedResource.metadata;
    if (
      !protectedResource.available ||
      !protectedResourceMetadata?.authorizationServers?.[0]
    ) {
      return skipped(SILENT_NO_METADATA_MESSAGE, false);
    }
  } catch (error) {
    console.info("Remote MCP OAuth protected-resource discovery failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      error,
    });
    return skipped(SILENT_NO_METADATA_MESSAGE, false);
  }

  let draft: RemoteSessionIssuerDraft;
  try {
    draft = await client.remoteSessionIssuers.discover({
      discoverRemoteSessionIssuerRequestBody: {
        issuer: protectedResourceMetadata.authorizationServers[0],
      },
    });
  } catch (error) {
    console.info("Remote MCP auth-server discovery failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      issuer: protectedResourceMetadata.authorizationServers[0],
      error,
    });
    return skipped(
      "OAuth metadata was found, but the authorization server could not be discovered.",
      true,
    );
  }

  let existingIssuer: RemoteSessionIssuer | null;
  try {
    existingIssuer = await findMatchingIssuer(
      client,
      mcpServer.projectId,
      draft.issuer,
    );
  } catch (error) {
    console.info("Remote MCP matching issuer lookup failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      issuer: draft.issuer,
      error,
    });
    return skipped(
      "OAuth metadata was found, but existing identity providers could not be checked.",
      true,
    );
  }
  if (
    existingIssuer &&
    (!existingIssuer.authorizationEndpoint || !existingIssuer.tokenEndpoint)
  ) {
    return skipped(
      "A matching identity provider already exists, but it is missing OAuth endpoints.",
      true,
    );
  }

  // When the matched issuer already has a working remote session client,
  // reuse the whole stack — point the server at the client's
  // user_session_issuer instead of registering a duplicate client. This is
  // also the only path that works when the IdP doesn't support DCR.
  if (existingIssuer) {
    let existingClient: RemoteSessionClient | null;
    try {
      existingClient = await findIssuerClient(client, existingIssuer.id);
    } catch (error) {
      console.info("Remote MCP issuer client lookup failed.", {
        remoteMcpServerId: remoteMcpServer.id,
        remoteSessionIssuerId: existingIssuer.id,
        error,
      });
      return skipped(
        "A matching identity provider was found, but its OAuth clients could not be checked.",
        true,
      );
    }
    const reusableUserSessionIssuerId = existingClient?.userSessionIssuerIds[0];
    if (existingClient && reusableUserSessionIssuerId) {
      try {
        const updatedMcpServer = await pointMcpServerAtUserSessionIssuer(
          client,
          mcpServer,
          reusableUserSessionIssuerId,
        );
        return {
          status: "configured",
          mcpServer: updatedMcpServer,
          remoteSessionIssuerId: existingIssuer.id,
          userSessionIssuerId: reusableUserSessionIssuerId,
        };
      } catch (error) {
        console.info("Remote MCP existing issuer attachment failed.", {
          remoteMcpServerId: remoteMcpServer.id,
          remoteSessionIssuerId: existingIssuer.id,
          userSessionIssuerId: reusableUserSessionIssuerId,
          error,
        });
        return skipped(
          "A matching identity provider was found, but attaching it failed. You can configure it from the Authentication tab.",
          true,
        );
      }
    }
  }

  if (!draft.registrationEndpoint) {
    return skipped(
      "OAuth metadata was found, but automatic authentication setup requires dynamic client registration.",
      true,
    );
  }
  if (
    !existingIssuer &&
    (!draft.authorizationEndpoint || !draft.tokenEndpoint)
  ) {
    return skipped(
      "OAuth metadata was found, but it is missing required OAuth endpoints.",
      true,
    );
  }

  const scopes = preferredScopes(
    protectedResourceMetadata.scopesSupported,
    draft.scopesSupported,
  );
  const preferredAuthMethod = pickPreferredAuthMethod(
    draft.tokenEndpointAuthMethodsSupported ?? [],
  );

  let registered;
  try {
    registered = await proxyRegisterUpstreamClient(authedFetch, {
      registrationEndpoint: draft.registrationEndpoint,
      scope: scopes.length > 0 ? scopes.join(" ") : undefined,
      tokenEndpointAuthMethod: preferredAuthMethod,
    });
  } catch (error) {
    console.info("Remote MCP upstream DCR failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      registrationEndpoint: draft.registrationEndpoint,
      error,
    });
    return skipped(
      "OAuth metadata was found, but upstream dynamic client registration failed.",
      true,
    );
  }

  const resourceSlug = buildUserSessionResourceSlug(mcpServer.slug ?? "mcp");
  let createdRemoteSessionIssuerId: string | undefined;
  let createdUserSessionIssuerId: string | undefined;

  try {
    const remoteSessionIssuer =
      existingIssuer ??
      (await client.remoteSessionIssuers.create({
        createRemoteSessionIssuerForm: {
          slug: resourceSlug,
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
          oidc: draft.oidc,
          passthrough: draft.passthrough,
        },
      }));

    if (!existingIssuer) {
      createdRemoteSessionIssuerId = remoteSessionIssuer.id;
    }

    const userSessionIssuer = await client.userSessionIssuers.create({
      createUserSessionIssuerForm: {
        slug: resourceSlug,
        authnChallengeMode: "interactive",
        sessionDurationHours: DEFAULT_USER_SESSION_DURATION_HOURS,
      },
    });
    createdUserSessionIssuerId = userSessionIssuer.id;

    await client.remoteSessionClients.create({
      createRemoteSessionClientForm: {
        remoteSessionIssuerId: remoteSessionIssuer.id,
        userSessionIssuerIds: [userSessionIssuer.id],
        clientId: registered.clientId,
        clientSecret: registered.clientSecret || undefined,
        tokenEndpointAuthMethod:
          narrowTokenEndpointAuthMethod(registered.tokenEndpointAuthMethod) ??
          preferredAuthMethod,
        scope: scopes.length > 0 ? scopes : undefined,
      },
    });

    const updatedMcpServer = await pointMcpServerAtUserSessionIssuer(
      client,
      mcpServer,
      userSessionIssuer.id,
    );

    return {
      status: "configured",
      mcpServer: updatedMcpServer,
      remoteSessionIssuerId: remoteSessionIssuer.id,
      userSessionIssuerId: userSessionIssuer.id,
    };
  } catch (error) {
    console.info("Remote MCP authentication auto-configuration failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      error,
    });
    await cleanupCreatedAuthRecords(client, {
      remoteSessionIssuerId: createdRemoteSessionIssuerId,
      userSessionIssuerId: createdUserSessionIssuerId,
    });
    return skipped(
      "Automatic authentication setup failed. You can configure it from the Authentication tab.",
      true,
    );
  }
}

// Full-record replace: updateMcpServer nulls omitted fields, so re-send the
// server's existing references alongside the new issuer linkage.
async function pointMcpServerAtUserSessionIssuer(
  client: Gram,
  mcpServer: McpServer,
  userSessionIssuerId: string,
): Promise<McpServer> {
  return await client.mcpServers.update({
    updateMcpServerForm: {
      id: mcpServer.id,
      name: mcpServer.name ?? undefined,
      remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
      toolsetId: mcpServer.toolsetId ?? undefined,
      environmentId: mcpServer.environmentId ?? undefined,
      toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
      visibility: "private",
      userSessionIssuerId,
    },
  });
}

async function findIssuerClient(
  client: Gram,
  remoteSessionIssuerId: string,
): Promise<RemoteSessionClient | null> {
  const pages = await client.remoteSessionClients.list({
    remoteSessionIssuerId,
    limit: 1,
  });

  for await (const page of pages) {
    const match = page.result.items[0];
    if (match) return match;
  }

  return null;
}

async function findMatchingIssuer(
  client: Gram,
  projectId: string,
  discoveredIssuer: string,
): Promise<RemoteSessionIssuer | null> {
  const normalized = normalizeIssuerURL(discoveredIssuer);
  let organizationMatch: RemoteSessionIssuer | null = null;
  const pages = await client.remoteSessionIssuers.list({ limit: 100 });

  for await (const page of pages) {
    for (const issuer of page.result.items) {
      if (normalizeIssuerURL(issuer.issuer) !== normalized) continue;
      if (issuer.projectId === projectId) return issuer;
      if (!issuer.projectId && !organizationMatch) {
        organizationMatch = issuer;
      }
    }
  }

  return organizationMatch;
}

function preferredScopes(
  protectedResourceScopes: string[] | undefined,
  authorizationServerScopes: string[] | undefined,
): string[] {
  const scopes = nonEmptyStrings(protectedResourceScopes);
  if (scopes.length > 0) return scopes;
  return nonEmptyStrings(authorizationServerScopes);
}

function nonEmptyStrings(values: string[] | undefined): string[] {
  return (values ?? [])
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

function normalizeIssuerURL(value: string): string {
  return value.replace(/\/+$/g, "");
}

async function cleanupCreatedAuthRecords(
  client: Gram,
  {
    remoteSessionIssuerId,
    userSessionIssuerId,
  }: {
    remoteSessionIssuerId: string | undefined;
    userSessionIssuerId: string | undefined;
  },
): Promise<void> {
  if (userSessionIssuerId) {
    try {
      await client.userSessionIssuers.delete({ id: userSessionIssuerId });
    } catch (error) {
      console.info("Failed to clean up auto-created user session issuer.", {
        userSessionIssuerId,
        error,
      });
    }
  }

  if (remoteSessionIssuerId) {
    try {
      await client.remoteSessionIssuers.delete({ id: remoteSessionIssuerId });
    } catch (error) {
      console.info("Failed to clean up auto-created remote session issuer.", {
        remoteSessionIssuerId,
        error,
      });
    }
  }
}

function skipped(
  message: string,
  warn: boolean,
): Extract<AutoConfigureAuthResult, { status: "skipped" }> {
  return { status: "skipped", message, warn };
}
