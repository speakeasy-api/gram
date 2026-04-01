import { describe, expect, it } from "vitest";
import { getToolsetOAuthMode, type OAuthMode } from "./playgroundOAuthMode";

// Minimal toolset shapes for testing — only the fields getToolsetOAuthMode inspects
function makeToolset(
  overrides: {
    oauthProxyServer?: {
      oauthProxyProviders?: Array<{
        providerType: "custom" | "gram";
        slug: string;
      }>;
    };
    externalOauthServer?: { slug: string };
  } = {},
) {
  return overrides;
}

describe("getToolsetOAuthMode", () => {
  it('returns "none" when toolset has no OAuth config', () => {
    expect(getToolsetOAuthMode(makeToolset())).toBe("none" satisfies OAuthMode);
  });

  it('returns "none" for gram proxy provider (transparent auth)', () => {
    const toolset = makeToolset({
      oauthProxyServer: {
        oauthProxyProviders: [{ providerType: "gram", slug: "gram-provider" }],
      },
    });
    expect(getToolsetOAuthMode(toolset)).toBe("none" satisfies OAuthMode);
  });

  it('returns "custom-proxy" for custom proxy provider', () => {
    const toolset = makeToolset({
      oauthProxyServer: {
        oauthProxyProviders: [{ providerType: "custom", slug: "google-oauth" }],
      },
    });
    expect(getToolsetOAuthMode(toolset)).toBe(
      "custom-proxy" satisfies OAuthMode,
    );
  });

  it('returns "external" for external OAuth server', () => {
    const toolset = makeToolset({
      externalOauthServer: { slug: "my-oauth-server" },
    });
    expect(getToolsetOAuthMode(toolset)).toBe("external" satisfies OAuthMode);
  });

  it("prefers proxy server over external OAuth server when both are set", () => {
    const toolset = makeToolset({
      oauthProxyServer: {
        oauthProxyProviders: [{ providerType: "custom", slug: "google-oauth" }],
      },
      externalOauthServer: { slug: "my-oauth-server" },
    });
    expect(getToolsetOAuthMode(toolset)).toBe(
      "custom-proxy" satisfies OAuthMode,
    );
  });

  it('returns "none" when proxy server exists but has no providers', () => {
    const toolset = makeToolset({
      oauthProxyServer: { oauthProxyProviders: [] },
    });
    expect(getToolsetOAuthMode(toolset)).toBe("none" satisfies OAuthMode);
  });

  it('returns "none" when proxy server exists but providers array is undefined', () => {
    const toolset = makeToolset({
      oauthProxyServer: {},
    });
    expect(getToolsetOAuthMode(toolset)).toBe("none" satisfies OAuthMode);
  });
});
