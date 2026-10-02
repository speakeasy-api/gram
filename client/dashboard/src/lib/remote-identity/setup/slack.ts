import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";

export const SLACK_READ_SCOPES = [
  "channels:history",
  "channels:read",
  "groups:history",
  "groups:read",
  "im:history",
  "im:read",
  "mpim:history",
  "mpim:read",
  "search:read.im",
  "search:read.mpim",
  "search:read.private",
  "search:read.public",
] as const;

export function slackAppConfiguration(
  callback: string | undefined,
): { json: string; creationUrl: string } | null {
  if (!callback) return null;
  try {
    const url = new URL(callback);
    if (
      url.protocol !== "https:" ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/mcp/remote_login_callback"
    )
      return null;
  } catch {
    return null;
  }
  const json = JSON.stringify(
    {
      display_information: { name: "Speakeasy Slack MCP" },
      settings: { is_mcp_enabled: true },
      oauth_config: {
        redirect_urls: [callback],
        scopes: { user: [...SLACK_READ_SCOPES] },
      },
    },
    null,
    2,
  );
  const url = new URL("https://api.slack.com/apps");
  url.searchParams.set("new_app", "1");
  url.searchParams.set("manifest_json", json);
  return { json, creationUrl: url.toString() };
}

export function isSlackMcpUrl(value: string | undefined): boolean {
  if (!value) return false;
  try {
    const url = new URL(value);
    return (
      url.origin === "https://mcp.slack.com" &&
      url.pathname === "/mcp" &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  } catch {
    return false;
  }
}

export function isSlackProvider(
  provider:
    | {
        issuer?: string;
        authorizationEndpoint?: string | null;
        tokenEndpoint?: string | null;
        tokenEndpointAuthMethodsSupported?: string[] | null;
      }
    | null
    | undefined,
): boolean {
  return (
    !!provider &&
    provider.issuer === "https://mcp.slack.com" &&
    provider.authorizationEndpoint ===
      "https://slack.com/oauth/v2_user/authorize" &&
    provider.tokenEndpoint === "https://slack.com/api/oauth.v2.user.access" &&
    !!provider.tokenEndpointAuthMethodsSupported?.includes("client_secret_post")
  );
}

export function hasSlackReadScopes(
  scopes: readonly string[] | undefined,
): boolean {
  if (!scopes) return false;
  const unique = new Set(scopes);
  return (
    unique.size === SLACK_READ_SCOPES.length &&
    SLACK_READ_SCOPES.every((scope) => unique.has(scope))
  );
}

export function slackClientMismatch(
  client: Pick<
    RemoteSessionClient,
    | "remoteSessionIssuerId"
    | "scope"
    | "tokenEndpointAuthMethod"
    | "legacyCallbackUrl"
  >,
  providerId: string,
): string | null {
  if (client.remoteSessionIssuerId !== providerId)
    return "Different identity provider";
  if (!hasSlackReadScopes(client.scope))
    return "Scopes must match the read/search permissions exactly";
  if (client.tokenEndpointAuthMethod !== "client_secret_post")
    return "Requires client_secret_post authentication";
  if (client.legacyCallbackUrl !== false)
    return "Canonical callback mode is required";
  return null;
}
