import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentContract } from "@gram/client/models/components/intentcontract.js";
import type { IntentLabelQuestion } from "@gram/client/models/components/intentlabelquestion.js";

export type RuleOutcome = "block" | "clarify";

/** Which rules the list emphasizes: all, or one outcome. */
export type RuleFilter = "all" | RuleOutcome;

export const KIND_LABEL: Record<string, string> = {
  noul: "yes / no",
  multi_noul: "any of",
  choice: "one of",
};

export const OUTCOME_ACTION: Record<RuleOutcome, string> = {
  block: "Block the call",
  clarify: "Ask a person first",
};

/** Label keys a rule reads. */
export function labelsRead(contract: IntentContract): string[] {
  const keys: string[] = [];
  for (const c of contract.conditions) {
    if (c.label && !keys.includes(c.label)) keys.push(c.label);
  }
  return keys;
}

/** Option keys of `question` that the rule checks for; empty when none apply. */
export function checkedOptions(
  contract: IntentContract,
  question: IntentLabelQuestion,
): Set<string> {
  const checked = new Set<string>();
  for (const c of contract.conditions) {
    if (c.label !== question.key) continue;
    const negated =
      c.op === "is not" || c.op === "includes anything other than";
    for (const o of question.options) {
      if (c.values.includes(o.key) !== negated) checked.add(o.key);
    }
  }
  return checked;
}

export type RuleHit = {
  /** 0-based position of the call in the analysis. */
  callIndex: number;
  call: IntentCallAssessment;
  result: "block" | "clarify" | "error";
  /** True when another contract's decision won. */
  overridden: boolean;
};

/** Calls on which the rule fired (or errored), in session order. */
export function ruleHits(
  contract: IntentContract,
  calls: IntentCallAssessment[],
): RuleHit[] {
  return calls.flatMap((call, callIndex) => {
    const o = call.outcomes.find((x) => x.contractId === contract.id);
    if (!o) return [];
    const result = o.error
      ? "error"
      : o.result === "block" || o.result === "clarify"
        ? o.result
        : null;
    if (!result) return [];
    return [
      {
        callIndex,
        call,
        result,
        overridden: !call.decidedBy.includes(contract.id),
      },
    ];
  });
}

/** The rule's outcome; an outcome a newer server adds renders as the strictest. */
export function ruleOutcome(contract: IntentContract): RuleOutcome {
  return contract.outcome === "clarify" ? "clarify" : "block";
}

export type ExpressionToken = {
  text: string;
  kind: "ref" | "block" | "clarify" | "abstain" | "string" | "plain";
};

const TOKEN =
  /(?:label|action|tool)\.[A-Za-z_][\w.]*|\b(?:BLOCK|CLARIFY|ABSTAIN)\b|'(?:[^'\\]|\\.)*'/g;

/** Splits a CEL expression into tokens for highlighting. */
export function tokenizeExpression(expression: string): ExpressionToken[] {
  const tokens: ExpressionToken[] = [];
  let last = 0;
  for (const m of expression.matchAll(TOKEN)) {
    const at = m.index ?? 0;
    if (at > last) {
      tokens.push({ text: expression.slice(last, at), kind: "plain" });
    }
    const text = m[0];
    tokens.push({
      text,
      kind: text.startsWith("'")
        ? "string"
        : text === "BLOCK"
          ? "block"
          : text === "CLARIFY"
            ? "clarify"
            : text === "ABSTAIN"
              ? "abstain"
              : "ref",
    });
    last = at + text.length;
  }
  if (last < expression.length) {
    tokens.push({ text: expression.slice(last), kind: "plain" });
  }
  return tokens;
}
