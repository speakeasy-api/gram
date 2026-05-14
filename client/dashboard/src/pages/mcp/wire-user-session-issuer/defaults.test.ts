import type { Toolset } from "@/lib/toolTypes";
import { describe, expect, it } from "vitest";

import { canCloneProvider, deriveMigrationDefaults } from "./defaults";

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

  it("derives slugs from the toolset slug", () => {
    const d = deriveMigrationDefaults(mkToolset({ slug: "github" }));
    expect(d?.userSessionIssuerSlug).toBe("github-usi");
    expect(d?.remoteSessionIssuerSlug).toBe("github-rsi");
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
});

describe("canCloneProvider", () => {
  it("permits custom providers", () => {
    const t = mkToolset({ providerType: "custom" });
    const provider = t.oauthProxyServer!.oauthProxyProviders![0]!;
    expect(canCloneProvider(provider)).toBe(true);
  });

  it("refuses gram-managed providers", () => {
    const t = mkToolset({ providerType: "gram" });
    const provider = t.oauthProxyServer!.oauthProxyProviders![0]!;
    expect(canCloneProvider(provider)).toBe(false);
  });
});
