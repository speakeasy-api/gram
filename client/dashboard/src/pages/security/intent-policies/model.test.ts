// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import { callOutcome, formatValue, toolDisplayName } from "./model";

function call(overrides: Partial<IntentCallAssessment>): IntentCallAssessment {
  return {
    arguments: "{}",
    decidedBy: [],
    decision: "permit",
    labels: [],
    messageId: "m",
    occurredAt: new Date(0),
    outcomes: [],
    seq: 1,
    toolCallId: "t",
    toolName: "Bash",
    usage: { costUsd: 0, inputTokens: 0, latencyMs: 0, outputTokens: 0 },
    ...overrides,
  };
}

describe("callOutcome", () => {
  it("separates evaluator failure from a policy block", () => {
    const failed = call({
      decision: "block",
      decidedBy: ["no_secret_egress"],
      evaluatorError: "jev: API key is required",
      outcomes: [
        {
          contractId: "no_secret_egress",
          error: "evaluator did not answer: carries_secret",
          inputs: {},
        },
      ],
    });
    expect(callOutcome(failed)).toBe("failed");
  });

  it("keeps a real block when a contract matched", () => {
    const blocked = call({
      decision: "block",
      decidedBy: ["no_force_push"],
      evaluatorError: "partial",
      outcomes: [{ contractId: "no_force_push", result: "block", inputs: {} }],
    });
    expect(callOutcome(blocked)).toBe("block");
  });

  it("passes clarify through", () => {
    expect(callOutcome(call({ decision: "clarify" }))).toBe("clarify");
  });
});

describe("formatValue", () => {
  it("renders sets and missing values", () => {
    expect(formatValue(["read", "exec"])).toBe("read, exec");
    expect(formatValue([])).toBe("none");
    expect(formatValue(null)).toBe("missing");
    expect(formatValue(true)).toBe("true");
  });
});

describe("toolDisplayName", () => {
  it("prefers the MCP function", () => {
    expect(
      toolDisplayName(
        call({
          toolName: "mcp__github__create_issue",
          mcpFunction: "create_issue",
        }),
      ),
    ).toBe("create_issue");
  });
});
