// @vitest-environment node
import { describe, expect, it } from "vitest";
import { readableServiceNameFromUrl } from "./sources";

describe("readableServiceNameFromUrl", () => {
  it.each([
    ["https://mcp.canva.com/mcp", "Canva"],
    ["https://mcp.linear.app/mcp", "Linear"],
    ["https://app.asana.com", "Asana"],
    ["https://access.stripe.com/mcp", "Stripe"],
    ["https://mcp.acme-wiki.example/mcp", "Acme Wiki"],
    ["https://slick.chat/mcp", "Slick"],
    ["https://api.example.co.uk/mcp", "Example"],
    ["http://127.0.0.1:4100/mcp", "127.0.0.1"],
    ["https://localhost:8080/rpc/mcp", "localhost"],
  ])("reads %s as %s", (url, name) => {
    expect(readableServiceNameFromUrl(url)).toBe(name);
  });

  it("returns null for something that is not a URL", () => {
    expect(readableServiceNameFromUrl("not a url")).toBeNull();
  });
});
