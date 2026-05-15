import type { Gram } from "@gram/client";
import type {
  ExternalMCPToolDefinition,
  RemoteSessionIssuerDraft,
  Tool,
} from "@gram/client/models/components";

import { getServerURL } from "@/lib/utils";

export const ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG =
  "onboard-external-mcp-to-user-sessions";

export const DEFAULT_USER_SESSION_DURATION_HOURS = 24 * 14;

const MAX_SLUG_LENGTH = 40;

type RequestOptions = Parameters<Gram["toolsets"]["setUserSessionIssuer"]>[2];

type ToolsetWithExternalMcpTools = {
  slug: string;
  name: string;
  tools?: Tool[];
  rawTools?: Tool[];
};

export type ExternalMcpUserSessionOAuthConfig = {
  slug: string;
  name: string;
  issuerUrl: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  scopesSupported: string[];
  tokenEndpointAuthMethodsSupported: string[];
};

export type OnboardExternalMcpToUserSessionsResult = {
  userSessionIssuerId: string;
  remoteSessionIssuerId: string;
};

export function remoteLoginCallbackURL(): string {
  return `${getServerURL()}/mcp/remote_login_callback`;
}

export function buildUserSessionResourceSlug(baseSlug: string): string {
  const suffix = randomSlugSuffix();
  const normalizedBase = slugify(baseSlug) || "mcp";
  const maxBaseLength = MAX_SLUG_LENGTH - suffix.length - 1;
  const trimmedBase =
    normalizedBase.slice(0, maxBaseLength).replace(/-+$/g, "") || "mcp";
  return `${trimmedBase}-${suffix}`;
}

export function resolveExternalMcpUserSessionOAuthConfig(
  toolset: ToolsetWithExternalMcpTools,
): ExternalMcpUserSessionOAuthConfig | null {
  const tools = toolset.rawTools ?? toolset.tools ?? [];
  for (const tool of tools) {
    const def = tool.externalMcpToolDefinition;
    if (!def?.requiresOauth) continue;
    const config = configFromExternalMcpDefinition(def);
    if (config) return config;
  }
  return null;
}

export function externalMcpUserSessionOAuthConfigFromMetadata({
  slug,
  name,
  metadata,
}: {
  slug: string;
  name: string;
  metadata: Record<string, unknown>;
}): ExternalMcpUserSessionOAuthConfig | null {
  const authorizationEndpoint = readString(metadata.authorization_endpoint);
  const tokenEndpoint = readString(metadata.token_endpoint);
  const registrationEndpoint = readString(metadata.registration_endpoint);
  if (!authorizationEndpoint || !tokenEndpoint || !registrationEndpoint) {
    return null;
  }

  const issuerUrl =
    extractOrigin(authorizationEndpoint) ??
    extractOrigin(tokenEndpoint) ??
    extractOrigin(registrationEndpoint);
  if (!issuerUrl) return null;

  return {
    slug,
    name,
    issuerUrl,
    authorizationEndpoint,
    tokenEndpoint,
    registrationEndpoint,
    scopesSupported: readStringArray(metadata.scopes_supported),
    tokenEndpointAuthMethodsSupported: readStringArray(
      metadata.token_endpoint_auth_methods_supported,
    ),
  };
}

