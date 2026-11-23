import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentLabelValue } from "@gram/client/models/components/intentlabelvalue.js";

/** How a call's decision renders: an evaluator failure is not a policy block. */
export type CallOutcome = "permit" | "clarify" | "block" | "failed";

/** Prefix the server puts on a contract error caused by an unanswered label. */
const MISSING_LABEL_PREFIX = "evaluator did not answer";

export function needsReview(call: IntentCallAssessment): boolean {
  return callOutcome(call) !== "permit";
}

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

/** Labels at or below this probability are flagged in the decision panel. */
export const LOW_CONFIDENCE = 0.75;

/** Guardrail label keys the session timeline reads. */
const SECRET_LABELS = ["carries_secret", "touches_credential_store"];
const EFFECT_CLASS_LABEL = "effect_class";
const RUNS_OR_SENDS = ["exec_task", "exec_program", "outbound"];

/** "m:ss", or "h:mm:ss" past an hour, for an elapsed number of seconds. */
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (v: number) => String(v).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`;
}

/** "45s", "2m 41s", "1h 5m" for a span in milliseconds. */
export function formatDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

export type SecretExposure = {
  /** Index of the first call whose labels say it read or carried a secret. */
  firstIndex: number;
  /** Later calls that run code or send data while that secret is in context. */
  riskyIndexes: number[];
};

/**
 * Per-call contracts judge each call alone; this shows when a later call runs
 * code or sends data after an earlier call put a secret in the agent's context.
 */
export function secretExposure(
  calls: IntentCallAssessment[],
): SecretExposure | null {
  const firstIndex = calls.findIndex((c) =>
    SECRET_LABELS.some((k) => findLabel(c, k)?.value === true),
  );
  if (firstIndex === -1) return null;
  const riskyIndexes = calls.flatMap((c, i) => {
    if (i <= firstIndex) return [];
    const effects = findLabel(c, EFFECT_CLASS_LABEL)?.value;
    return Array.isArray(effects) &&
      effects.some((e) => RUNS_OR_SENDS.includes(String(e)))
      ? [i]
      : [];
  });
  return { firstIndex, riskyIndexes };
}

/** The shell command a call ran, when its arguments carry one. */
export function shellCommand(raw: string): string | null {
  try {
    const args: unknown = JSON.parse(raw);
    if (args && typeof args === "object" && "command" in args) {
      const command = (args as { command: unknown }).command;
      return typeof command === "string" ? command : null;
    }
  } catch {
    // Not JSON: the arguments render as raw text.
  }
  return null;
}

/** Splits a shell command into steps at top-level `;` and newlines. */
export function shellSteps(command: string): string[] {
  const steps: string[] = [];
  let current = "";
  let quote: string | null = null;
  for (let i = 0; i < command.length; i++) {
    const ch = command[i] ?? "";
    if (quote) {
      current += ch;
      if (ch === "\\" && quote === '"' && i + 1 < command.length) {
        current += command[++i] ?? "";
      } else if (ch === quote) {
        quote = null;
      }
    } else if (ch === "'" || ch === '"') {
      quote = ch;
      current += ch;
    } else if (ch === "\\" && i + 1 < command.length) {
      current += ch + (command[++i] ?? "");
    } else if (ch === ";" || ch === "\n") {
      if (current.trim()) steps.push(current.trim());
      current = "";
    } else {
      current += ch;
    }
  }
  if (current.trim()) steps.push(current.trim());
  return steps;
}

/** One sentence on why the call got its decision, built from contract inputs. */
export function decisionReason(
  call: IntentCallAssessment,
  contractNames: Map<string, string>,
): string {
  const outcome = callOutcome(call);
  if (outcome === "failed") {
    return "The evaluator did not answer every label, so the call fails closed.";
  }
  const deciders = call.outcomes.filter((o) =>
    call.decidedBy.includes(o.contractId),
  );
  if (outcome === "permit" || deciders.length === 0) {
    return "No contract fired, so the call runs without asking anyone.";
  }
  return deciders
    .map((o) => {
      const name = contractNames.get(o.contractId) ?? o.contractId;
      const parts = Object.entries(o.inputs)
        .filter(([k]) => k.startsWith("label.") || k.startsWith("action."))
        .map(([k, v]) => {
          const [scope, key = k] = k.split(".", 2);
          const label = scope === "label" ? findLabel(call, key) : undefined;
          const confidence = label
            ? ` (${Math.round(label.probability * 100)}%)`
            : "";
          return `${key} is ${formatValue(v)}${confidence}`;
        });
      return parts.length > 0
        ? `${name} fired because ${parts.join(" and ")}.`
        : `${name} matched this call.`;
    })
    .join(" ");
}
