import type { Toolset } from "@/lib/toolTypes";
import { describe, expect, it } from "vitest";

import { deriveMigrationDefaults } from "./defaults";

function mkToolset(overrides: {
  slug?: string;
  providerType?: "custom" | "gram";
  authorizationEndpoint?: string;
  noProvider?: boolean;
}): Toolset {
  const t: Partial<Toolset> = {
    slug: overrides.slug ?? "demo",
    oauthProxyServer: overrides.noProvider
      ? undefined
      : ({
          oauthProxyProviders: [
            {
              id: "00000000-0000-0000-0000-000000000001",
              slug: "p",
              providerType: overrides.providerType ?? "custom",
              authorizationEndpoint:
                overrides.authorizationEndpoint ??
                "https://idp.example.com/oauth/authorize",
              tokenEndpoint: "https://idp.example.com/oauth/token",
              createdAt: new Date(0),
              updatedAt: new Date(0),
            },
          ],
        } as Toolset["oauthProxyServer"]),
  };
  return t as Toolset;
}

describe("deriveMigrationDefaults", () => {
  it("returns null when the toolset has no oauth_proxy_provider", () => {
    expect(deriveMigrationDefaults(mkToolset({ noProvider: true }))).toBeNull();
  });

  it("uses the toolset slug as the base with a random suffix", () => {
    const d = deriveMigrationDefaults(mkToolset({ slug: "github" }));
    expect(d?.userSessionIssuerSlug).toMatch(/^github-[0-9a-f]{8}$/);
    expect(d?.remoteSessionIssuerSlug).toMatch(/^github-[0-9a-f]{8}$/);
  });

  it("uses one slug for both the USI and RSI so they read as a pair", () => {
    const d = deriveMigrationDefaults(mkToolset({ slug: "github" }));
    expect(d?.userSessionIssuerSlug).toBe(d?.remoteSessionIssuerSlug);
  });

  it("extracts the origin from the authorization_endpoint", () => {
    const d = deriveMigrationDefaults(
      mkToolset({
        authorizationEndpoint: "https://idp.example.com/oauth/authorize",
      }),
    );
    expect(d?.issuerOriginGuess).toBe("https://idp.example.com");
  });

  it("returns null origin when authorization_endpoint is malformed", () => {
    const d = deriveMigrationDefaults(
      mkToolset({ authorizationEndpoint: "not-a-url" }),
    );
    expect(d?.issuerOriginGuess).toBeNull();
  });

  it("preserves the proxy provider's providerType so the hook can branch on paradigm", () => {
    expect(
      deriveMigrationDefaults(mkToolset({ providerType: "gram" }))
        ?.proxyProvider.providerType,
    ).toBe("gram");
    expect(
      deriveMigrationDefaults(mkToolset({ providerType: "custom" }))
        ?.proxyProvider.providerType,
    ).toBe("custom");
  });
});
