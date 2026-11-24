// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentSessionAnalysis } from "@gram/client/models/components/intentsessionanalysis.js";
import {
  analyzableCalls,
  buildReport,
  mergeBatch,
  reportFilename,
} from "./analysisRun";

function call(
  toolCallId: string,
  decision: "permit" | "block",
): IntentCallAssessment {
  return {
    arguments: "{}",
    decidedBy: decision === "block" ? ["c"] : [],
    decision,
    labels: [],
    messageId: "m",
    occurredAt: new Date(0),
    outcomes: [],
    seq: 1,
    toolCallId,
    toolName: "Bash",
    usage: { costUsd: 0.01, inputTokens: 0, latencyMs: 100, outputTokens: 0 },
  };
}

function batch(
  calls: IntentCallAssessment[],
  total: number,
): IntentSessionAnalysis {
  return {
    chatId: "chat",
    generation: 0,
    guardrailVersion: "v1",
    limitHit: false,
    evaluator: {
      evaluator: "jev",
      provider: "p",
      model: "m",
      promptVersion: "1",
    },
    assessments: calls,
    totals: {
      calls: total,
      judged: calls.length,
      blocked: calls.filter((c) => c.decision === "block").length,
      clarified: 0,
      permitted: calls.filter((c) => c.decision === "permit").length,
      costUsd: 0.01 * calls.length,
      latencyMs: 100 * calls.length,
    },
  };
}

describe("mergeBatch", () => {
  it("appends calls and sums totals, keeping the session call count", () => {
    const merged = mergeBatch(
      mergeBatch(null, batch([call("a", "permit"), call("b", "block")], 3)),
      batch([call("c", "permit")], 3),
    );
    expect(merged.assessments.map((a) => a.toolCallId)).toEqual([
      "a",
      "b",
      "c",
    ]);
    expect(merged.totals).toMatchObject({
      calls: 3,
      judged: 3,
      blocked: 1,
      permitted: 2,
      latencyMs: 300,
    });
  });
});

describe("analyzableCalls", () => {
  it("caps at the server limit", () => {
    expect(analyzableCalls(12)).toBe(12);
    expect(analyzableCalls(500)).toBe(200);
  });
});

describe("buildReport", () => {
  it("records each call with named deciders and flags an incomplete run", () => {
    const report = buildReport({
      analysis: batch([call("a", "block")], 4),
      guardrail: {
        id: "intent-baseline",
        version: "v1",
        statements: [],
        questions: [],
        contracts: [
          {
            id: "c",
            name: "No secret egress",
            statement: "",
            outcome: "block",
            conditions: [],
            expression: "",
          },
        ],
      },
      sessionName: "run.jsonl",
      generatedAt: new Date(0),
    });
    expect(report.complete).toBe(false);
    expect(report.calls[0]).toMatchObject({
      index: 1,
      outcome: "block",
      decidedBy: [{ id: "c", name: "No secret egress" }],
    });
  });
});

describe("reportFilename", () => {
  it("slugs the session name and stamps the time", () => {
    const at = new Date(2026, 10, 23, 9, 5);
    expect(reportFilename("Delayed Exfil.jsonl", at)).toBe(
      "intent-analysis-delayed-exfil-20261123-0905.json",
    );
    expect(reportFilename(undefined, at)).toBe(
      "intent-analysis-session-20261123-0905.json",
    );
  });
});
