import { describe, expect, it } from "vitest";
import { chunk, getCustomDomainCNAME, tunnelGatewayURL } from "./utils";

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
    ["https://dev.ai.speakeasy.com", "wss://tunnel.dev.getgram.ai/connect"],
    [
      "https://pr-42.dev.getgram.ai",
      "wss://tunnel-pr-42.dev.getgram.ai/connect",
    ],
    ["https://fooai.speakeasy.com", "wss://tunnel.fooai.speakeasy.com/connect"],
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
    ["https://dev.ai.speakeasy.com", "cname.dev.getgram.ai."],
    ["https://pr-42.dev.getgram.ai", "cname.pr-42.dev.getgram.ai."],
    ["https://fooai.speakeasy.com", "cname.fooai.speakeasy.com."],
    ["http://localhost:8080", "localhost."],
    ["not a url", "cname.getgram.ai."],
  ])("maps %s to %s", (serverURL, expected) => {
    expect(getCustomDomainCNAME(serverURL)).toBe(expected);
  });
});
