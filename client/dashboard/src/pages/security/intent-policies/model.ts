import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentLabelValue } from "@gram/client/models/components/intentlabelvalue.js";

/** How a call's decision renders: an evaluator failure is not a policy block. */
export type CallOutcome = "permit" | "clarify" | "block" | "failed";

/** Prefix the server puts on a contract error caused by an unanswered label. */
const MISSING_LABEL_PREFIX = "evaluator did not answer";

export function callOutcome(call: IntentCallAssessment): CallOutcome {
  if (call.decision === "block" && blockedByEvaluatorFailure(call)) {
    return "failed";
  }
  switch (call.decision) {
    case "permit":
      return "permit";
    case "clarify":
      return "clarify";
    case "block":
      return "block";
    default:
      // A decision a newer server adds renders as the strictest.
      return "block";
  }
}

export function formatCost(usd: number): string {
  return usd > 0 ? `$${usd.toFixed(4)}` : "cost not reported";
}

/** True when every contract that decided BLOCK did so because a label is missing. */
export function blockedByEvaluatorFailure(call: IntentCallAssessment): boolean {
  if (!call.evaluatorError) return false;
  const deciders = call.outcomes.filter((o) =>
    call.decidedBy.includes(o.contractId),
  );
  return (
    deciders.length > 0 &&
    deciders.every((o) => o.error?.startsWith(MISSING_LABEL_PREFIX))
  );
}

export function findLabel(
  call: IntentCallAssessment,
  key: string,
): IntentLabelValue | undefined {
  return call.labels.find((l) => l.key === key);
}

/** The display name of the called tool: the MCP function when there is one. */
export function toolDisplayName(call: IntentCallAssessment): string {
  return call.mcpFunction || call.toolName;
}

export function formatValue(value: unknown): string {
  if (value === null || value === undefined) return "missing";
  if (Array.isArray(value)) return value.length ? value.join(", ") : "none";
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}

export function prettyArguments(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export const OUTCOME_LABEL: Record<CallOutcome, string> = {
  permit: "Permit",
  clarify: "Clarify",
  block: "Block",
  failed: "Evaluator failed",
};
