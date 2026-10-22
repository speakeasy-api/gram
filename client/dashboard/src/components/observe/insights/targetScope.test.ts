import { describe, expect, it } from "vitest";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";
import { targetScope } from "./InsightsGrid";

function target(
  overrides: Partial<ToolUsageTargetSummary>,
): ToolUsageTargetSummary {
  return {
    targetType: "hosted_mcp_server",
    targetKind: "server",
    targetId: "payments",
    targetLabel: "Payments",
    eventCount: 1,
    uniqueTools: 1,
    successCount: 1,
    failureCount: 0,
    failureRate: 0,
    ...overrides,
  };
}

describe("targetScope", () => {
  it("links a hosted target by its server id", () => {
    const id = "0198a3b2-7c4d-7e5f-8a9b-0c1d2e3f4a5b";
    expect(targetScope(target({ mcpServerId: id }))).toEqual({
      target: { type: "hosted", id },
    });
  });

  it("links a hosted target without a server id by its slug", () => {
    expect(targetScope(target({}))).toEqual({
      target: { type: "hosted", id: "payments" },
    });
  });
});
