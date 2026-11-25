// Types and pure helpers for the Organizational Memory eval runs page. The report shape
// mirrors server/internal/platformmcp/memoryeval/bench/report.go (Report and
// ArmScore), prepare.go (Preparation) and metered.go (ModelSpend), which the
// API returns as report_json.

import { z } from "zod";

/** Server cap on answer models per run (evalrun.MaxAnswerModels). */
export const MAX_ANSWER_MODELS = 4;
/** Server cap on repeats per model and arm (evalrun.MaxRuns). */
export const MAX_RUNS = 3;

export type MemoryEvalStatus = "queued" | "running" | "succeeded" | "failed";

export type MemoryEvalConfig = {
  answer_models: string[];
  judge_model: string;
  summary_model: string;
  digest_model: string;
  runs: number;
};

export type MemoryEvalRun = {
  id: string;
  status: MemoryEvalStatus;
  config: MemoryEvalConfig;
  cost_usd?: number;
  error?: string;
  requested_by: string;
  report_json?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
};

export type MemoryEvalOptions = {
  defaults: MemoryEvalConfig;
  allowed_models: string[];
  daily_run_limit: number;
  runs_today: number;
};

export type KindScore = { correct: number; total: number };

export type ArmScore = {
  model: string;
  arm: string;
  has_tools: boolean;
  /** Null or empty when nothing in the arm was graded. */
  accuracy_by_run: number[] | null;
  accuracy: number;
  by_kind: Record<string, KindScore>;
  answerable: number;
  cited_answers: number;
  valid_cited: number;
  leaks: string[] | null;
  isolation_violations: string[] | null;
  not_found: number;
  negatives: number;
  errors: number;
  answers: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
  latency_mean_ms: number;
  latency_p95_ms: number;
};

export type ModelSpend = {
  model: string;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
  /** Calls whose provider reported no cost; cost_usd is then a lower bound. */
  unpriced_calls?: number;
};

export type Preparation = {
  models: { summary_model: string; digest_model: string };
  summaries: number;
  failed_summaries: number;
  digests: number;
  summary_spend: ModelSpend;
  digest_spend: ModelSpend;
  duration_ms: number;
};

export type MemoryEvalReport = {
  judge_model: string;
  runs: number;
  questions: number;
  scores: ArmScore[];
  judge_cost_usd: number;
  preparation?: Preparation;
};

/** Runs that may still write team memory; a new run waits for these. */
export function isActive(run: Pick<MemoryEvalRun, "status">): boolean {
  return run.status === "queued" || run.status === "running";
}

// Reports are stored indefinitely, so an older or hand-written one may lack
// fields. These schemas cover every field the page reads; a report that fails
// them is treated as unreadable instead of crashing the comparison.
const spendSchema = z.object({
  cost_usd: z.number(),
  unpriced_calls: z.number().optional(),
});

const reportSchema = z.object({
  judge_model: z.string(),
  judge_cost_usd: z.number(),
  scores: z.array(
    z.object({
      model: z.string(),
      arm: z.string(),
      has_tools: z.boolean(),
      accuracy_by_run: z.array(z.number()).nullable(),
      accuracy: z.number(),
      answerable: z.number(),
      valid_cited: z.number(),
      leaks: z.array(z.string()).nullable(),
      isolation_violations: z.array(z.string()).nullable(),
      errors: z.number(),
      prompt_tokens: z.number(),
      completion_tokens: z.number(),
      cost_usd: z.number(),
      latency_p95_ms: z.number(),
    }),
  ),
  preparation: z
    .object({
      models: z.object({ summary_model: z.string(), digest_model: z.string() }),
      summaries: z.number(),
      failed_summaries: z.number(),
      digests: z.number(),
      summary_spend: spendSchema,
      digest_spend: spendSchema,
    })
    .optional(),
});

/** parseReport returns null for a run without a readable report. */
export function parseReport(run: MemoryEvalRun): MemoryEvalReport | null {
  if (!run.report_json) return null;
  let raw: unknown;
  try {
    raw = JSON.parse(run.report_json);
  } catch {
    return null;
  }
  return reportSchema.safeParse(raw).success ? (raw as MemoryEvalReport) : null;
}

/** graded is false for an arm where every answer errored. */
export function graded(s: Pick<ArmScore, "accuracy_by_run">): boolean {
  return (s.accuracy_by_run?.length ?? 0) > 0;
}

/** accuracyLabel prints a dash for an arm with nothing graded. */
export function accuracyLabel(s: ArmScore): string {
  return graded(s) ? percent(s.accuracy) : "—";
}

/** memoryWriteCost sums summary and digest spend, marking a lower bound. */
export function memoryWriteCost(p: Preparation): string {
  const unpriced =
    (p.summary_spend.unpriced_calls ?? 0) +
    (p.digest_spend.unpriced_calls ?? 0);
  const total = usd(p.summary_spend.cost_usd + p.digest_spend.cost_usd);
  return unpriced > 0 ? `≥${total}` : total;
}

