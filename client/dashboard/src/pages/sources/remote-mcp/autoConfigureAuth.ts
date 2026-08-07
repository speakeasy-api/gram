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
import { isNotFoundError } from "@/lib/errors";
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
    draft = await client.remoteSessionIssuers.fetchMetadata(
      {
        fetchIssuerMetadataRequestBody: {
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

  // Checked before the issuer is looked up, not after. A miss below creates the
  // issuer, and one created for an upstream that cannot do dynamic client
  // registration could never receive a client — so a server without DCR must
  // bail out before anything is written.
  if (!draft.registrationEndpoint) {
    return skipped(
      "OAuth metadata was found, but automatic authentication setup requires dynamic client registration.",
      true,
    );
  }

  // Ask the server whether this upstream already has an identity provider — in
  // this project, inherited from the organization, or in the platform catalog.
  // A 404 is the normal answer for a new upstream and means "create one". The
  // lookup lives server-side so that the tier precedence and the URL
  // normalization behind it have a single definition, rather than every caller
  // scanning the issuer list and matching URLs itself.
  const resourceSlug = buildUserSessionResourceSlug(mcpServer.slug ?? "mcp");
  let remoteSessionIssuer: RemoteSessionIssuer | null;
  try {
    remoteSessionIssuer = await client.remoteSessionIssuers.get(
      { issuer: draft.issuer },
      undefined,
      options,
    );
  } catch (error) {
    if (!isNotFoundError(error)) {
      console.info("Remote MCP identity provider lookup failed.", {
        remoteMcpServerId: remoteMcpServer.id,
        issuer: draft.issuer,
        error,
      });
      return skipped(
        "OAuth metadata was found, but existing identity providers could not be checked.",
        true,
      );
    }
    remoteSessionIssuer = null;
  }

  // An issuer that predates discovery may carry no endpoints, and reusing one
  // that cannot authorize would produce a client that never works.
  if (
    remoteSessionIssuer &&
    (!remoteSessionIssuer.authorizationEndpoint ||
      !remoteSessionIssuer.tokenEndpoint)
  ) {
    return skipped(
      "A matching identity provider already exists, but it is missing OAuth endpoints.",
      true,
    );
  }
  if (
    !remoteSessionIssuer &&
    (!draft.authorizationEndpoint || !draft.tokenEndpoint)
  ) {
    return skipped(
      "OAuth metadata was found, but it is missing required OAuth endpoints.",
      true,
    );
  }

  if (!remoteSessionIssuer) {
    try {
      remoteSessionIssuer = await client.remoteSessionIssuers.create(
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
      );
    } catch (error) {
      console.info("Remote MCP identity provider creation failed.", {
        remoteMcpServerId: remoteMcpServer.id,
        issuer: draft.issuer,
        error,
      });
      return skipped(
        "OAuth metadata was found, but an identity provider for it could not be created.",
        true,
      );
    }
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

  // No rollback of a newly created issuer if the steps below fail. The lookup
  // above is keyed on the upstream URL, so an issuer left behind is exactly what
  // the next attempt finds and reuses; deleting it would force a re-create on
  // every retry. The trade is that an abandoned install can leave an issuer with
  // no clients in the project's list, which is deletable from the UI.
  try {
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

function skipped(
  message: string,
  warn: boolean,
): Extract<AutoConfigureAuthResult, { status: "skipped" }> {
  return { status: "skipped", message, warn };
}
