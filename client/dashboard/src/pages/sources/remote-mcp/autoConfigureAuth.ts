import type { Gram } from "@gram/client";
import { type RequestOptions } from "@gram/client/lib/sdks.js";
import {
  type McpServer,
  type McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { type ProtectedResourceMetadata } from "@gram/client/models/components/protectedresourcemetadata.js";
import { type RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { type RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { type RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import {
  type AuthedFetch,
  proxyRegisterUpstreamClient,
} from "@/lib/proxyRegisterUpstreamClient";
import {
  deriveRemoteSessionIssuerNameFromUrl,
  resolveRemoteSessionIssuerByTierPrecedence,
} from "@/lib/sources";
import {
  narrowTokenEndpointAuthMethod,
  pickPreferredAuthMethod,
} from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";

type AutoConfigureAuthInput = {
  client: Gram;
  authedFetch: AuthedFetch;
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  /**
   * Per-request SDK options (e.g. a gram-project header for cross-project
   * installs) applied to every management API call made during auto-config.
   */
  options?: RequestOptions;
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
  options,
}: AutoConfigureAuthInput): Promise<AutoConfigureAuthResult> {
  // Every remote-backed server gets its USI at setup; auto-config only attaches
  // a client under it, never creates one. No USI means nothing to anchor a
  // client to, so skip silently (setup already surfaced the link failure).
  const userSessionIssuerId = mcpServer.userSessionIssuerId;
  if (!userSessionIssuerId) {
    return skipped("No user session issuer is linked to this server.", false);
  }

  let protectedResourceMetadata: ProtectedResourceMetadata | undefined;
  try {
    const protectedResource =
      await client.remoteMcp.discoverProtectedResourceMetadata(
        {
          discoverProtectedResourceMetadataRequestBody: {
            remoteMcpServerId: remoteMcpServer.id,
          },
        },
        undefined,
        options,
      );
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
    draft = await client.remoteSessionIssuers.discover(
      {
        discoverRemoteSessionIssuerRequestBody: {
          issuer: protectedResourceMetadata.authorizationServers[0],
        },
      },
      undefined,
      options,
    );
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
    existingIssuer = await findMatchingIssuer(client, draft.issuer, options);
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

  try {
    const remoteSessionIssuer =
      existingIssuer ??
      (await client.remoteSessionIssuers.create(
        {
          createRemoteSessionIssuerForm: {
            slug: resourceSlug,
            issuer: draft.issuer,
            name:
              deriveRemoteSessionIssuerNameFromUrl(draft.issuer) ?? undefined,
            authorizationEndpoint: draft.authorizationEndpoint,
            tokenEndpoint: draft.tokenEndpoint,
            registrationEndpoint: draft.registrationEndpoint,
            jwksUri: draft.jwksUri,
            scopesSupported: draft.scopesSupported ?? [],
            grantTypesSupported: draft.grantTypesSupported ?? [],
            responseTypesSupported: draft.responseTypesSupported ?? [],
            tokenEndpointAuthMethodsSupported:
              draft.tokenEndpointAuthMethodsSupported ?? [],
            clientIdMetadataDocumentSupported:
              draft.clientIdMetadataDocumentSupported,
            oidc: draft.oidc,
            passthrough: draft.passthrough,
          },
        },
        undefined,
        options,
      ));

    if (!existingIssuer) {
      createdRemoteSessionIssuerId = remoteSessionIssuer.id;
    }

    // Attach the freshly-registered upstream client to the server's permanent
    // USI.
    await client.remoteSessionClients.create(
      {
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: remoteSessionIssuer.id,
          userSessionIssuerIds: [userSessionIssuerId],
          clientId: registered.clientId,
          clientSecret: registered.clientSecret || undefined,
          tokenEndpointAuthMethod:
            narrowTokenEndpointAuthMethod(registered.tokenEndpointAuthMethod) ??
            preferredAuthMethod,
          scope: scopes.length > 0 ? scopes : undefined,
        },
      },
      undefined,
      options,
    );

    const updatedMcpServer = await setMcpServerVisibility(
      client,
      mcpServer,
      "private",
      options,
    );

    return {
      status: "configured",
      mcpServer: updatedMcpServer,
      remoteSessionIssuerId: remoteSessionIssuer.id,
      userSessionIssuerId,
    };
  } catch (error) {
    console.info("Remote MCP authentication auto-configuration failed.", {
      remoteMcpServerId: remoteMcpServer.id,
      error,
    });
    // Clean up only a newly-created issuer. The USI is the server's permanent
    // identity and must survive a failed client registration.
    await cleanupCreatedRemoteSessionIssuer(
      client,
      createdRemoteSessionIssuerId,
      options,
    );
    return skipped(
      "Automatic authentication setup failed. You can configure it from the Authentication tab.",
      true,
    );
  }
}

// Full-record replace: updateMcpServer nulls omitted fields, so re-send the
// server's existing references alongside the new visibility.
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

// findMatchingIssuer resolves the issuer a discovered authorization-server URL
// should reuse. The listing is project-scoped, so any project-tier match belongs
// to this project; resolution is by tier precedence — project > organization >
// platform — never by list order, which is by creation time and would otherwise
// pick an organization or platform issuer over an equally-valid project one
// depending on when each was created.
async function findMatchingIssuer(
  client: Gram,
  discoveredIssuer: string,
  options?: RequestOptions,
): Promise<RemoteSessionIssuer | null> {
  const normalized = normalizeIssuerURL(discoveredIssuer);
  const matches: RemoteSessionIssuer[] = [];
  const pages = await client.remoteSessionIssuers.list(
    { limit: 100 },
    undefined,
    options,
  );

  for await (const page of pages) {
    for (const issuer of page.result.items) {
      if (normalizeIssuerURL(issuer.issuer) === normalized) {
        matches.push(issuer);
      }
    }
  }

  return resolveRemoteSessionIssuerByTierPrecedence(matches) ?? null;
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

async function cleanupCreatedRemoteSessionIssuer(
  client: Gram,
  remoteSessionIssuerId: string | undefined,
  options?: RequestOptions,
): Promise<void> {
  if (!remoteSessionIssuerId) return;
  try {
    await client.remoteSessionIssuers.delete(
      { id: remoteSessionIssuerId },
      undefined,
      options,
    );
  } catch (error) {
    console.info("Failed to clean up auto-created remote session issuer.", {
      remoteSessionIssuerId,
      error,
    });
  }
}

function skipped(
  message: string,
  warn: boolean,
): Extract<AutoConfigureAuthResult, { status: "skipped" }> {
  return { status: "skipped", message, warn };
}
