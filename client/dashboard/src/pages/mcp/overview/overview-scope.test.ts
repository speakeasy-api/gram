import { describe, expect, it } from "vitest";
import { overviewScope } from "./overview-scope";

describe("overviewScope", () => {
  it("scopes remote MCP counters, charts and breakdowns by server ID only", () => {
    expect(
      overviewScope({
        kind: "mcp-server",
        id: "server-1",
        slug: "remote-server",
      }),
    ).toEqual({ mcpServerId: "server-1" });
  });
  it("preserves toolset slug scoping for toolset-backed servers", () => {
    expect(
      overviewScope({
        kind: "toolset",
        id: "toolset-1",
        slug: "example-tools",
      }),
    ).toEqual({ toolsetSlug: "example-tools" });
  });
});