export async function onboardExternalMcpToUserSessions({
  client,
  toolsetSlug,
  toolsetName,
  oauth,
  options,
  sessionDurationHours = DEFAULT_USER_SESSION_DURATION_HOURS,
}: {
  client: Gram;
  toolsetSlug: string;
  toolsetName: string;
  oauth: ExternalMcpUserSessionOAuthConfig;
  options?: RequestOptions;
  sessionDurationHours?: number;
}): Promise<OnboardExternalMcpToUserSessionsResult> {
  const resourceSlug = buildUserSessionResourceSlug(toolsetSlug || oauth.slug);

  const userSessionIssuer = await client.userSessionIssuers.create(
    {
      createUserSessionIssuerForm: {
        slug: resourceSlug,
        authnChallengeMode: "interactive",
        sessionDurationHours,
      },
    },
    undefined,
    options,
  );

  const draft = await discoverIssuerDraft(client, oauth.issuerUrl, options);
  const remoteSessionIssuer = await client.remoteSessionIssuers.create(
    {
      createRemoteSessionIssuerForm: {
        slug: resourceSlug,
        issuer: draft?.issuer ?? oauth.issuerUrl,
        authorizationEndpoint:
          draft?.authorizationEndpoint ?? oauth.authorizationEndpoint,
        tokenEndpoint: draft?.tokenEndpoint ?? oauth.tokenEndpoint,
        registrationEndpoint:
          draft?.registrationEndpoint ?? oauth.registrationEndpoint,
        jwksUri: draft?.jwksUri,
        scopesSupported: draft?.scopesSupported ?? oauth.scopesSupported,
        grantTypesSupported: draft?.grantTypesSupported ?? [
          "authorization_code",
          "refresh_token",
        ],
        responseTypesSupported: draft?.responseTypesSupported ?? ["code"],
        tokenEndpointAuthMethodsSupported:
          draft?.tokenEndpointAuthMethodsSupported ??
          oauth.tokenEndpointAuthMethodsSupported,
        oidc: draft?.oidc,
        passthrough: draft?.passthrough,
      },
    },
    undefined,
    options,
  );

  await client.remoteSessionIssuers.register(
    {
      registerRemoteSessionIssuerForm: {
        remoteSessionIssuerId: remoteSessionIssuer.id,
        userSessionIssuerId: userSessionIssuer.id,
        clientName: toolsetName || "Gram",
        redirectUris: [remoteLoginCallbackURL()],
      },
    },
    undefined,
    options,
  );

  await client.toolsets.setUserSessionIssuer(
    {
      slug: toolsetSlug,
      setUserSessionIssuerRequestBody: {
        userSessionIssuerId: userSessionIssuer.id,
      },
    },
    undefined,
    options,
  );

  return {
    userSessionIssuerId: userSessionIssuer.id,
    remoteSessionIssuerId: remoteSessionIssuer.id,
  };
}

function configFromExternalMcpDefinition(
  def: ExternalMCPToolDefinition,
): ExternalMcpUserSessionOAuthConfig | null {
  if (
    !def.oauthAuthorizationEndpoint ||
    !def.oauthTokenEndpoint ||
    !def.oauthRegistrationEndpoint
  ) {
    return null;
  }

  const issuerUrl =
    extractOrigin(def.oauthAuthorizationEndpoint) ??
    extractOrigin(def.oauthTokenEndpoint) ??
    extractOrigin(def.oauthRegistrationEndpoint);
  if (!issuerUrl) return null;

  return {
    slug: def.slug,
    name: def.registryServerName || def.name,
    issuerUrl,
    authorizationEndpoint: def.oauthAuthorizationEndpoint,
    tokenEndpoint: def.oauthTokenEndpoint,
    registrationEndpoint: def.oauthRegistrationEndpoint,
    scopesSupported: def.oauthScopesSupported ?? [],
    tokenEndpointAuthMethodsSupported: [],
  };
}

async function discoverIssuerDraft(
  client: Gram,
  issuer: string,
  options?: RequestOptions,
): Promise<RemoteSessionIssuerDraft | null> {
  try {
    return await client.remoteSessionIssuers.discover(
      {
        discoverRemoteSessionIssuerRequestBody: { issuer },
      },
      undefined,
      options,
    );
  } catch {
    return null;
  }
}

function readString(value: unknown): string | null {
  return typeof value === "string" && value.trim().length > 0 ? value : null;
}

function readStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string")
    : [];
}

function extractOrigin(url: string): string | null {
  try {
    const parsed = new URL(url);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return null;
  }
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function randomSlugSuffix(): string {
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
