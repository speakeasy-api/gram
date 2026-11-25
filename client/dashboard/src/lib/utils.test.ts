import { describe, expect, it } from "vitest";
import {
  agentEgressHosts,
  buildLoginRedirectURL,
  chunk,
  firstPartyHosts,
  getCustomDomainCNAME,
  supportsFirstPartyConnect,
  tunnelGatewayURL,
} from "./utils";
import type { RuntimeConfig } from "./runtime-config";

describe("supportsFirstPartyConnect", () => {
  const eligible = {
    userSessionIssuerId: "issuer-1",
    platformSlug: "server",
    visibility: "private",
  };

  it("allows an issuer-gated server with a Speakeasy-hosted slug", () => {
    expect(supportsFirstPartyConnect(eligible)).toBe(true);
  });

  it("rejects a server with no identity provider", () => {
    expect(
      supportsFirstPartyConnect({
        ...eligible,
        userSessionIssuerId: undefined,
      }),
    ).toBe(false);
  });

  it("rejects a public tunnel even when an issuer is set", () => {
    expect(
      supportsFirstPartyConnect({
        ...eligible,
        tunneledMcpServerId: "tunnel-1",
        visibility: "public",
      }),
    ).toBe(false);
  });

  it("allows a private tunnel", () => {
    expect(
      supportsFirstPartyConnect({
        ...eligible,
        tunneledMcpServerId: "tunnel-1",
        visibility: "private",
      }),
    ).toBe(true);
  });

  it("allows a public server that is not tunneled", () => {
    expect(
      supportsFirstPartyConnect({
        ...eligible,
        visibility: "public",
      }),
    ).toBe(true);
  });

  it("rejects a custom-domain-only server with no platform slug", () => {
    expect(
      supportsFirstPartyConnect({ ...eligible, platformSlug: undefined }),
    ).toBe(false);
  });
});

describe("chunk", () => {
  it("splits into fixed batches and keeps order", () => {
    expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
    expect(chunk([], 2)).toEqual([]);
  });
});

// Mirrors the configuration the gram-infra chart renders for each deployment.
const prod: RuntimeConfig = {
  tunnelHost: "tunnel.speakeasy.com",
  customDomainCname: "cname.getgram.ai.",
  firstPartyHosts: ["app.getgram.ai", "ai.speakeasy.com"],
};
const dev: RuntimeConfig = {
  tunnelHost: "tunnel.dev.getgram.ai",
  customDomainCname: "cname.dev.getgram.ai.",
  firstPartyHosts: ["dev.getgram.ai", "dev.ai.speakeasy.com"],
};
const preview: RuntimeConfig = {
  tunnelHost: "tunnel-pr-6012.dev.getgram.ai",
  customDomainCname: "cname.dev.getgram.ai.",
};
const local: RuntimeConfig = {};

describe("tunnelGatewayURL", () => {
  it.each([
    ["https://app.getgram.ai", prod, "wss://tunnel.speakeasy.com/connect"],
    ["https://ai.speakeasy.com", prod, "wss://tunnel.speakeasy.com/connect"],
    ["https://dev.getgram.ai", dev, "wss://tunnel.dev.getgram.ai/connect"],
    [
      "https://dev.ai.speakeasy.com",
      dev,
      "wss://tunnel.dev.getgram.ai/connect",
    ],
    [
      "https://pr-6012.dev.getgram.ai",
      preview,
      "wss://tunnel-pr-6012.dev.getgram.ai/connect",
    ],
    ["http://localhost:8080", local, "ws://tunnel.localhost:8080/connect"],
  ])(
    "maps %s to the configured or derived host",
    (serverURL, config, expected) => {
      expect(tunnelGatewayURL(serverURL, config)).toBe(expected);
    },
  );
});

describe("getCustomDomainCNAME", () => {
  it.each([
    ["https://app.getgram.ai", prod, "cname.getgram.ai."],
    ["https://ai.speakeasy.com", prod, "cname.getgram.ai."],
    ["https://dev.getgram.ai", dev, "cname.dev.getgram.ai."],
    ["https://dev.ai.speakeasy.com", dev, "cname.dev.getgram.ai."],
    ["https://app.dev.getgram.ai", local, "cname.dev.getgram.ai."],
    ["https://staging.example.com", local, "cname.staging.example.com."],
    ["not a url", local, ""],
  ])(
    "maps %s to the configured or derived target",
    (serverURL, config, expected) => {
      expect(getCustomDomainCNAME(serverURL, config)).toBe(expected);
    },
  );
});

describe("agentEgressHosts", () => {
  it.each([
    ["https://app.getgram.ai", prod, ["app.getgram.ai", "ai.speakeasy.com"]],
    ["https://ai.speakeasy.com", prod, ["ai.speakeasy.com", "app.getgram.ai"]],
    ["https://dev.getgram.ai", dev, ["dev.getgram.ai", "dev.ai.speakeasy.com"]],
    [
      "https://dev.ai.speakeasy.com",
      dev,
      ["dev.ai.speakeasy.com", "dev.getgram.ai"],
    ],
    ["https://pr-12.dev.getgram.ai", preview, ["pr-12.dev.getgram.ai"]],
    ["https://localhost:8080", local, ["localhost"]],
  ])("allows %s egress to %j", (serverURL, config, expected) => {
    expect(agentEgressHosts(serverURL, config)).toEqual(expected);
  });
});

describe("firstPartyHosts", () => {
  it.each([
    ["https://app.getgram.ai", prod, ["app.getgram.ai", "ai.speakeasy.com"]],
    ["https://ai.speakeasy.com", prod, ["app.getgram.ai", "ai.speakeasy.com"]],
    ["https://dev.getgram.ai", dev, ["dev.getgram.ai", "dev.ai.speakeasy.com"]],
    ["https://pr-12.dev.getgram.ai", preview, ["pr-12.dev.getgram.ai"]],
    // A host the deployment routes but does not list keeps only itself.
    ["https://api.getgram.ai", prod, ["api.getgram.ai"]],
  ])("lists %s as %j", (serverURL, config, expected) => {
    expect(firstPartyHosts(serverURL, config)).toEqual(expected);
  });
});

describe("partner signup redirect", () => {
  it("encodes the public code alongside signup details", () => {
    const url = new URL(
      buildLoginRedirectURL(
        "/welcome",
        "Acme",
        "user@example.com",
        "partner & friends",
      ),
    );
    expect(url.searchParams.get("partner_code")).toBe("partner & friends");
    expect(url.searchParams.get("org_name")).toBe("Acme");
    expect(url.searchParams.get("redirect")).toBe("/welcome");
  });

  it("adds sign-up attribution params", () => {
    const url = new URL(
      buildLoginRedirectURL("/welcome", "Acme", undefined, undefined, {
        utm_source: "linkedin",
        analytics_session_id: "sess-123",
      }),
    );
    expect(url.searchParams.get("utm_source")).toBe("linkedin");
    expect(url.searchParams.get("analytics_session_id")).toBe("sess-123");
  });
});
