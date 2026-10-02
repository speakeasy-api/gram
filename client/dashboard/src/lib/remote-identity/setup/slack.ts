import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";

export const SLACK_SCOPE_CHOICES = [
  {
    label: "Search public channel messages",
    description: "Find messages in public channels you can access.",
    scopes: ["search:read.public"],
    defaultSelected: true,
  },
  {
    label: "Read public channels",
    description:
      "Read messages, channel details, and member lists in public channels.",
    scopes: ["channels:history", "channels:read"],
    defaultSelected: true,
  },
  {
    label: "Search private channel messages",
    description: "Find messages in private channels you belong to.",
    scopes: ["search:read.private"],
    defaultSelected: false,
  },
  {
    label: "Read private channels",
    description:
      "Read messages, channel details, and member lists in private channels you belong to.",
    scopes: ["groups:history", "groups:read"],
    defaultSelected: false,
  },
  {
    label: "Search direct messages",
    description:
      "Find messages in direct and group conversations you belong to.",
    scopes: ["search:read.im", "search:read.mpim"],
    defaultSelected: false,
  },
  {
    label: "Read direct messages",
    description:
      "Read messages and conversation details in direct and group conversations you belong to.",
    scopes: ["im:history", "im:read", "mpim:history", "mpim:read"],
    defaultSelected: false,
  },
] as const;

export const SLACK_READ_SCOPES = SLACK_SCOPE_CHOICES.flatMap((choice) => [
  ...choice.scopes,
]);

export const SLACK_DEFAULT_SCOPES = SLACK_SCOPE_CHOICES.filter(
  (choice) => choice.defaultSelected,
).flatMap((choice) => [...choice.scopes]);

export function slackAppConfiguration(
  callback: string | undefined,
  scopes: readonly string[] = SLACK_DEFAULT_SCOPES,
): { json: string; creationUrl: string } | null {
  if (!callback || !hasSlackReadScopes(scopes)) return null;
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
        scopes: { user: [...scopes] },
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
  if (!scopes?.length) return false;
  const unique = new Set(scopes);
  return (
    [...unique].every((scope) =>
      SLACK_READ_SCOPES.some((allowed) => allowed === scope),
    ) &&
    SLACK_SCOPE_CHOICES.every(
      (choice) =>
        choice.scopes.every((scope) => unique.has(scope)) ||
        choice.scopes.every((scope) => !unique.has(scope)),
    )
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
    return "Scopes must match the supported read/search access choices";
  if (client.tokenEndpointAuthMethod !== "client_secret_post")
    return "Requires client_secret_post authentication";
  if (client.legacyCallbackUrl !== false)
    return "Canonical callback mode is required";
  return null;
}
