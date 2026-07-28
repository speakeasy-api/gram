import { describe, expect, it } from "vitest";

import type { Toolset } from "@/lib/toolTypes";
import {
  externalOauthIssuerUrl,
  getOAuthParadigm,
  isUserSessionIssuerWired,
  mustConvertOAuthBeforePrivate,
  toolsetAuthSurface,
  toolsetConvertAction,
} from "./toolsetAuthSurface";

describe("toolsetAuthSurface", () => {
  it("shows the unchanged legacy UI when the flag is off, regardless of state", () => {
    expect(
      toolsetAuthSurface({
        flagEnabled: false,
        userSessionIssuerWired: true,
        oauthParadigm: "proxy",
      }),
    ).toBe("legacy-only");
    expect(
      toolsetAuthSurface({
        flagEnabled: false,
        userSessionIssuerWired: false,
        oauthParadigm: null,
      }),
    ).toBe("legacy-only");
  });

  it("shows the manage surface once a user_session_issuer is wired", () => {
    expect(
      toolsetAuthSurface({
        flagEnabled: true,
        userSessionIssuerWired: true,
        oauthParadigm: null,
      }),
    ).toBe("manage");
  });

  it("prefers the manage surface over leftover legacy config", () => {
    // Wire-migrated toolsets keep their inert oauth_proxy_server rows; the
    // wired issuer is what gates the serve path, so it wins the tiebreak.
    expect(
      toolsetAuthSurface({
        flagEnabled: true,
        userSessionIssuerWired: true,
        oauthParadigm: "proxy",
      }),
    ).toBe("manage");
    expect(
      toolsetAuthSurface({
        flagEnabled: true,
        userSessionIssuerWired: true,
        oauthParadigm: "external",
      }),
    ).toBe("manage");
  });

  it("keeps the legacy surface while a legacy paradigm is configured unwired", () => {
    for (const oauthParadigm of ["external", "gram", "proxy"] as const) {
      expect(
        toolsetAuthSurface({
          flagEnabled: true,
          userSessionIssuerWired: false,
          oauthParadigm,
        }),
      ).toBe("legacy");
    }
  });

  it("shows the attach surface when nothing is configured", () => {
    expect(
      toolsetAuthSurface({
        flagEnabled: true,
        userSessionIssuerWired: false,
        oauthParadigm: null,
      }),
    ).toBe("attach");
  });
});

describe("toolsetConvertAction", () => {
  it("routes proxy paradigms through the wire modal", () => {
    expect(toolsetConvertAction("proxy")).toBe("wire-modal");
    expect(toolsetConvertAction("gram")).toBe("wire-modal");
  });

  it("routes external OAuth through the attach sheet", () => {
    expect(toolsetConvertAction("external")).toBe("attach-sheet");
  });

  it("offers no convert path without a legacy paradigm", () => {
    expect(toolsetConvertAction(null)).toBeNull();
  });
});

describe("mustConvertOAuthBeforePrivate", () => {
  it("blocks going private while legacy OAuth is configured unwired", () => {
    for (const oauthParadigm of ["external", "gram", "proxy"] as const) {
      expect(
        mustConvertOAuthBeforePrivate({
          flagEnabled: true,
          mcpIsPublic: true,
          userSessionIssuerWired: false,
          oauthParadigm,
        }),
      ).toBe(true);
    }
  });

  it("keeps today's silent clear when the flag is off (no convert path)", () => {
    expect(
      mustConvertOAuthBeforePrivate({
        flagEnabled: false,
        mcpIsPublic: true,
        userSessionIssuerWired: false,
        oauthParadigm: "external",
      }),
    ).toBe(false);
  });

  it("allows the flip once a user session issuer is wired (leftover config is inert)", () => {
    expect(
      mustConvertOAuthBeforePrivate({
        flagEnabled: true,
        mcpIsPublic: true,
        userSessionIssuerWired: true,
        oauthParadigm: "external",
      }),
    ).toBe(false);
  });

  it("does not block without OAuth config or when already private", () => {
    expect(
      mustConvertOAuthBeforePrivate({
        flagEnabled: true,
        mcpIsPublic: true,
        userSessionIssuerWired: false,
        oauthParadigm: null,
      }),
    ).toBe(false);
    expect(
      mustConvertOAuthBeforePrivate({
        flagEnabled: true,
        mcpIsPublic: false,
        userSessionIssuerWired: false,
        oauthParadigm: "proxy",
      }),
    ).toBe(false);
  });
});

describe("isUserSessionIssuerWired", () => {
  it("treats either issuer field as wired", () => {
    expect(
      isUserSessionIssuerWired({
        userSessionIssuerId: "usi_123",
      } as unknown as Toolset),
    ).toBe(true);
    expect(
      isUserSessionIssuerWired({
        userSessionIssuerSlug: "my-issuer",
      } as unknown as Toolset),
    ).toBe(true);
    expect(
      isUserSessionIssuerWired({
        userSessionIssuerId: "usi_123",
        userSessionIssuerSlug: "my-issuer",
      } as unknown as Toolset),
    ).toBe(true);
  });

  it("is unwired when both fields are absent", () => {
    expect(isUserSessionIssuerWired({} as Toolset)).toBe(false);
  });
});

describe("getOAuthParadigm", () => {
  it("prefers external OAuth over a proxy server", () => {
    const toolset = {
      externalOauthServer: { id: "ext" },
      oauthProxyServer: { oauthProxyProviders: [{ providerType: "custom" }] },
    } as unknown as Toolset;
    expect(getOAuthParadigm(toolset)).toBe("external");
  });

  it("distinguishes gram-managed from custom proxy providers", () => {
    const gram = {
      oauthProxyServer: { oauthProxyProviders: [{ providerType: "gram" }] },
    } as unknown as Toolset;
    const custom = {
      oauthProxyServer: { oauthProxyProviders: [{ providerType: "custom" }] },
    } as unknown as Toolset;
    expect(getOAuthParadigm(gram)).toBe("gram");
    expect(getOAuthParadigm(custom)).toBe("proxy");
  });

  it("returns null when no legacy OAuth is configured", () => {
    expect(getOAuthParadigm({} as Toolset)).toBeNull();
  });
});

describe("externalOauthIssuerUrl", () => {
  it("reads the RFC 8414 issuer claim from the stored metadata", () => {
    const toolset = {
      externalOauthServer: {
        metadata: { issuer: "https://auth.example.com" },
      },
    } as unknown as Toolset;
    expect(externalOauthIssuerUrl(toolset)).toBe("https://auth.example.com");
  });

  it("returns undefined for missing, blank, or non-string issuers", () => {
    expect(externalOauthIssuerUrl({} as Toolset)).toBeUndefined();
    expect(
      externalOauthIssuerUrl({
        externalOauthServer: { metadata: { issuer: "   " } },
      } as unknown as Toolset),
    ).toBeUndefined();
    expect(
      externalOauthIssuerUrl({
        externalOauthServer: { metadata: { issuer: 42 } },
      } as unknown as Toolset),
    ).toBeUndefined();
    expect(
      externalOauthIssuerUrl({
        externalOauthServer: { metadata: null },
      } as unknown as Toolset),
    ).toBeUndefined();
  });
});
