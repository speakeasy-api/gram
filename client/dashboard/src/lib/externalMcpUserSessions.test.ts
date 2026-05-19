import { describe, expect, it } from "vitest";

import {
  externalMcpUserSessionOAuthConfigFromMetadata,
  resolveExternalMcpUserSessionOAuthConfig,
} from "./externalMcpUserSessions";

describe("external MCP user-session OAuth config", () => {
  it("prefers an explicit issuer when it matches the upstream endpoints", () => {
    const config = externalMcpUserSessionOAuthConfigFromMetadata({
      slug: "okta",
      name: "Okta",
      metadata: {
        issuer: "https://idp.example/oauth2/default",
        authorization_endpoint:
          "https://idp.example/oauth2/default/v1/authorize",
        token_endpoint: "https://idp.example/oauth2/default/v1/token",
        registration_endpoint: "https://idp.example/oauth2/default/v1/register",
      },
    });

    expect(config?.issuerUrl).toBe("https://idp.example/oauth2/default");
  });

  it("ignores unrelated synthetic issuers and preserves the upstream path", () => {
    const config = externalMcpUserSessionOAuthConfigFromMetadata({
      slug: "okta",
      name: "Okta",
      metadata: {
        issuer: "https://app.getgram.ai/mcp/okta",
        authorization_endpoint:
          "https://idp.example/oauth2/default/v1/authorize",
        token_endpoint: "https://idp.example/oauth2/default/v1/token",
        registration_endpoint: "https://idp.example/oauth2/default/v1/register",
      },
    });

    expect(config?.issuerUrl).toBe("https://idp.example/oauth2/default");
  });

  it("preserves issuer path when extracting OAuth config from tool metadata", () => {
    const toolset = {
      slug: "okta-tools",
      name: "Okta Tools",
      rawTools: [
        {
          externalMcpToolDefinition: {
            requiresOauth: true,
            slug: "okta",
            name: "proxy",
            registryServerName: "Okta",
            oauthAuthorizationEndpoint:
              "https://idp.example/oauth2/default/v1/authorize",
            oauthTokenEndpoint: "https://idp.example/oauth2/default/v1/token",
            oauthRegistrationEndpoint:
              "https://idp.example/oauth2/default/v1/register",
          },
        },
      ],
    } as Parameters<typeof resolveExternalMcpUserSessionOAuthConfig>[0];

    const config = resolveExternalMcpUserSessionOAuthConfig(toolset);

    expect(config?.issuerUrl).toBe("https://idp.example/oauth2/default");
  });
});
