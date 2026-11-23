import { requireKnownEnumValue } from "@/lib/enum";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import { isForbiddenError } from "@/lib/errors";
import {
  detectAuthorizationServer,
  type DetectionFailure,
  issuerFormFromDraft,
} from "./detectAuthorizationServer";
import { serverIdentityAuthMethod } from "@/lib/remote-identity/model/clientConfiguration";
import {
  bindServiceAccountClient,
  type ServiceAccountClientSpec,
} from "@/lib/remote-identity/drafts/useServiceAccountClientDraft";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { CommitServerIdentityConfigurationResult } from "@gram/client/models/components/commitserveridentityconfigurationresult.js";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";

export type RemoteMcpCreationIdentity = "user" | "agent" | "none";

type ConfigureCreatedIdentityInput = {
  client: Gram;
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  identityMode: RemoteMcpCreationIdentity;
  agentAuthorization?: string;
  /**
   * An OAuth service account client to bind instead of a static credential.
   * Takes precedence over agentAuthorization.
   */
  serviceAccountClient?: ServiceAccountClientSpec;
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

export async function configureCreatedRemoteMcpIdentity({
  client,
  remoteMcpServer,
  mcpServer,
  identityMode,
  agentAuthorization,
  serviceAccountClient,
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

  if (identityMode === "agent" && serviceAccountClient) {
    try {
      await bindServiceAccountClient(
        client,
        serviceAccountClient,
        mcpServer.toolsetId ?? mcpServer.id,
        options,
      );
    } catch {
      return setupRequired(
        mcpServer,
        identityMode,
        `Couldn't connect the server to ${serviceAccountClient.issuerName}. Finish the service account in Settings > Sign-in.`,
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
        "Service account was configured, but the server could not be enabled. Enable it from Settings.",
      );
    }
  }

  if (identityMode === "agent") {
    const authorization = agentAuthorization?.trim();
    if (!authorization) {
      return setupRequired(
        mcpServer,
        identityMode,
        "Add a shared credential in Settings > Sign-in.",
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
        "The shared credential could not be configured. Add it in Settings > Sign-in.",
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
        "The shared credential was configured, but the server could not be enabled. Enable it from Settings.",
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

  const detected = await detectAuthorizationServer(
    client,
    remoteMcpServer.id,
    options,
  );
  if (detected.kind === "unavailable") {
    return setupRequired(
      mcpServer,
      identityMode,
      DETECTION_FAILURE_MESSAGE[detected.reason],
    );
  }
  const provider = detected.kind === "existing" ? detected.issuer : null;
  const draft = detected.kind === "new" ? detected.draft : null;

  // Capabilities come from whichever side actually knows them.
  const issuerAuthMethods =
    provider?.tokenEndpointAuthMethodsSupported ??
    draft?.tokenEndpointAuthMethodsSupported ??
    [];

  let result: CommitServerIdentityConfigurationResult;
  try {
    result = await client.remoteSessions.commitServerIdentityConfiguration(
      {
        commitServerIdentityConfigurationForm: {
          mcpServerId: mcpServer.id,
          providerId: provider?.id,
          createProvider:
            provider || !draft
              ? undefined
              : issuerFormFromDraft(draft, mcpServer.slug ?? "mcp"),
          clientMode: "auto",
          clientConfiguration: {
            // No copied scope: the server discovers what to request at
            // each sign-in, so nothing stored here can go stale.
            scope: undefined,
            tokenEndpointAuthMethod:
              serverIdentityAuthMethod(issuerAuthMethods),
          },
        },
      },
      undefined,
      options,
    );
  } catch (error) {
    return setupRequired(
      mcpServer,
      identityMode,
      isForbiddenError(error)
        ? "This server shares its sign-in client with other projects. An organization admin can finish setup in Settings > Sign-in."
        : "User identity could not be configured. Open Settings > Sign-in to finish setup.",
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

  // The provider landed but the upstream needs a human to finish registering
  // the client, so nobody can sign in yet. Enabling the server here would
  // advertise it as ready when the first connection is going to fail.
  if (result.manualSetupRequired) {
    return setupRequired(
      mcpServer,
      identityMode,
      "The authorization server needs its client registered by hand. Finish setup in Settings > Sign-in.",
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
      "User identity was configured, but the server could not be enabled. Enable it from Settings.",
      result,
    );
  }
}

const DETECTION_FAILURE_MESSAGE: Record<DetectionFailure, string> = {
  no_metadata:
    "OAuth metadata could not be discovered. Configure user identity in Settings > Sign-in.",
  lookup_failed:
    "Existing authorization servers could not be checked. Configure user identity in Settings > Sign-in.",
  existing_incomplete:
    "The matching authorization server is missing OAuth endpoints. Update it in Authorization Servers.",
  metadata_failed:
    "The authorization server metadata could not be discovered. Configure user identity in Settings > Sign-in.",
  issuer_mismatch:
    "The authorization server metadata identified a different issuer. Configure user identity in Settings > Sign-in.",
  missing_endpoints:
    "OAuth metadata is missing required endpoints. Configure user identity in Settings > Sign-in.",
};

function commitFailureMessage(
  result: CommitServerIdentityConfigurationResult,
): string {
  if (result.failure) {
    const outcome =
      result.failure.outcome === "unreachable"
        ? "The authorization server was unreachable"
        : "The authorization server refused registration";
    return `${outcome}. Configure user identity in Settings > Sign-in.`;
  }
  return "Automatic client registration is unavailable. Configure user identity in Settings > Sign-in.";
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
        visibility: requireKnownEnumValue(
          KnownUpdateMcpServerFormVisibility,
          visibility,
        ),
      },
    },
    undefined,
    options,
  );
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
