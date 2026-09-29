import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { describe, expect, it } from "vitest";
import {
  policiesForMcpServer,
  scopedToolsLabel,
  serverGuardrailPolicies,
} from "./server-policies";

function policy(
  id: string,
  mcpScope: RiskPolicy["mcpScope"],
  enabled = true,
): RiskPolicy {
  return { id, mcpScope, enabled } as RiskPolicy;
}

describe("policiesForMcpServer", () => {
  const everyServer = policy("every", { allServers: true, servers: [] });
  const here = policy("here", { servers: [{ mcpServerId: "srv" }] });
  const viaGateway = policy("gw", { servers: [{ mcpServerId: "gateway-1" }] });
  const override = policy("override", {
    allServers: true,
    servers: [{ mcpServerId: "srv", tools: ["a"] }],
  });

  it("keeps all-servers policies inherited and everything else scoped", () => {
    const result = policiesForMcpServer(
      [everyServer, here, viaGateway, override],
      "srv",
    );
    expect(result.scoped.map((p) => p.id)).toEqual(["here", "gw", "override"]);
    expect(result.inherited.map((p) => p.id)).toEqual(["every"]);
    expect(scopedToolsLabel(viaGateway, "srv")).toBe("Through a gateway");
  });
});

describe("serverGuardrailPolicies", () => {
  it("adds disabled policies naming this server or every server", () => {
    const applied = policy("applied", { servers: [{ mcpServerId: "srv" }] });
    const offHere = policy(
      "off-here",
      { servers: [{ mcpServerId: "srv" }] },
      false,
    );
    const offEvery = policy(
      "off-every",
      { allServers: true, servers: [] },
      false,
    );
    const offElsewhere = policy(
      "off-else",
      { servers: [{ mcpServerId: "other" }] },
      false,
    );
    const offUnscoped = policy("off-unscoped", undefined, false);

    const ids = serverGuardrailPolicies(
      [applied],
      [applied, offHere, offEvery, offElsewhere, offUnscoped],
      "srv",
    ).map((p) => p.id);
    expect(ids).toEqual(["applied", "off-here", "off-every"]);
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
        policy("a", { servers: [{ mcpServerId: "s", tools: [] }] }),
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
