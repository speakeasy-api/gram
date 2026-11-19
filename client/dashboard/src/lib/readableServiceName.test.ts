// @vitest-environment node
import { describe, expect, it } from "vitest";
import { readableServiceNameFromUrl } from "./sources";

describe("readableServiceNameFromUrl", () => {
  it.each([
    ["https://mcp.canva.com/mcp", "Canva"],
    ["https://mcp.canva.com./mcp", "Canva"],
    ["https://mcp.linear.app/mcp", "Linear"],
    ["https://app.asana.com", "Asana"],
    ["https://access.stripe.com/mcp", "Stripe"],
    ["https://mcp.acme-wiki.example/mcp", "Acme Wiki"],
    ["https://slick.chat/mcp", "Slick"],
    ["https://api.example.co.uk/mcp", "Example"],
    ["https://MCP.Ramp.COM/mcp", "Ramp"],
  ])("reads %s as %s", (url, name) => {
    expect(readableServiceNameFromUrl(url)).toBe(name);
  });

  it.each([
    "http://127.0.0.1:4100/mcp",
    "http://[::1]:4100/mcp",
    "https://localhost:8080/rpc/mcp",
    "https://foo.vercel.app/mcp",
    "https://svc-x.a.run.app/mcp",
    "https://acme.github.io/mcp",
    // URLs read internationalised names as punycode, which reads badly.
    "https://mcp.über.example/mcp",
  ])("gives no name for %s", (url) => {
    expect(readableServiceNameFromUrl(url)).toBeNull();
  });

  it("returns null for something that is not a URL", () => {
    expect(readableServiceNameFromUrl("not a url")).toBeNull();
  });
});
