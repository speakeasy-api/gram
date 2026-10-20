import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  chatFindingDetail,
  findingKind,
  findingMessageKind,
} from "./finding-kind";

function finding(overrides: Partial<RiskResult> = {}): RiskResult {
  return {
    id: "f",
    policyId: "p",
    policyVersion: 1,
    createdAt: new Date(0),
    source: "gitleaks",
    ...overrides,
  };
}

describe("findingKind", () => {
  it.each([
    ["mcp", finding({ mediationSurface: "hosted_mcp", toolName: "search" })],
    ["mcp", finding({ mcpServerId: "s1" })],
    ["chat", finding({ chatId: "c1" })],
    ["chat", finding({ source: "presidio", chatId: "c1" })],
    ["judge", finding({ source: "llm_judge", chatId: "c1" })],
    ["judge", finding({ source: "prompt_injection", chatId: "c1" })],
    ["analyzer", finding({ source: "llm_analyzer", ruleId: "pii.llm" })],
    ["shadow", finding({ source: "shadow_mcp", chatId: "c1" })],
    [
      "shadow",
      finding({ source: "shadow_mcp", mediationSurface: "shadow_mcp" }),
    ],
  ] as const)("detects %s", (kind, result) => {
    expect(findingKind(result)).toBe(kind);
  });

  it("keeps a judge finding on an MCP call in the MCP view", () => {
    expect(
      findingKind(finding({ source: "prompt_injection", toolsetId: "t" })),
    ).toBe("mcp");
  });
});

describe("findingMessageKind", () => {
  it("reads the attributed span field", () => {
    expect(
      findingMessageKind(
        finding({ spans: [{ match: "", field: "tool.args" }] }),
      ),
    ).toBe("Tool request");
    expect(
      findingMessageKind(finding({ spans: [{ match: "", field: "prompt" }] })),
    ).toBe("User prompt");
  });

  it("falls back to the tool name, then a generic message", () => {
    expect(findingMessageKind(finding({ toolName: "bash" }))).toBe(
      "Tool request",
    );
    expect(findingMessageKind(finding())).toBe("Message");
  });

  it("formats the row detail tool-first context", () => {
    expect(chatFindingDetail(finding({ toolName: "bash" }))).toBe(
      "Tool request · bash",
    );
    expect(chatFindingDetail(finding())).toBe("");
  });
});
