// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentContract } from "@gram/client/models/components/intentcontract.js";
import type { IntentCondition } from "@gram/client/models/components/intentcondition.js";
import type { IntentLabelQuestion } from "@gram/client/models/components/intentlabelquestion.js";
import {
  checkedOptions,
  labelsRead,
  ruleHits,
  tokenizeExpression,
} from "./guardrail";

const effectClass: IntentLabelQuestion = {
  key: "effect_class",
  kind: "multi_noul",
  instructions: "",
  options: ["read", "write", "outbound"].map((key) => ({
    key,
    description: "",
    ordinal: 0,
    uncertain: false,
  })),
};

function contract(overrides: Partial<IntentContract>): IntentContract {
  return {
    id: "c",
    name: "C",
    statement: "",
    outcome: "block",
    conditions: [],
    expression: "",
    ...overrides,
  };
}

const cond = (
  label: string,
  op: IntentCondition["op"],
  ...values: string[]
): IntentCondition => ({
  join: "",
  label,
  op,
  values,
});

describe("labelsRead", () => {
  it("lists each label once in condition order", () => {
    const c = contract({
      conditions: [
        cond("target_locations", "includes", "remote"),
        cond("target_locations", "includes", "credential_store"),
        cond("effect_class", "includes", "destructive"),
      ],
    });
    expect(labelsRead(c)).toEqual(["target_locations", "effect_class"]);
  });
});

describe("checkedOptions", () => {
  it("checks the listed values", () => {
    const c = contract({
      conditions: [cond("effect_class", "includes", "outbound")],
    });
    expect([...checkedOptions(c, effectClass)]).toEqual(["outbound"]);
  });

  it("checks everything else for a negated clause", () => {
    const c = contract({
      conditions: [
        cond("effect_class", "includes anything other than", "read"),
      ],
    });
    expect([...checkedOptions(c, effectClass)]).toEqual(["write", "outbound"]);
  });
});

describe("ruleHits", () => {
  const call = (
    result: string | undefined,
    decidedBy: string[],
    error?: string,
  ): IntentCallAssessment => ({
    arguments: "{}",
    decidedBy,
    decision: "block",
    labels: [],
    messageId: "m",
    occurredAt: new Date(0),
    outcomes: [{ contractId: "c", inputs: {}, result, error }],
    seq: 1,
    toolCallId: "t",
    toolName: "Bash",
    usage: { costUsd: 0, inputTokens: 0, latencyMs: 0, outputTokens: 0 },
  });

  it("finds firings, errors, and overrides", () => {
    const hits = ruleHits(contract({}), [
      call("abstain", []),
      call("block", ["c"]),
      call("clarify", ["other"]),
      call(undefined, ["c"], "boom"),
    ]);
    expect(hits.map((h) => [h.callIndex, h.result, h.overridden])).toEqual([
      [1, "block", false],
      [2, "clarify", true],
      [3, "error", false],
    ]);
  });
});

describe("tokenizeExpression", () => {
  it("marks refs, outcomes, and strings, keeping the text intact", () => {
    const expr =
      "'credential' in label.payload_data && 'outbound' in action.effect_class ? BLOCK : ABSTAIN";
    const tokens = tokenizeExpression(expr);
    expect(tokens.map((t) => t.text).join("")).toBe(expr);
    expect(tokens.filter((t) => t.kind !== "plain")).toEqual([
      { text: "'credential'", kind: "string" },
      { text: "label.payload_data", kind: "ref" },
      { text: "'outbound'", kind: "string" },
      { text: "action.effect_class", kind: "ref" },
      { text: "BLOCK", kind: "block" },
      { text: "ABSTAIN", kind: "abstain" },
    ]);
  });
});