/** One row of the comparison table: a model in an arm, in one run. */
export type ComparisonRow = {
  runId: string;
  runLabel: string;
  summaryModel: string;
  digestModel: string;
  judgeModel: string;
  score: ArmScore;
  /** Best accuracy for this arm across every compared, safe, graded row. */
  best: boolean;
  /** Any planted secret or cross-project canary reached the answer. */
  unsafe: boolean;
};

export function comparisonRows(
  runs: { run: MemoryEvalRun; report: MemoryEvalReport }[],
): ComparisonRow[] {
  const isUnsafe = (s: ArmScore) =>
    (s.leaks?.length ?? 0) > 0 || (s.isolation_violations?.length ?? 0) > 0;
  // A leak disqualifies a row, so only safe, graded rows can be best.
  const bestByArm = new Map<string, number>();
  for (const { report } of runs) {
    for (const s of report.scores) {
      if (isUnsafe(s) || !graded(s)) continue;
      bestByArm.set(s.arm, Math.max(bestByArm.get(s.arm) ?? 0, s.accuracy));
    }
  }
  const rows: ComparisonRow[] = [];
  for (const { run, report } of runs) {
    for (const s of report.scores) {
      rows.push({
        runId: run.id,
        runLabel: shortRunLabel(run),
        summaryModel:
          report.preparation?.models.summary_model ?? run.config.summary_model,
        digestModel:
          report.preparation?.models.digest_model ?? run.config.digest_model,
        judgeModel: report.judge_model || run.config.judge_model,
        score: s,
        best:
          !isUnsafe(s) &&
          graded(s) &&
          s.accuracy > 0 &&
          s.accuracy === bestByArm.get(s.arm),
        unsafe: isUnsafe(s),
      });
    }
  }
  // Ungraded rows sort last in their arm rather than reading as 0%.
  const rank = (s: ArmScore) => (graded(s) ? s.accuracy : -1);
  return rows.sort(
    (a, b) =>
      a.score.arm.localeCompare(b.score.arm) || rank(b.score) - rank(a.score),
  );
}

/** utcMinute formats a timestamp as "YYYY-MM-DD HH:MM" in UTC. */
function utcMinute(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toISOString().slice(0, 16).replace("T", " ");
}

/**
 * runLabel is the queue time in UTC plus the ID's last 8 characters: IDs are
 * UUIDv7, so the leading characters are timestamp bits shared by runs created
 * the same minute, while the tail is random.
 */
export function runLabel(
  run: Pick<MemoryEvalRun, "id" | "created_at">,
): string {
  return `${utcMinute(run.created_at)} · ${run.id.slice(-8)}`;
}

/** shortRunLabel fits a comparison cell: month, day and time in UTC. */
export function shortRunLabel(run: Pick<MemoryEvalRun, "created_at">): string {
  return utcMinute(run.created_at).slice(5);
}

/** duration renders a span in milliseconds as "2h 05m", "12m" or "<1m". */
export function duration(ms: number): string {
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 1) return "<1m";
  const hours = Math.floor(minutes / 60);
  return hours > 0
    ? `${hours}h ${String(minutes % 60).padStart(2, "0")}m`
    : `${minutes}m`;
}

/**
 * runTiming says how long an active run has been going, or how long a
 * finished one took. A queued run counts from when it was queued.
 */
export function runTiming(
  run: Pick<
    MemoryEvalRun,
    "status" | "created_at" | "started_at" | "finished_at"
  >,
  now: number,
): string {
  const since = Date.parse(run.started_at ?? run.created_at);
  if (isActive(run)) return `for ${duration(now - since)}`;
  if (run.started_at && run.finished_at)
    return `took ${duration(Date.parse(run.finished_at) - Date.parse(run.started_at))}`;
  return "";
}

export function percent(value: number): string {
  return `${Math.round(value * 1000) / 10}%`;
}

export function usd(value: number | undefined): string {
  return value === undefined ? "—" : `$${value.toFixed(2)}`;
}

/** citationRate is valid citations over answerable graded answers. */
export function citationRate(s: ArmScore): string {
  if (!s.has_tools || s.answerable === 0) return "—";
  return percent(s.valid_cited / s.answerable);
}

/** validateConfig mirrors the server's caps so the form says why early. */
export function validateConfig(
  cfg: MemoryEvalConfig,
  allowed: string[],
): string | null {
  if (cfg.answer_models.length === 0) return "Pick at least one answer model.";
  if (cfg.answer_models.length > MAX_ANSWER_MODELS)
    return `Pick at most ${MAX_ANSWER_MODELS} answer models.`;
  if (new Set(cfg.answer_models).size !== cfg.answer_models.length)
    return "Each answer model can appear once.";
  if (cfg.runs < 1 || cfg.runs > MAX_RUNS)
    return `Runs must be between 1 and ${MAX_RUNS}.`;
  if (cfg.answer_models.includes(cfg.judge_model))
    return "The judge must not also be an answer model; it would grade itself.";
  const allowedSet = new Set(allowed);
  for (const model of [
    ...cfg.answer_models,
    cfg.judge_model,
    cfg.summary_model,
    cfg.digest_model,
  ]) {
    if (!allowedSet.has(model)) return `${model} is not an allowed model.`;
  }
  return null;
}
