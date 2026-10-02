import { describe, expect, it } from "vitest";
import {
  hasSlackReadScopes,
  isSlackMcpUrl,
  isSlackProvider,
  slackClientMismatch,
  SLACK_READ_SCOPES,
  slackAppConfiguration,
} from "./slack";

describe("Slack setup compatibility", () => {
  it("generates only MCP enablement, user read scopes and the canonical callback", () => {
    const callback = "https://api.example.com/mcp/remote_login_callback";
    const config = slackAppConfiguration(callback)!;
    const manifest = JSON.parse(config.json);
    expect(manifest).toEqual({
      display_information: { name: "Speakeasy Slack MCP" },
      settings: { is_mcp_enabled: true },
      oauth_config: {
        redirect_urls: [callback],
        scopes: { user: [...SLACK_READ_SCOPES] },
      },
    });
    const link = new URL(config.creationUrl);
    expect(link.origin + link.pathname).toBe("https://api.slack.com/apps");
    expect(link.searchParams.get("new_app")).toBe("1");
    expect(link.searchParams.get("manifest_json")).toBe(config.json);
    expect(config.json).not.toMatch(/secret|bot|events|chat:write|is_public/);
  });
  it("refuses missing or noncanonical callbacks rather than deriving a browser origin", () => {
    for (const callback of [
      undefined,
      "",
      "invalid",
      "http://api.example.com/mcp/remote_login_callback",
      "https://user:secret@api.example.com/mcp/remote_login_callback",
      "https://api.example.com/other",
      "https://api.example.com/mcp/remote_login_callback?token=secret",
    ]) {
      expect(slackAppConfiguration(callback)).toBeNull();
    }
  });
  it("only accepts the reviewed parsed endpoint", () => {
    expect(isSlackMcpUrl("https://mcp.slack.com/mcp")).toBe(true);
    for (const url of [
      "http://mcp.slack.com/mcp",
      "https://mcp.slack.com.evil.example/mcp",
      "https://mcp.slack.com/mcp/",
      "https://mcp.slack.com/mcp?q=1",
      "https://user@mcp.slack.com/mcp",
      "https://mcp.slack.com:8443/mcp",
      "invalid",
    ])
      expect(isSlackMcpUrl(url)).toBe(false);
  });
  it("requires exact normalized read/search scopes, never writes or unknown scopes", () => {
    expect(hasSlackReadScopes([...SLACK_READ_SCOPES].reverse())).toBe(true);
    expect(
      hasSlackReadScopes([...SLACK_READ_SCOPES, SLACK_READ_SCOPES[0]]),
    ).toBe(true);
    expect(hasSlackReadScopes([...SLACK_READ_SCOPES, "chat:write"])).toBe(
      false,
    );
    expect(hasSlackReadScopes(SLACK_READ_SCOPES.slice(1))).toBe(false);
    expect(hasSlackReadScopes(undefined)).toBe(false);
  });
  it("checks provider, auth method and callback without modifying stored clients", () => {
    const client = {
      remoteSessionIssuerId: "provider",
      scope: [...SLACK_READ_SCOPES],
      tokenEndpointAuthMethod: "client_secret_post" as const,
      legacyCallbackUrl: false,
    };
    expect(slackClientMismatch(client, "provider")).toBeNull();
    expect(slackClientMismatch(client, "another")).toMatch(/provider/);
    expect(
      slackClientMismatch({ ...client, legacyCallbackUrl: true }, "provider"),
    ).toMatch(/callback/);
    expect(
      slackClientMismatch(
        { ...client, tokenEndpointAuthMethod: "client_secret_basic" },
        "provider",
      ),
    ).toMatch(/authentication/);
    expect(client.legacyCallbackUrl).toBe(false);
    const provider = {
      issuer: "https://mcp.slack.com",
      authorizationEndpoint: "https://slack.com/oauth/v2_user/authorize",
      tokenEndpoint: "https://slack.com/api/oauth.v2.user.access",
      tokenEndpointAuthMethodsSupported: ["client_secret_post"],
    };
    expect(isSlackProvider(provider)).toBe(true);
    expect(
      isSlackProvider({
        ...provider,
        tokenEndpoint: "https://slack.com/api/oauth.v2.access",
      }),
    ).toBe(false);
    expect(isSlackProvider({ ...provider, issuer: "https://slack.com" })).toBe(
      false,
    );
  });
});
