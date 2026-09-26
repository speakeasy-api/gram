import { describe, expect, it } from "vitest";
import {
  chunk,
  getCustomDomainCNAME,
  supportsFirstPartyConnect,
  tunnelGatewayURL,
} from "./utils";

describe("supportsFirstPartyConnect", () => {
  const eligible = {
    userSessionIssuerId: "issuer-1",
    platformSlug: "server",
    visibility: "private",
  };

  it("allows an issuer-gated server with a Gram-hosted slug", () => {
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

describe("tunnelGatewayURL", () => {
  it.each([
    ["https://app.getgram.ai", "wss://tunnel.speakeasy.com/connect"],
    ["https://ai.speakeasy.com", "wss://tunnel.speakeasy.com/connect"],
    ["https://dev.getgram.ai", "wss://tunnel.dev.getgram.ai/connect"],
    [
      "https://dev.ai.speakeasy.com",
      "wss://tunnel.dev.ai.speakeasy.com/connect",
    ],
    [
      "https://pr-6012.dev.getgram.ai",
      "wss://tunnel-pr-6012.dev.getgram.ai/connect",
    ],
    ["http://localhost:8080", "ws://tunnel.localhost:8080/connect"],
  ])("maps %s to %s", (serverURL, expected) => {
    expect(tunnelGatewayURL(serverURL)).toBe(expected);
  });
});

describe("getCustomDomainCNAME", () => {
  it.each([
    ["https://app.getgram.ai", "cname.getgram.ai."],
    ["https://ai.speakeasy.com", "cname.getgram.ai."],
    ["https://dev.getgram.ai", "cname.dev.getgram.ai."],
    ["https://app.dev.getgram.ai", "cname.dev.getgram.ai."],
    ["not a url", "cname.getgram.ai."],
  ])("maps %s to %s", (serverURL, expected) => {
    expect(getCustomDomainCNAME(serverURL)).toBe(expected);
  });
});
