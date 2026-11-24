// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import {
  callOutcome,
  decisionReason,
  formatClock,
  formatDuration,
  formatValue,
  secretExposure,
  shellCommand,
  shellSteps,
  toolDisplayName,
} from "./model";

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
      decidedBy: ["no_credential_egress"],
      evaluatorError: "jev: API key is required",
      outcomes: [
        {
          contractId: "no_credential_egress",
          error: "evaluator did not answer: carries_secret",
          inputs: {},
        },
      ],
    });
    expect(callOutcome(failed)).toBe("failed");
  });

  it("treats an unanswered label as a failure even without an evaluator error", () => {
    const failed = call({
      decision: "block",
      decidedBy: ["no_credential_egress"],
      outcomes: [
        {
          contractId: "no_credential_egress",
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

describe("formatClock and formatDuration", () => {
  it("formats elapsed time", () => {
    expect(formatClock(5)).toBe("0:05");
    expect(formatClock(3725)).toBe("1:02:05");
    expect(formatDuration(161_000)).toBe("2m 41s");
    expect(formatDuration(42_000)).toBe("42s");
  });
});

function labeled(labels: Record<string, unknown>): IntentCallAssessment {
  return call({
    labels: Object.entries(labels).map(([key, value]) => ({
      key,
      value,
      probability: 0.9,
      rank: 0,
    })),
  });
}

describe("secretExposure", () => {
  it("flags later calls that run code or send data", () => {
    const exposure = secretExposure([
      labeled({ effect_class: ["exec_program"] }),
      labeled({ effect_class: ["read"], touches_credential_store: true }),
      labeled({ effect_class: ["read"] }),
      labeled({ effect_class: ["exec_program"] }),
      labeled({ effect_class: ["write_local", "outbound"] }),
    ]);
    expect(exposure).toEqual({ firstIndex: 1, riskyIndexes: [3, 4] });
  });

  it("ignores a credential store named only in an outbound call", () => {
    expect(
      secretExposure([
        labeled({ effect_class: ["outbound"], touches_credential_store: true }),
        labeled({ effect_class: ["exec_program"] }),
      ]),
    ).toBeNull();
  });

  it("is null when no call touches a secret", () => {
    expect(
      secretExposure([labeled({ effect_class: ["exec_program"] })]),
    ).toBeNull();
  });
});

describe("shell commands", () => {
  it("reads the command argument", () => {
    expect(shellCommand('{"command":"ls -la"}')).toBe("ls -la");
    expect(shellCommand('{"path":"/app"}')).toBeNull();
    expect(shellCommand("not json")).toBeNull();
  });

  it("splits at top-level separators only", () => {
    expect(
      shellSteps(`ls /app; echo "a; b"; cat x || echo 'no; file'\nwhich curl`),
    ).toEqual([
      "ls /app",
      'echo "a; b"',
      "cat x || echo 'no; file'",
      "which curl",
    ]);
  });

  it("keeps separators inside substitutions and subshells", () => {
    expect(shellSteps("echo $(a; b); (cd x; make); ls")).toEqual([
      "echo $(a; b)",
      "(cd x; make)",
      "ls",
    ]);
  });
});

describe("decisionReason", () => {
  it("names the contract and the label that fired it", () => {
    const c = call({
      decision: "clarify",
      decidedBy: ["review_credential_reads"],
      labels: [
        {
          key: "touches_credential_store",
          value: true,
          probability: 0.94,
          rank: 0,
        },
      ],
      outcomes: [
        {
          contractId: "review_credential_reads",
          result: "clarify",
          inputs: { "label.touches_credential_store": true },
        },
      ],
    });
    expect(
      decisionReason(
        c,
        new Map([["review_credential_reads", "Review credential reads"]]),
      ),
    ).toBe(
      "Review credential reads fired because touches_credential_store is true (94%).",
    );
  });

  it("explains a permit", () => {
    expect(decisionReason(call({}), new Map())).toMatch(/No contract fired/);
  });
});
