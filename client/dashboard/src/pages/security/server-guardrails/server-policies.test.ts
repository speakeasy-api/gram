import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { describe, expect, it } from "vitest";
import { policiesForMcpServer, scopedToolsLabel } from "./server-policies";

function policy(id: string, mcpScope: RiskPolicy["mcpScope"]): RiskPolicy {
  return { id, mcpScope } as RiskPolicy;
}

describe("policiesForMcpServer", () => {
  const orgWide = policy("org", undefined);
  const everyServer = policy("every", { allServers: true, servers: [] });
  const here = policy("here", { servers: [{ mcpServerId: "srv" }] });
  const elsewhere = policy("else", { servers: [{ mcpServerId: "other" }] });
  const override = policy("override", {
    allServers: true,
    servers: [{ mcpServerId: "srv", tools: ["a"] }],
  });

  it("separates named policies from inherited ones and drops the rest", () => {
    const result = policiesForMcpServer(
      [orgWide, everyServer, here, elsewhere, override],
      "srv",
    );
    expect(result.scoped.map((p) => p.id)).toEqual(["here", "override"]);
    expect(result.inherited.map((p) => p.id)).toEqual(["org", "every"]);
  });
});

describe("scopedToolsLabel", () => {
  it("reads all tools, a wildcard, or a count", () => {
    expect(
      scopedToolsLabel(policy("a", { servers: [{ mcpServerId: "s" }] }), "s"),
    ).toBe("All tools");
    expect(
      scopedToolsLabel(
        policy("a", { servers: [{ mcpServerId: "s", tools: ["*"] }] }),
        "s",
      ),
    ).toBe("All tools");
    expect(
      scopedToolsLabel(
        policy("a", { servers: [{ mcpServerId: "s", tools: ["x"] }] }),
        "s",
      ),
    ).toBe("1 tool");
    expect(
      scopedToolsLabel(
        policy("a", { servers: [{ mcpServerId: "s", tools: ["x", "y"] }] }),
        "s",
      ),
    ).toBe("2 tools");
  });
});
