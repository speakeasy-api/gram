import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { IntentSessionAnalysis } from "@gram/client/models/components/intentsessionanalysis.js";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import { callOutcome, type CallOutcome } from "./model";

/** Calls labeled per request; small enough that progress moves visibly. */
export const ANALYSIS_BATCH = 8;

// Mirrors the server's per-session cap (intentpolicy.MaxActions).
const MAX_ANALYZED_CALLS = 200;

export type AnalysisProgress = { done: number; total: number };

/** Appends one batch to the analysis so far; totals count the whole session. */
export function mergeBatch(
  prev: IntentSessionAnalysis | null,
  batch: IntentSessionAnalysis,
): IntentSessionAnalysis {
  if (!prev) return batch;
  const a = prev.totals;
  const b = batch.totals;
  return {
    ...batch,
    assessments: [...prev.assessments, ...batch.assessments],
    totals: {
      calls: b.calls,
      judged: a.judged + b.judged,
      blocked: a.blocked + b.blocked,
      clarified: a.clarified + b.clarified,
      permitted: a.permitted + b.permitted,
      costUsd: a.costUsd + b.costUsd,
      latencyMs: a.latencyMs + b.latencyMs,
    },
  };
}

/** How many calls a full run labels: every call, up to the server's cap. */
export function analyzableCalls(totalCalls: number): number {
  return Math.min(totalCalls, MAX_ANALYZED_CALLS);
}

export type AnalysisReport = {
  kind: "intent-policy-analysis";
  generatedAt: string;
  session: { chatId: string; name: string | null; generation: number };
  guardrail: { id: string | null; version: string };
  evaluator: IntentSessionAnalysis["evaluator"];
  /** False when the run stopped before labeling every analyzable call. */
  complete: boolean;
  limitHit: boolean;
  totals: IntentSessionAnalysis["totals"];
  calls: {
    index: number;
    occurredAt: string;
    messageId: string;
    toolCallId: string;
    tool: string;
    mcpServer: string | null;
    mcpFunction: string | null;
    arguments: string;
    decision: string;
    outcome: CallOutcome;
    decidedBy: { id: string; name: string }[];
    evaluatorError: string | null;
    labels: IntentCallAssessment["labels"];
    contracts: IntentCallAssessment["outcomes"];
    usage: IntentCallAssessment["usage"];
  }[];
};

/** A self-describing record of one analysis, for download. */
export function buildReport({
  analysis,
  guardrail,
  sessionName,
  generatedAt,
}: {
  analysis: IntentSessionAnalysis;
  guardrail: IntentGuardrail | undefined;
  sessionName: string | undefined;
  generatedAt: Date;
}): AnalysisReport {
  const names = new Map(guardrail?.contracts.map((c) => [c.id, c.name]));
  return {
    kind: "intent-policy-analysis",
    generatedAt: generatedAt.toISOString(),
    session: {
      chatId: analysis.chatId,
      name: sessionName ?? null,
      generation: analysis.generation,
    },
    guardrail: {
      id: guardrail?.id ?? null,
      version: analysis.guardrailVersion,
    },
    evaluator: analysis.evaluator,
    complete: analysis.totals.judged >= analyzableCalls(analysis.totals.calls),
    limitHit: analysis.limitHit,
    totals: analysis.totals,
    calls: analysis.assessments.map((a, i) => ({
      index: i + 1,
      occurredAt: a.occurredAt.toISOString(),
      messageId: a.messageId,
      toolCallId: a.toolCallId,
      tool: a.toolName,
      mcpServer: a.mcpServer ?? null,
      mcpFunction: a.mcpFunction ?? null,
      arguments: a.arguments,
      decision: a.decision,
      outcome: callOutcome(a),
      decidedBy: a.decidedBy.map((id) => ({ id, name: names.get(id) ?? id })),
      evaluatorError: a.evaluatorError ?? null,
      labels: a.labels,
      contracts: a.outcomes,
      usage: a.usage,
    })),
  };
}

/** `intent-analysis-<session>-<yyyymmdd-hhmm>.json` */
export function reportFilename(
  sessionName: string | undefined,
  at: Date,
): string {
  const slug =
    (sessionName ?? "session")
      .toLowerCase()
      .replace(/\.[a-z0-9]+$/, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48) || "session";
  const pad = (n: number) => String(n).padStart(2, "0");
  const stamp = `${at.getFullYear()}${pad(at.getMonth() + 1)}${pad(at.getDate())}-${pad(at.getHours())}${pad(at.getMinutes())}`;
  return `intent-analysis-${slug}-${stamp}.json`;
}
